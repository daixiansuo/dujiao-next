package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/google/uuid"
)

type memorySettings struct {
	mu    sync.Mutex
	value jsonmap.JSON
}

func (m *memorySettings) GetByKey(string) (jsonmap.JSON, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.value == nil {
		return nil, false, nil
	}
	raw, _ := json.Marshal(m.value)
	var value jsonmap.JSON
	_ = json.Unmarshal(raw, &value)
	return value, true, nil
}
func (m *memorySettings) Upsert(_ string, value jsonmap.JSON) (jsonmap.JSON, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, _ := json.Marshal(value)
	_ = json.Unmarshal(raw, &m.value)
	return value, nil
}

type memoryRecords struct {
	mu          sync.Mutex
	rows        map[string]domain.Record
	deleteError error
	saveError   error
}

func (m *memoryRecords) Create(ctx context.Context, r *domain.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.ID] = *copyRecord(r)
	return nil
}
func (m *memoryRecords) Save(ctx context.Context, r *domain.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveError != nil {
		return m.saveError
	}
	if _, ok := m.rows[r.ID]; !ok {
		return domain.ErrNotFound
	}
	m.rows[r.ID] = *copyRecord(r)
	return nil
}
func (m *memoryRecords) Get(ctx context.Context, id string) (*domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyRecord(&r), nil
}
func (m *memoryRecords) snapshot() []domain.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]domain.Record, 0, len(m.rows))
	for _, row := range m.rows {
		result = append(result, *copyRecord(&row))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}
func (m *memoryRecords) List(ctx context.Context, limit, offset int) ([]domain.Record, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	rows := m.snapshot()
	total := int64(len(rows))
	offset = min(offset, len(rows))
	end := min(offset+limit, len(rows))
	return rows[offset:end], total, nil
}
func (m *memoryRecords) Running(ctx context.Context) ([]domain.Record, error) {
	return m.filtered(ctx, domain.StatusRunning)
}
func (m *memoryRecords) Successful(ctx context.Context) ([]domain.Record, error) {
	return m.filtered(ctx, domain.StatusSucceeded)
}
func (m *memoryRecords) filtered(ctx context.Context, status string) ([]domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []domain.Record{}
	for _, row := range m.snapshot() {
		if row.Status == status {
			result = append(result, row)
		}
	}
	return result, nil
}
func (m *memoryRecords) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteError != nil {
		return m.deleteError
	}
	if _, ok := m.rows[id]; !ok {
		return domain.ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

type fakeDumper struct {
	entered    chan struct{}
	release    <-chan struct{}
	dumpError  error
	checkError error
	inspect    func(string) error
}

func (d *fakeDumper) Check(ctx context.Context) (domain.ToolInfo, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolInfo{}, err
	}
	return domain.ToolInfo{PgDumpVersion: "17", ServerVersion: "17"}, d.checkError
}
func (d *fakeDumper) Dump(ctx context.Context, filename string) (domain.ToolInfo, error) {
	if d.entered != nil {
		select {
		case d.entered <- struct{}{}:
		default:
		}
	}
	if d.release != nil {
		select {
		case <-ctx.Done():
			return domain.ToolInfo{}, ctx.Err()
		case <-d.release:
		}
	}
	if d.dumpError != nil {
		return domain.ToolInfo{}, d.dumpError
	}
	if d.inspect != nil {
		if err := d.inspect(filename); err != nil {
			return domain.ToolInfo{}, err
		}
	}
	return domain.ToolInfo{}, os.WriteFile(filename, []byte("PGDMP isolated test archive"), 0600)
}

type fakeObjects struct {
	mu           sync.Mutex
	objects      map[string]domain.ObjectInfo
	uploadError  error
	headError    error
	deleteError  error
	beforeUpload func(string, string) error
	deleted      []string
}

