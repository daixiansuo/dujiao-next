package application

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
)

func (s *Service) List(ctx context.Context, limit, offset int) ([]domain.Record, int64, error) {
	if s.records == nil {
		return nil, 0, domain.ErrNotReady
	}
	return s.records.List(ctx, min(max(limit, 1), 200), max(offset, 0))
}

func (s *Service) recordObjects(record *domain.Record) (contract.ObjectStore, error) {
	if !s.supported() {
		return nil, domain.ErrUnsupported
	}
	cfg, err := s.objectConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Endpoint != record.Endpoint || cfg.Bucket != record.Bucket {
		return nil, domain.ErrTargetChange
	}
	if s.objects == nil {
		return nil, domain.ErrNotReady
	}
	objects, err := s.objects(cfg)
	if err != nil || objects == nil {
		return nil, domain.ErrInvalidConfig
	}
	return objects, nil
}

func (s *Service) Download(ctx context.Context, id string) (string, error) {
	if !s.lockOps(ctx) {
		return "", ctx.Err()
	}
	defer s.ops.Unlock()
	if s.records == nil {
		return "", domain.ErrNotReady
	}
	record, err := s.records.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if record.Status != domain.StatusSucceeded {
		return "", domain.ErrNotReady
	}
	objects, err := s.recordObjects(record)
	if err != nil {
		return "", err
	}
	return objects.Presign(ctx, record.ObjectKey, record.Filename, 15*time.Minute)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if !s.lockOps(ctx) {
		return ctx.Err()
	}
	defer s.ops.Unlock()
	return s.delete(ctx, id)
}

func (s *Service) delete(ctx context.Context, id string) error {
	if s.records == nil {
		return domain.ErrNotReady
	}
	record, err := s.records.Get(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	active := s.current != nil && s.current.ID == id
	s.mu.Unlock()
	if active || record.Status == domain.StatusRunning {
		return domain.ErrBusy
	}
	if record.ObjectKey != "" {
		objects, err := s.recordObjects(record)
		if err != nil {
			return err
		}
		if err := objects.Delete(ctx, record.ObjectKey); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	// A failed remote deletion keeps its record so users can retry safely.
	return s.records.Delete(ctx, id)
}

func (s *Service) cleanup(ctx context.Context) {
	if !s.lockOps(ctx) {
		return
	}
	defer s.ops.Unlock()
	err := s.retain(ctx)
	s.mu.Lock()
	s.cleanupError = ""
	if err != nil && ctx.Err() == nil {
		s.cleanupError = "error.backup_failed"
	}
	s.mu.Unlock()
}

func (s *Service) retain(ctx context.Context) error {
	if s.records == nil {
		return domain.ErrNotReady
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	if err := validateSchedule(cfg.Config); err != nil {
		return err
	}
	records, err := s.records.Successful(ctx)
	if err != nil {
		return err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	var failures error
	for i := cfg.RetainCount; i < len(records); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.delete(ctx, records[i].ID); err != nil {
			failures = errors.Join(failures, err)
		}
	}
	return failures
}

func (s *Service) reconcile(ctx context.Context) {
	if s.records == nil {
		return
	}
	records, err := s.records.Running(ctx)
	if err != nil {
		return
	}
	for i := range records {
		if ctx.Err() != nil {
			return
		}
		s.reconcileRecord(ctx, &records[i])
	}
}

func (s *Service) reconcileRecord(ctx context.Context, record *domain.Record) {
	if !s.lockOps(ctx) {
		return
	}
	defer s.ops.Unlock()
	s.mu.Lock()
	active := s.current != nil && s.current.ID == record.ID
	s.mu.Unlock()
	if active {
		return
	}
	// Re-fetch inside the operation lock to avoid reviving a record removed by cleanup.
	fresh, err := s.records.Get(ctx, record.ID)
	if err != nil || fresh.Status != domain.StatusRunning {
		return
	}
	record = fresh
	succeeded := false
	if record.Stage == domain.StageUploading {
		objects, err := s.recordObjects(record)
		if err != nil {
			return
		}
		headCtx, cancel := s.shortContext(ctx)
		info, err := objects.Head(headCtx, record.ObjectKey)
		cancel()
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return
		}
		if err == nil {
			succeeded = matches(record, info)
		}
	}
	now := time.Now().UTC()
	record.Status, record.Error, record.FinishedAt = domain.StatusFailed, "error.backup_failed", &now
	if succeeded {
		record.Status, record.Error = domain.StatusSucceeded, ""
	}
	// Shutdown cancellation must not strand an already resolved interrupted record.
	saveCtx, cancel := s.finalContext()
	defer cancel()
	_ = s.records.Save(saveCtx, record)
}
