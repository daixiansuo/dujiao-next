package gormstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteRecordStoreLifecycleAndOrdering(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "records.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&domain.Record{}); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, record := range []domain.Record{
		{ID: "old", Status: domain.StatusSucceeded, CreatedAt: now.Add(-time.Hour)},
		{ID: "new-b", Status: domain.StatusSucceeded, CreatedAt: now},
		{ID: "new-a", Status: domain.StatusSucceeded, CreatedAt: now},
		{ID: "running", Status: domain.StatusRunning, Stage: domain.StageExporting, CreatedAt: now.Add(time.Hour)},
		{ID: "failed", Status: domain.StatusFailed, CreatedAt: now.Add(2 * time.Hour)},
	} {
		r := record
		if err := store.Create(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}
	list, total, err := store.List(ctx, 2, 1)
	if err != nil || total != 5 || len(list) != 2 || list[0].ID != "running" || list[1].ID != "new-b" {
		t.Fatalf("paginated order: %+v %d %v", list, total, err)
	}
	successful, err := store.Successful(ctx)
	if err != nil || len(successful) != 3 || successful[0].ID != "new-b" || successful[2].ID != "old" {
		t.Fatalf("successful order: %+v %v", successful, err)
	}
	running, err := store.Running(ctx)
	if err != nil || len(running) != 1 {
		t.Fatalf("running: %+v %v", running, err)
	}
	record := running[0]
	record.Status = domain.StatusFailed
	record.Stage = ""
	record.Size = 0
	record.Error = "error.backup_failed"
	record.FinishedAt = &now
	if err := store.Save(ctx, &record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, record.ID)
	if err != nil || got.Stage != "" || got.Status != domain.StatusFailed || got.FinishedAt == nil {
		t.Fatalf("zero-value update: %+v %v", got, err)
	}
	if err := store.Delete(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, record.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
	if err := store.Save(ctx, &record); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Save revived deleted record: %v", err)
	}
	if err := store.Delete(ctx, record.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.List(canceled, 20, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("ignored cancellation: %v", err)
	}
}