func (o *fakeObjects) Upload(ctx context.Context, key string, file *os.File, checksum string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.beforeUpload != nil {
		if err := o.beforeUpload(key, checksum); err != nil {
			return err
		}
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.objects[key] = domain.ObjectInfo{Size: info.Size(), SHA256: checksum}
	return o.uploadError
}
func (o *fakeObjects) Head(ctx context.Context, key string) (domain.ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return domain.ObjectInfo{}, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.headError != nil {
		return domain.ObjectInfo{}, o.headError
	}
	info, ok := o.objects[key]
	if !ok {
		return info, domain.ErrNotFound
	}
	return info, nil
}
func (o *fakeObjects) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.deleted = append(o.deleted, key)
	if o.deleteError != nil {
		return o.deleteError
	}
	delete(o.objects, key)
	return nil
}
func (o *fakeObjects) Presign(context.Context, string, string, time.Duration) (string, error) {
	return "https://download.invalid/archive", nil
}

func testConfig() domain.Config {
	cfg := domain.DefaultConfig()
	cfg.Endpoint = "https://account.r2.cloudflarestorage.com"
	cfg.Bucket = "test-backups"
	cfg.AccessKeyID = "access-id"
	cfg.SecretAccessKey = "highly-private-secret"
	cfg.Enabled = true
	return cfg
}
func newTestService(t *testing.T) (*Service, *memoryRecords, *fakeObjects, *fakeDumper) {
	t.Helper()
	records := &memoryRecords{rows: map[string]domain.Record{}}
	objects := &fakeObjects{objects: map[string]domain.ObjectInfo{}}
	dumper := &fakeDumper{}
	service := NewService(records, &memorySettings{}, dumper, func(domain.Config) (contract.ObjectStore, error) { return objects, nil }, Options{Driver: "postgres", SecretKey: "application-secret", TempDir: t.TempDir(), Timeout: time.Second})
	if _, err := service.UpdateConfig(testConfig()); err != nil {
		t.Fatal(err)
	}
	return service, records, objects, dumper
}
func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not reached")
}
func runService(t *testing.T, s *Service) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- s.Run(ctx) }()
	eventually(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.ctx != nil })
	t.Cleanup(func() {
		cancel()
		ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := s.Stop(ctx); err != nil {
			t.Errorf("stop: %v", err)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Run did not stop")
		}
	})
	return cancel
}
func waitIdle(t *testing.T, s *Service) {
	t.Helper()
	eventually(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.current == nil })
}

