package gormstore

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	siteconnectioncontract "github.com/dujiao-next/internal/modules/siteconnection/contract"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupSiteConnectionStoreTest(t *testing.T) (*gorm.DB, *Store) {
	t.Helper()
	dsn := fmt.Sprintf("file:siteconnection_store_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&siteconnectiondomain.Connection{}); err != nil {
		t.Fatalf("migrate site connection: %v", err)
	}
	return db, New(db)
}

func TestStoreDeleteHidesConnectionFromEveryReadPath(t *testing.T) {
	db, store := setupSiteConnectionStoreTest(t)
	connection := &siteconnectiondomain.Connection{
		Name:               "primary upstream",
		BaseURL:            "https://upstream.example.com",
		ApiKey:             "upstream-key",
		ApiSecret:          "encrypted-secret",
		Protocol:           constants.ConnectionProtocolDujiaoNext,
		Status:             constants.ConnectionStatusActive,
		RetryMax:           5,
		RetryIntervals:     "[30,60,300]",
		ExchangeRate:       decimal.NewFromInt(1),
		PriceMarkupPercent: decimal.Zero,
		PriceRoundingMode:  "none",
	}
	if err := store.Create(connection); err != nil {
		t.Fatalf("create connection: %v", err)
	}

	if err := store.Delete(connection.ID); err != nil {
		t.Fatalf("delete connection: %v", err)
	}

	if got, err := store.GetByID(connection.ID); err != nil || got != nil {
		t.Fatalf("GetByID after delete = %#v, %v; want nil, nil", got, err)
	}
	if got, err := store.GetByApiKey(connection.ApiKey); err != nil || got != nil {
		t.Fatalf("GetByApiKey after delete = %#v, %v; want nil, nil", got, err)
	}
	listed, total, err := store.List(siteconnectioncontract.ListFilter{})
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if total != 0 || len(listed) != 0 {
		t.Fatalf("list after delete = total %d rows %d; want empty", total, len(listed))
	}
	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("list active connections: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active list after delete has %d rows; want empty", len(active))
	}

	var persisted siteconnectiondomain.Connection
	if err := db.Where("id = ?", connection.ID).First(&persisted).Error; err != nil {
		t.Fatalf("load persisted deleted connection: %v", err)
	}
	if persisted.DeletedAt == nil {
		t.Fatal("delete must persist deleted_at instead of physically removing the row")
	}
}

func TestConnectionMarkupMigrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "connections.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	testConnectionMarkupMigration(t, db)
}

func TestConnectionMarkupMigrationPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	// Always isolate this test from existing application tables.
	schema := fmt.Sprintf("test_connection_markup_%d", time.Now().UnixNano())
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if err := db.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	testConnectionMarkupMigration(t, db)
}

func testConnectionMarkupMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&siteconnectiondomain.Connection{}); err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-feature schema with existing percentage configuration.
	for _, column := range []string{"price_markup_mode", "price_markup_amount", "price_markup_threshold"} {
		if err := db.Migrator().DropColumn(&siteconnectiondomain.Connection{}, column); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &siteconnectiondomain.Connection{Name: "legacy", BaseURL: "https://example.com", ApiKey: "key", ApiSecret: "encrypted", ExchangeRate: decimal.RequireFromString("7.2"), PriceMarkupPercent: decimal.NewFromInt(40)}
	if err := db.Omit("PriceMarkupMode", "PriceMarkupAmount", "PriceMarkupThreshold").Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&siteconnectiondomain.Connection{}); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	conn, err := store.GetByID(legacy.ID)
	if err != nil || conn == nil {
		t.Fatalf("get migrated connection: %v, %+v", err, conn)
	}
	if conn.PriceMarkupMode != constants.ConnectionPriceMarkupPercent || !conn.PriceMarkupAmount.IsZero() || !conn.PriceMarkupThreshold.IsZero() || !conn.PriceMarkupPercent.Equal(decimal.NewFromInt(40)) || !conn.ExchangeRate.Equal(legacy.ExchangeRate) {
		t.Fatalf("migration lost pricing: %+v", conn)
	}
	conn.PriceMarkupMode = constants.ConnectionPriceMarkupFixedAmount
	conn.PriceMarkupAmount = money.FromDecimal(decimal.RequireFromString("3.25"))
	if err := store.Update(conn); err != nil {
		t.Fatal(err)
	}
	// Simulate upgrading an existing fixed-mode connection from before segmented markup.
	if err := db.Migrator().DropColumn(&siteconnectiondomain.Connection{}, "price_markup_threshold"); err != nil {
		t.Fatal(err)
	}
	// Startup migration is repeatable and preserves both active and inactive values.
	if err := db.AutoMigrate(&siteconnectiondomain.Connection{}); err != nil {
		t.Fatal(err)
	}
	rows, total, err := store.List(siteconnectioncontract.ListFilter{})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("list: %v, %d, %+v", err, total, rows)
	}
	conn = &rows[0]
	if conn.PriceMarkupMode != constants.ConnectionPriceMarkupFixedAmount || conn.PriceMarkupAmount.String() != "3.25" || !conn.PriceMarkupThreshold.IsZero() || !conn.PriceMarkupPercent.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("save/list lost pricing: %+v", conn)
	}
	conn.PriceMarkupMode = constants.ConnectionPriceMarkupSegmented
	conn.PriceMarkupThreshold = money.FromDecimal(decimal.NewFromInt(15))
	conn.PriceMarkupAmount = money.FromDecimal(decimal.NewFromInt(5))
	conn.PriceMarkupPercent = decimal.NewFromInt(30)
	if err := store.Update(conn); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&siteconnectiondomain.Connection{}); err != nil {
		t.Fatal(err)
	}
	reread, err := store.GetByID(conn.ID)
	if err != nil || reread == nil || reread.PriceMarkupMode != constants.ConnectionPriceMarkupSegmented || reread.PriceMarkupThreshold.String() != "15.00" || reread.PriceMarkupAmount.String() != "5.00" || !reread.PriceMarkupPercent.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("segmented migration/save lost pricing: %v, %+v", err, reread)
	}
	conn.PriceMarkupAmount = money.FromDecimal(decimal.Zero)
	if err := store.Update(conn); err != nil {
		t.Fatal(err)
	}
	reread, err = store.GetByID(conn.ID)
	if err != nil || reread == nil || !reread.PriceMarkupAmount.IsZero() || reread.PriceMarkupThreshold.String() != "15.00" {
		t.Fatalf("zero not persisted: %v, %+v", err, reread)
	}
}
