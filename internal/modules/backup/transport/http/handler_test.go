package backuphttp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dujiao-next/internal/modules/backup/contract"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	backupapp "github.com/dujiao-next/internal/modules/backup/application"
	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/gin-gonic/gin"
)

type testSettings struct{ value jsonmap.JSON }

func (s *testSettings) GetByKey(string) (jsonmap.JSON, bool, error) {
	return s.value, s.value != nil, nil
}
func (s *testSettings) Upsert(_ string, value jsonmap.JSON) (jsonmap.JSON, error) {
	s.value = value
	return value, nil
}

type emptyRecords struct{}

func (emptyRecords) Create(context.Context, *domain.Record) error { return nil }
func (emptyRecords) Save(context.Context, *domain.Record) error   { return nil }
func (emptyRecords) Get(context.Context, string) (*domain.Record, error) {
	return nil, domain.ErrNotFound
}
func (emptyRecords) List(context.Context, int, int) ([]domain.Record, int64, error) {
	return nil, 0, nil
}
func (emptyRecords) Running(context.Context) ([]domain.Record, error)    { return nil, nil }
func (emptyRecords) Successful(context.Context) ([]domain.Record, error) { return nil, nil }
func (emptyRecords) Delete(context.Context, string) error                { return domain.ErrNotFound }

type responseBody struct {
	StatusCode int             `json:"status_code"`
	Msg        string          `json:"msg"`
	Data       json.RawMessage `json:"data"`
}

func request(t *testing.T, router http.Handler, method, path, body, locale string) responseBody {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", locale)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var result responseBody
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func newRouter(service *backupapp.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) { c.Set("admin_id", uint(42)) })
	RegisterAdminRoutes(admin, NewHandler(service))
	return router
}