func TestConfigEncryptsMasksAndPreservesSecret(t *testing.T) {
	s, _, _, _ := newTestService(t)
	raw, _, _ := s.settings.GetByKey(domain.SettingKey)
	encoded, _ := json.Marshal(raw)
	if strings.Contains(string(encoded), testConfig().SecretAccessKey) || strings.Contains(string(encoded), "secret_access_key") {
		t.Fatalf("plaintext credential stored: %s", encoded)
	}
	ciphertext := raw["secret_ciphertext"]
	cfg, err := s.GetConfig()
	if err != nil || !cfg.SecretConfigured || cfg.SecretAccessKey != "" {
		t.Fatalf("masked configuration: %+v %v", cfg, err)
	}
	cfg.DailyTime = "07:45"
	if _, err = s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	raw, _, _ = s.settings.GetByKey(domain.SettingKey)
	if raw["secret_ciphertext"] != ciphertext {
		t.Fatal("empty secret replaced the stored ciphertext")
	}
	decrypted, err := s.objectConfig()
	if err != nil || decrypted.SecretAccessKey != testConfig().SecretAccessKey {
		t.Fatal("secret was not recoverable")
	}
	cfg.AccessKeyID = "new-access-id"
	if _, err = s.UpdateConfig(cfg); !errors.Is(err, domain.ErrInvalidConfig) {
		t.Fatalf("credential mismatch allowed: %v", err)
	}
	cfg.SecretAccessKey = "new-secret"
	if _, err = s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRejectsTargetChangeWithRecords(t *testing.T) {
	s, records, _, _ := newTestService(t)
	record := domain.Record{ID: uuid.NewString(), Status: domain.StatusFailed}
	_ = records.Create(context.Background(), &record)
	cfg, _ := s.GetConfig()
	cfg.Bucket = "other-bucket"
	if _, err := s.UpdateConfig(cfg); !errors.Is(err, domain.ErrTargetChange) {
		t.Fatalf("bucket change: %v", err)
	}
	cfg, _ = s.GetConfig()
	cfg.Endpoint = "https://other.example"
	if _, err := s.UpdateConfig(cfg); !errors.Is(err, domain.ErrTargetChange) {
		t.Fatalf("endpoint change: %v", err)
	}
	cfg, _ = s.GetConfig()
	cfg.Prefix = "new-prefix"
	if _, err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestManualAndScheduledShareLockAndCancelPersists(t *testing.T) {
	s, records, _, dumper := newTestService(t)
	block := make(chan struct{})
	dumper.release = block
	dumper.entered = make(chan struct{}, 1)
	cancel := runService(t, s)
	const workers = 16
	var wg sync.WaitGroup
	results := make(chan *domain.Record, workers)
	failures := make(chan error, workers)
	for i := range workers {
		wg.Go(func() {
			source := "manual"
			if i%2 == 0 {
				source = "scheduled"
			}
			record, err := s.StartBackup(source, 17)
			if err != nil {
				failures <- err
			} else {
				results <- record
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	if len(results) != 1 {
		t.Fatalf("started %d concurrent backups", len(results))
	}
	record := <-results
	for err := range failures {
		if !errors.Is(err, domain.ErrBusy) {
			t.Fatalf("unexpected admission error: %v", err)
		}
	}
	if err := s.Delete(context.Background(), record.ID); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("deleted active backup: %v", err)
	}
	<-dumper.entered
	cancel()
	waitIdle(t, s)
	saved, err := records.Get(context.Background(), record.ID)
	if err != nil || saved.Status != domain.StatusFailed || saved.FinishedAt == nil {
		t.Fatalf("cancellation was not persisted: %+v %v", saved, err)
	}
	if saved.Error != "error.backup_failed" {
		t.Fatalf("unexpected failure key: %s", saved.Error)
	}
}

func TestExportPrivateDirectoryMetadataAndSuccessfulDownload(t *testing.T) {
	s, records, objects, dumper := newTestService(t)
	dumper.inspect = func(filename string) error {
		info, err := os.Stat(filepath.Dir(filename))
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0700 {
			return errors.New("non-private temporary directory")
		}
		return nil
	}
	objects.beforeUpload = func(key, hash string) error {
		rows := records.snapshot()
		if len(rows) != 1 || rows[0].Stage != domain.StageUploading || rows[0].Size <= 0 || rows[0].SHA256 != hash || rows[0].ObjectKey != key {
			return errors.New("missing durable upload metadata")
		}
		return nil
	}
	runService(t, s)
	record, err := s.StartBackup("manual", 3)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	saved, _ := records.Get(context.Background(), record.ID)
	if saved.Status != domain.StatusSucceeded || saved.FinishedAt == nil {
		t.Fatalf("backup failed: %+v", saved)
	}
	hash := sha256.Sum256([]byte("PGDMP isolated test archive"))
	if saved.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("incorrect archive hash")
	}
	entries, err := os.ReadDir(s.tempRoot())
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary archives remained: %v %v", entries, err)
	}
	url, err := s.Download(context.Background(), record.ID)
	if err != nil || url == "" {
		t.Fatalf("download: %s %v", url, err)
	}
	if err := s.Delete(context.Background(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := records.Get(context.Background(), record.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted record remained")
	}
}

func TestUncertainUploadStaysPendingAndDoesNotBlockNextBackup(t *testing.T) {
	s, records, objects, _ := newTestService(t)
	objects.uploadError = errors.New("request with highly-private-secret timed out")
	objects.headError = errors.New("storage temporarily unavailable")
	runService(t, s)
	first, err := s.StartBackup("manual", 1)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	pending, _ := records.Get(context.Background(), first.ID)
	if pending.Status != domain.StatusRunning || pending.Stage != domain.StageUploading || pending.Size == 0 || len(pending.SHA256) != 64 || pending.FinishedAt != nil {
		t.Fatalf("lost ambiguous upload intent: %+v", pending)
	}
	if strings.Contains(pending.Error, "highly-private") {
		t.Fatal("credential leaked into record")
	}
	second, err := s.StartBackup("scheduled", 0)
	if err != nil {
		t.Fatalf("pending record blocked new job: %v", err)
	}
	waitIdle(t, s)
	objects.mu.Lock()
	objects.headError = nil
	objects.uploadError = nil
	objects.mu.Unlock()
	s.reconcile(context.Background())
	for _, id := range []string{first.ID, second.ID} {
		r, _ := records.Get(context.Background(), id)
		if r.Status != domain.StatusSucceeded {
			t.Fatalf("reconciliation failed: %+v", r)
		}
	}
}

func TestStartupReconcilesInterruptedAndRetainsUnknown(t *testing.T) {
	s, records, objects, _ := newTestService(t)
	cfg := testConfig()
	hash := strings.Repeat("a", 64)
	for _, row := range []domain.Record{
		{ID: "exporting", Status: domain.StatusRunning, Stage: domain.StageExporting},
		{ID: "uploaded", Status: domain.StatusRunning, Stage: domain.StageUploading, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, ObjectKey: "exists", Size: 12, SHA256: hash},
		{ID: "missing", Status: domain.StatusRunning, Stage: domain.StageUploading, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, ObjectKey: "missing", Size: 12, SHA256: hash},
	} {
		r := row
		_ = records.Create(context.Background(), &r)
	}
	objects.objects["exists"] = domain.ObjectInfo{Size: 12, SHA256: hash}
	runService(t, s)
	eventually(t, func() bool {
		r, _ := records.Get(context.Background(), "missing")
		return r.Status == domain.StatusFailed
	})
	exported, _ := records.Get(context.Background(), "exporting")
	uploaded, _ := records.Get(context.Background(), "uploaded")
	if exported.Status != domain.StatusFailed || uploaded.Status != domain.StatusSucceeded {
		t.Fatalf("startup recovery: %+v %+v", exported, uploaded)
	}
	pending := domain.Record{ID: "uncertain", Status: domain.StatusRunning, Stage: domain.StageUploading, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, ObjectKey: "unknown", Size: 12, SHA256: hash}
	_ = records.Create(context.Background(), &pending)
	objects.mu.Lock()
	objects.headError = errors.New("remote unavailable")
	objects.mu.Unlock()
	s.reconcile(context.Background())
	saved, _ := records.Get(context.Background(), pending.ID)
	if saved.Status != domain.StatusRunning {
		t.Fatal("unknown remote result was discarded")
	}
}

func TestRetentionCountsOnlySuccessAndKeepsDeleteFailures(t *testing.T) {
	s, records, objects, _ := newTestService(t)
	cfg := testConfig()
	cfg.RetainCount = 2
	if _, err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, id := range []string{"newest", "second", "old", "oldest"} {
		r := domain.Record{ID: id, Status: domain.StatusSucceeded, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, ObjectKey: id, CreatedAt: now.Add(-time.Duration(i) * time.Hour)}
		_ = records.Create(context.Background(), &r)
	}
	failed := domain.Record{ID: "failure", Status: domain.StatusFailed, CreatedAt: now.Add(time.Hour)}
	_ = records.Create(context.Background(), &failed)
	objects.deleteError = errors.New("R2 delete rejected")
	s.cleanup(context.Background())
	if len(records.snapshot()) != 5 {
		t.Fatal("failed deletion removed a record")
	}
	s.mu.Lock()
	cleanupError := s.cleanupError
	s.mu.Unlock()
	if cleanupError == "" {
		t.Fatal("cleanup failure was not exposed")
	}
	objects.mu.Lock()
	objects.deleteError = nil
	objects.mu.Unlock()
	s.cleanup(context.Background())
	rows := records.snapshot()
	if len(rows) != 3 {
		t.Fatalf("incorrect retention: %+v", rows)
	}
	for _, id := range []string{"newest", "second", "failure"} {
		if _, err := records.Get(context.Background(), id); err != nil {
			t.Fatalf("retained record %s removed", id)
		}
	}
}

func TestScheduleHotReloadDisableAndIndependentCleanup(t *testing.T) {
	s, _, _, _ := newTestService(t)
	runService(t, s)
	eventually(t, func() bool { status, _ := s.Status(context.Background()); return status.NextRun != nil })
	before, _ := s.Status(context.Background())
	cfg, _ := s.GetConfig()
	cfg.DailyTime = "11:27"
	cfg.Timezone = "UTC"
	if _, err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		status, _ := s.Status(context.Background())
		return status.NextRun != nil && status.NextRun.In(time.UTC).Hour() == 11 && status.NextRun.In(time.UTC).Minute() == 27
	})
	after, _ := s.Status(context.Background())
	if before.NextRun.Equal(*after.NextRun) {
		t.Fatal("schedule was not changed")
	}
	s.mu.Lock()
	id := s.scheduleID
	s.mu.Unlock()
	s.reloadSchedule(context.Background(), s.scheduler)
	s.mu.Lock()
	same := s.scheduleID == id
	s.mu.Unlock()
	if !same {
		t.Fatal("unchanged configuration unnecessarily reset schedule")
	}
	cfg.Enabled = false
	if _, err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { status, _ := s.Status(context.Background()); return status.NextRun == nil })
	s.mu.Lock()
	entries := len(s.scheduler.Entries())
	s.mu.Unlock()
	if entries != 1 {
		t.Fatalf("cleanup schedule lost when disabling backups: %d", entries)
	}
	if _, err := s.StartBackup("scheduled", 0); !errors.Is(err, domain.ErrNotReady) {
		t.Fatalf("disabled schedule admitted job: %v", err)
	}
}

func TestInvalidSettingsOrMissingToolDoesNotExitRun(t *testing.T) {
	for _, mode := range []string{"invalid-config", "missing-tool", "sqlite"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _, dumper := newTestService(t)
			switch mode {
			case "invalid-config":
				cfg := testConfig()
				cfg.DailyTime = "broken"
				raw, _ := json.Marshal(storedConfig{Config: cfg})
				var value jsonmap.JSON
				_ = json.Unmarshal(raw, &value)
				_, _ = s.settings.Upsert(domain.SettingKey, value)
			case "missing-tool":
				dumper.checkError = errors.New("executable not found")
			case "sqlite":
				s.options.Driver = "sqlite"
			}
			runService(t, s)
			status, err := s.Status(context.Background())
			if err != nil || status.Ready {
				t.Fatalf("unexpected readiness: %+v %v", status, err)
			}
			s.mu.Lock()
			alive := s.ctx != nil && s.ctx.Err() == nil
			s.mu.Unlock()
			if !alive {
				t.Fatal("backup unavailability stopped Run")
			}
		})
	}
}

