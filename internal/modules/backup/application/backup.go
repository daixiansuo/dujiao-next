package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/google/uuid"
)

func (s *Service) StartBackup(source string, adminID uint) (*domain.Record, error) {
	if !s.supported() {
		return nil, domain.ErrUnsupported
	}
	source = strings.TrimSpace(source)
	if source == "" || len(source) > 16 {
		return nil, domain.ErrInvalidConfig
	}
	s.mu.Lock()
	life := s.ctx
	s.mu.Unlock()
	if life == nil || !s.lockOps(life) {
		return nil, domain.ErrNotReady
	}
	defer s.ops.Unlock()
	s.mu.Lock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, domain.ErrNotReady
	}
	if s.current != nil {
		s.mu.Unlock()
		return nil, domain.ErrBusy
	}
	life = s.ctx
	s.mu.Unlock()
	cfg, err := s.objectConfig()
	if err != nil {
		return nil, err
	}
	if source == "scheduled" && !cfg.Enabled {
		return nil, domain.ErrNotReady
	}
	if s.records == nil || s.dumper == nil || s.objects == nil {
		return nil, domain.ErrNotReady
	}
	objects, err := s.objects(cfg)
	if err != nil {
		return nil, domain.ErrInvalidConfig
	}
	if objects == nil {
		return nil, domain.ErrNotReady
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	filename := "database-" + now.Format("20060102T150405Z") + "-" + id + ".dump"
	record := &domain.Record{ID: id, Source: source, AdminID: adminID, Status: domain.StatusRunning, Stage: domain.StageExporting, Filename: filename, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, ObjectKey: path.Join(cfg.Prefix, filename), CreatedAt: now, StartedAt: now}
	// Add and admission are atomic with Run's shutdown before Wait.
	s.mu.Lock()
	if life.Err() != nil || s.ctx != life {
		s.mu.Unlock()
		return nil, domain.ErrNotReady
	}
	s.current = copyRecord(record)
	s.jobs.Add(1)
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(life, s.options.Timeout)
	if err := s.records.Create(ctx, record); err != nil {
		cancel()
		s.setCurrent(nil)
		s.jobs.Done()
		return nil, err
	}
	result := copyRecord(record)
	go func() {
		defer s.jobs.Done()
		defer cancel()
		defer s.setCurrent(nil)
		s.execute(ctx, record, objects)
	}()
	return result, nil
}