func TestAdminRoutesAndNestedListResponse(t *testing.T) {
	service := backupapp.NewService(emptyRecords{}, &testSettings{}, nil, nil, backupapp.Options{Driver: "postgres", SecretKey: "test"})
	router := newRouter(service)
	if got := len(router.Routes()); got != 8 {
		t.Fatalf("registered %d routes", got)
	}
	result := request(t, router, "GET", "/api/v1/admin/backups/records?page=2&page_size=7", "", "en-US")
	if result.StatusCode != 0 {
		t.Fatal(result.Msg)
	}
	var page struct {
		Items    []domain.Record `json:"items"`
		Total    int64           `json:"total"`
		Page     int             `json:"page"`
		PageSize int             `json:"page_size"`
	}
	if err := json.Unmarshal(result.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.Total != 0 || page.Page != 2 || page.PageSize != 7 {
		t.Fatalf("unexpected list response: %s", result.Data)
	}
	for _, route := range router.Routes() {
		if strings.Contains(route.Path, "/admin/admin/") {
			t.Fatalf("duplicated admin prefix: %s", route.Path)
		}
	}
}

func TestConfigHTTPNeverReturnsSecretAndMalformedConfigUsesBackupKey(t *testing.T) {
	settings := &testSettings{}
	service := backupapp.NewService(emptyRecords{}, settings, nil, nil, backupapp.Options{Driver: "postgres", SecretKey: "test"})
	router := newRouter(service)
	cfg := domain.DefaultConfig()
	cfg.Endpoint = "http://localhost:9000"
	cfg.Bucket = "test"
	cfg.AccessKeyID = "access"
	cfg.SecretAccessKey = "never-return-this-secret"
	raw, _ := json.Marshal(cfg)
	saved := request(t, router, "PUT", "/api/v1/admin/backups/config", string(raw), "en-US")
	if saved.StatusCode != 0 {
		t.Fatal(saved.Msg)
	}
	got := request(t, router, "GET", "/api/v1/admin/backups/config", "", "en-US")
	for _, value := range []json.RawMessage{saved.Data, got.Data} {
		if strings.Contains(string(value), cfg.SecretAccessKey) || strings.Contains(string(value), "secret_ciphertext") {
			t.Fatal("secret exposed in HTTP response")
		}
		var masked domain.Config
		_ = json.Unmarshal(value, &masked)
		if !masked.SecretConfigured || masked.SecretAccessKey != "" {
			t.Fatalf("unexpected masking: %s", value)
		}
	}
	invalid := request(t, router, "PUT", "/api/v1/admin/backups/config", `{"retain_count":"oops"}`, "en-US")
	if invalid.StatusCode != 400 || invalid.Msg != "Invalid backup configuration" {
		t.Fatalf("invalid config response: %+v", invalid)
	}
}

func TestStatusLocalizesErrorAndDownloadNotFound(t *testing.T) {
	service := backupapp.NewService(emptyRecords{}, &testSettings{}, nil, nil, backupapp.Options{Driver: "sqlite"})
	router := newRouter(service)
	result := request(t, router, "GET", "/api/v1/admin/backups/status", "", "en-US")
	var status domain.RuntimeStatus
	if err := json.Unmarshal(result.Data, &status); err != nil {
		t.Fatal(err)
	}
	if status.Error != "Database backups require PostgreSQL" || status.Ready || status.Supported {
		t.Fatalf("runtime status not localized: %+v", status)
	}
	result = request(t, router, "POST", "/api/v1/admin/backups/records/missing/download", "{}", "zh-CN")
	if result.StatusCode != 404 || result.Msg != "备份记录不存在" {
		t.Fatalf("missing response: %+v", result)
	}
}

func TestErrorResponsesDoNotLeakUnderlyingCredentials(t *testing.T) {
	router := gin.New()
	router.GET("/failure", func(c *gin.Context) { respondError(c, errors.New("postgres://user:secret@localhost/db")) })
	result := request(t, router, "GET", "/failure", "", "en-US")
	if result.StatusCode != 500 || result.Msg != "Backup operation failed" || strings.Contains(string(result.Data), "secret") {
		t.Fatalf("unsafe response: %+v", result)
	}
}

type failingDumper struct{}

func (failingDumper) Check(context.Context) (domain.ToolInfo, error) {
	return domain.ToolInfo{PgDumpVersion: "17", ServerVersion: "17"}, nil
}
func (failingDumper) Dump(context.Context, string) (domain.ToolInfo, error) {
	return domain.ToolInfo{}, errors.New("database export failed: postgres://user:never-expose-password@host/db")
}

type unusedObjects struct{}

func (unusedObjects) Upload(context.Context, string, *os.File, string) error {
	return errors.New("unexpected upload after export failure")
}
func (unusedObjects) Head(context.Context, string) (domain.ObjectInfo, error) {
	return domain.ObjectInfo{}, domain.ErrNotFound
}
func (unusedObjects) Delete(context.Context, string) error { return nil }
func (unusedObjects) Presign(context.Context, string, string, time.Duration) (string, error) {
	return "", domain.ErrNotReady
}

func TestFailedBackupHTTPReturnsChineseDiagnosisWithoutRawProcessError(t *testing.T) {
	records := &persistedRecords{}
	service := backupapp.NewService(records, &testSettings{}, failingDumper{}, func(domain.Config) (contract.ObjectStore, error) { return unusedObjects{}, nil }, backupapp.Options{Driver: "postgres", SecretKey: "application-secret", TempDir: t.TempDir(), Timeout: time.Second})
	cfg := domain.DefaultConfig()
	cfg.Endpoint, cfg.Bucket, cfg.AccessKeyID, cfg.SecretAccessKey = "https://r2.example.com", "backups", "test-id", "test-secret"
	if _, err := service.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- service.Run(ctx) }()
	defer func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		if err := service.Stop(stopCtx); err != nil {
			t.Error(err)
		}
		<-runDone
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := service.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.Ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("service not ready: %+v", status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	router := newRouter(service)
	started := request(t, router, "POST", "/api/v1/admin/backups/records", "{}", "zh-CN")
	if started.StatusCode != 0 {
		t.Fatal(started.Msg)
	}
	var record domain.Record
	if err := json.Unmarshal(started.Data, &record); err != nil {
		t.Fatal(err)
	}
	if record.AdminID != 42 {
		t.Fatalf("lost administrator ID: %d", record.AdminID)
	}
	var page struct {
		Items []domain.Record `json:"items"`
	}
	for {
		result := request(t, router, "GET", "/api/v1/admin/backups/records", "", "zh-CN")
		if result.StatusCode != 0 {
			t.Fatal(result.Msg)
		}
		if err := json.Unmarshal(result.Data, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 && page.Items[0].Status == domain.StatusFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backup did not fail: %s", result.Data)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if page.Items[0].Error != "备份操作失败" {
		t.Fatalf("Chinese response retained an untranslated diagnosis: %+v", page.Items[0])
	}
	raw, err := records.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Error != "error.backup_failed" {
		t.Fatalf("stored a raw process error: %s", raw.Error)
	}
	if strings.Contains(page.Items[0].Error, "never-expose-password") || strings.Contains(page.Items[0].Error, "database export failed") {
		t.Fatal("process stderr leaked")
	}
}

func TestLegacyRecordDiagnosesAreLocalized(t *testing.T) {
	for _, message := range []string{"database export failed", "database backup canceled", "upload outcome pending verification", "uploaded database verification failed"} {
		if got := localized("zh-CN", message); got == message || strings.HasPrefix(got, "error.") {
			t.Fatalf("legacy diagnosis was not localized: %q -> %q", message, got)
		}
	}
}

// persistedRecords implements the record port while allowing the asynchronous
// service to persist the failure reached through an actual handler request.
type persistedRecords struct {
	emptyRecords
	mu     sync.Mutex
	record *domain.Record
}

func (r *persistedRecords) Create(ctx context.Context, record *domain.Record) error {
	return r.Save(ctx, record)
}
func (r *persistedRecords) Save(ctx context.Context, record *domain.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value := *record
	r.record = &value
	return nil
}
func (r *persistedRecords) Get(ctx context.Context, id string) (*domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record == nil || r.record.ID != id {
		return nil, domain.ErrNotFound
	}
	result := *r.record
	return &result, nil
}
func (r *persistedRecords) List(ctx context.Context, _, _ int) ([]domain.Record, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record == nil {
		return []domain.Record{}, 0, nil
	}
	return []domain.Record{*r.record}, 1, nil
}