func TestStorageProbeActuallyUploadsHeadsAndDeletes(t *testing.T) {
	s, _, objects, _ := newTestService(t)
	if err := s.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	defer objects.mu.Unlock()
	if len(objects.objects) != 0 || len(objects.deleted) != 1 {
		t.Fatalf("test left remote objects or skipped deletion: %+v %v", objects.objects, objects.deleted)
	}
}

func TestRecoverTempOnlyRemovesDedicatedJobDirectories(t *testing.T) {
	s, _, _, _ := newTestService(t)
	root, err := s.prepareTempRoot()
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "job-"+uuid.NewString()+"-123")
	other := filepath.Join(root, "user-files")
	outside := filepath.Join(s.options.TempDir, "keep")
	for _, dir := range []string{stale, other, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "job-"+uuid.NewString()+"-link")); err != nil {
		t.Fatal(err)
	}
	s.recoverTemp()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale private job directory survived")
	}
	for _, dir := range []string{other, outside} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("unrelated path removed: %s", dir)
		}
	}
}

func TestUploadDoesNotStartWithoutDurableMetadata(t *testing.T) {
	s, records, objects, _ := newTestService(t)
	records.saveError = errors.New("database unavailable")
	uploads := 0
	objects.beforeUpload = func(string, string) error { uploads++; return nil }
	runService(t, s)
	if _, err := s.StartBackup("manual", 1); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)
	if uploads != 0 {
		t.Fatal("uploaded an archive without durable size and checksum")
	}
}