func (s *Service) execute(ctx context.Context, record *domain.Record, objects contract.ObjectStore) {
	if _, err := s.dumper.Check(ctx); err != nil {
		s.fail(record, "error.backup_not_ready")
		return
	}
	dir, err := s.makeTempDir(record.ID)
	if err != nil {
		s.fail(record, "error.backup_failed")
		return
	}
	defer os.RemoveAll(dir)
	filename := filepath.Join(dir, record.Filename)
	if _, err := s.dumper.Dump(ctx, filename); err != nil {
		// pg_dump stderr can include connection URLs, credentials and SQL. Persist only
		// a fixed diagnosis; raw process errors must never cross the API boundary.
		s.fail(record, "error.backup_failed")
		return
	}
	file, err := os.Open(filename)
	if err != nil {
		s.fail(record, "error.backup_failed")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		s.fail(record, "error.backup_failed")
		return
	}
	hash := sha256.New()
	size, err := io.Copy(hash, &contextReader{ctx: ctx, reader: file})
	if err != nil || size == 0 {
		s.fail(record, "error.backup_failed")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		s.fail(record, "error.backup_failed")
		return
	}
	record.Size = size
	record.SHA256 = hex.EncodeToString(hash.Sum(nil))
	record.Stage = domain.StageUploading
	// Durable upload intent, including checksum, must precede any external side effect.
	if err := s.records.Save(ctx, record); err != nil {
		s.fail(record, "error.backup_failed")
		return
	}
	s.setCurrent(record)
	uploadErr := objects.Upload(ctx, record.ObjectKey, file, record.SHA256)
	// Even a timed-out PUT may have committed. Verify with a fresh, bounded context.
	verifyCtx, cancel := s.finalContext()
	defer cancel()
	head, headErr := objects.Head(verifyCtx, record.ObjectKey)
	if headErr == nil {
		if matches(record, head) {
			s.succeed(record)
		} else {
			s.fail(record, "error.backup_failed")
		}
	} else if errors.Is(headErr, domain.ErrNotFound) && uploadErr == nil {
		s.fail(record, "error.backup_failed")
	} else {
		// A failed PUT or HEAD is not proof that the object does not exist. Keep the
		// upload intent for reconciliation without holding the process backup lock.
		record.Error = "error.backup_not_ready"
		s.persist(record)
	}
	if record.Status == domain.StatusSucceeded {
		s.cleanup(ctx)
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func matches(record *domain.Record, info domain.ObjectInfo) bool {
	return record.Size > 0 && len(record.SHA256) == 64 && record.Size == info.Size && strings.EqualFold(record.SHA256, info.SHA256)
}

func (s *Service) persist(record *domain.Record) {
	ctx, cancel := s.finalContext()
	defer cancel()
	if err := s.records.Save(ctx, record); err != nil {
		s.mu.Lock()
		s.runtimeError = "error.backup_failed"
		s.mu.Unlock()
	}
	s.setCurrent(record)
}

func (s *Service) fail(record *domain.Record, message string) {
	now := time.Now().UTC()
	record.Status, record.Error, record.FinishedAt = domain.StatusFailed, message, &now
	s.persist(record)
}

func (s *Service) succeed(record *domain.Record) {
	now := time.Now().UTC()
	record.Status, record.Error, record.FinishedAt = domain.StatusSucceeded, "", &now
	s.persist(record)
}

func (s *Service) tempRoot() string {
	base := s.options.TempDir
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "dujiao-next-backup")
}

func (s *Service) prepareTempRoot() (string, error) {
	root := s.tempRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", domain.ErrInvalidConfig
	}
	if err := os.Chmod(root, 0700); err != nil {
		return "", err
	}
	return root, nil
}

func (s *Service) makeTempDir(id string) (string, error) {
	root, err := s.prepareTempRoot()
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "job-"+id+"-")
}

// recoverTemp only removes this service's known job directories, never TempDir itself.
func (s *Service) recoverTemp() {
	root, err := s.prepareTempRoot()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, "job-") || len(name) <= 41 || name[40] != '-' {
			continue
		}
		if _, err := uuid.Parse(name[4:40]); err != nil {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

func (s *Service) Test(ctx context.Context) error {
	if !s.supported() {
		return domain.ErrUnsupported
	}
	if !s.lockOps(ctx) {
		return ctx.Err()
	}
	defer s.ops.Unlock()
	cfg, err := s.objectConfig()
	if err != nil {
		return err
	}
	if s.objects == nil {
		return domain.ErrNotReady
	}
	objects, err := s.objects(cfg)
	if err != nil || objects == nil {
		return domain.ErrInvalidConfig
	}
	dir, err := s.makeTempDir(uuid.NewString())
	if err != nil {
		return errors.New("cannot create backup test file")
	}
	defer os.RemoveAll(dir)
	data := []byte("dujiao-next backup storage test\n")
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	filename := filepath.Join(dir, "probe")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return errors.New("cannot write backup test file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return errors.New("cannot open backup test file")
	}
	defer file.Close()
	key := path.Join(cfg.Prefix, ".test-"+uuid.NewString())
	ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	// Also attempt deletion when Upload fails: a remote write may still have succeeded.
	deleted := false
	defer func() {
		if deleted {
			return
		}
		cleanupCtx, cancel := s.finalContext()
		defer cancel()
		_ = objects.Delete(cleanupCtx, key)
	}()
	if err := objects.Upload(ctx, key, file, checksum); err != nil {
		return errors.New("backup storage test upload failed")
	}
	info, err := objects.Head(ctx, key)
	if err != nil || info.Size != int64(len(data)) || !strings.EqualFold(info.SHA256, checksum) {
		return errors.New("backup storage test verification failed")
	}
	if err := objects.Delete(ctx, key); err != nil {
		return errors.New("backup storage test deletion failed")
	}
	deleted = true
	return nil
}