func TestDeleteFailurePreservesRecordAndMissingObjectCanBeRemoved(t *testing.T) {
	s, records, objects, _ := newTestService(t)
	cfg := testConfig()
	record := domain.Record{ID: "delete-retry", Status: domain.StatusFailed, Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, ObjectKey: "stale-upload"}
	if err := records.Create(context.Background(), &record); err != nil {
		t.Fatal(err)
	}
	objects.deleteError = errors.New("R2 unavailable")
	if err := s.Delete(context.Background(), record.ID); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if _, err := records.Get(context.Background(), record.ID); err != nil {
		t.Fatal("lost the retry record")
	}
	objects.deleteError = domain.ErrNotFound
	if err := s.Delete(context.Background(), record.ID); err != nil {
		t.Fatalf("could not remove record whose object is absent: %v", err)
	}
}

func TestStorageProbeReportsDeleteFailure(t *testing.T) {
	s, _, objects, _ := newTestService(t)
	objects.deleteError = errors.New("storage permission denied")
	if err := s.Test(context.Background()); err == nil {
		t.Fatal("storage test hid missing delete permission")
	}
	objects.mu.Lock()
	defer objects.mu.Unlock()
	if len(objects.objects) != 1 {
		t.Fatal("failed deletion unexpectedly removed test object")
	}
}

func TestStopCancelsSchedulerWaitingForOperations(t *testing.T) {
	s, _, _, _ := newTestService(t)
	runService(t, s)
	s.ops.Lock()
	started := make(chan struct{})
	s.mu.Lock()
	scheduler, life := s.scheduler, s.ctx
	s.mu.Unlock()
	_, err := scheduler.AddFunc("@every 1s", func() { close(started); _, _ = s.StartBackup("scheduled", 0) })
	if err != nil {
		s.ops.Unlock()
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		s.ops.Unlock()
		t.Fatal("scheduler callback did not start")
	}
	// Reconciliation and schedule reload also wait on this same lock.
	reloadDone := make(chan struct{})
	go func() { s.reloadSchedule(life, scheduler); close(reloadDone) }()
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stopErr := s.Stop(stopCtx)
	s.ops.Unlock()
	if stopErr != nil {
		t.Fatalf("shutdown waited on an unrelated storage operation: %v", stopErr)
	}
	select {
	case <-reloadDone:
	case <-time.After(time.Second):
		t.Fatal("reload ignored lifecycle cancellation")
	}
}

type canceledUploadObjects struct {
	*fakeObjects
	entered chan struct{}
}

func (o *canceledUploadObjects) Upload(ctx context.Context, key string, file *os.File, checksum string) error {
	// Simulate a PUT that committed remotely, but whose response is lost at shutdown.
	if err := o.fakeObjects.Upload(ctx, key, file, checksum); err != nil {
		return err
	}
	close(o.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestShutdownReconcilesCommittedUploadWithIndependentContext(t *testing.T) {
	s, records, objects, _ := newTestService(t)
	remote := &canceledUploadObjects{fakeObjects: objects, entered: make(chan struct{})}
	s.objects = func(domain.Config) (contract.ObjectStore, error) { return remote, nil }
	runService(t, s)
	record, err := s.StartBackup("manual", 5)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-remote.entered:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	saved, err := records.Get(context.Background(), record.ID)
	if err != nil || saved.Status != domain.StatusSucceeded || saved.FinishedAt == nil {
		t.Fatalf("committed upload was lost during shutdown: %+v %v", saved, err)
	}
}
