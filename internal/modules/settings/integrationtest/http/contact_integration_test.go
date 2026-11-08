package http_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsstore "github.com/dujiao-next/internal/modules/settings/infrastructure/gormstore"
	settingshttp "github.com/dujiao-next/internal/modules/settings/transport/http"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise the real HTTP handler, normalizer and JSON store, including reads
// through a new database connection after each write.
func TestSiteContactPersistsAcrossReload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			open := contactDatabase(t, driver)
			db := open()
			if err := db.AutoMigrate(&settingsstore.SettingRecord{}); err != nil {
				t.Fatal(err)
			}

			contacts := []map[string]interface{}{
				{"telegram": "https://t.me/support", "whatsapp": "https://wa.me/123456789", "email": "support@example.com", "x": "https://x.com/example"},
				{"telegram": "https://t.me/support", "whatsapp": "https://wa.me/123456789", "email": "help@example.com", "x": "https://twitter.com/support"},
				{"telegram": "https://t.me/support", "whatsapp": "https://wa.me/123456789", "email": "", "x": ""},
			}
			for _, want := range contacts {
				server := contactServer(t, db)
				body, err := json.Marshal(map[string]interface{}{
					"key": constants.SettingKeySiteConfig, "value": map[string]interface{}{"contact": want},
				})
				if err != nil {
					t.Fatal(err)
				}
				saved := requestContact(t, server.URL, http.MethodPut, body)
				if !reflect.DeepEqual(saved["contact"], want) {
					t.Fatalf("save response contact = %#v, want %#v", saved["contact"], want)
				}
				server.Close()
				closeContactDatabase(t, db)

				db = open()
				server = contactServer(t, db)
				loaded := requestContact(t, server.URL, http.MethodGet, nil)
				server.Close()
				if !reflect.DeepEqual(loaded["contact"], want) {
					t.Fatalf("reloaded contact = %#v, want %#v", loaded["contact"], want)
				}
				public, err := settingsapp.NewService(settingsstore.New(db)).GetConfig(nil)
				if err != nil || !reflect.DeepEqual(public["contact"], want) {
					t.Fatalf("public contact = %#v, want %#v; err = %v", public["contact"], want, err)
				}
			}
			closeContactDatabase(t, db)
		})
	}
}

func contactDatabase(t *testing.T, driver string) func() *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "settings.db")
	var pgConfig *pgx.ConnConfig
	if driver == "postgres" {
		dsn = os.Getenv("SETTINGS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set SETTINGS_TEST_POSTGRES_DSN to test real PostgreSQL persistence")
		}
		var err error
		pgConfig, err = pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal("invalid PostgreSQL test DSN")
		}
		admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal("connect PostgreSQL test database")
		}
		schema := fmt.Sprintf("settings_contact_test_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			closeContactDatabase(t, admin)
			t.Fatalf("create isolated test schema: %v", err)
		}
		t.Cleanup(func() {
			if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
				t.Errorf("remove test schema: %v", err)
			}
			closeContactDatabase(t, admin)
		})
		pgConfig.RuntimeParams["search_path"] = schema
	}
	return func() *gorm.DB {
		t.Helper()
		var dialector gorm.Dialector = sqlite.Open(dsn)
		if pgConfig != nil {
			pool := stdlib.OpenDB(*pgConfig.Copy())
			t.Cleanup(func() { _ = pool.Close() })
			dialector = postgres.New(postgres.Config{Conn: pool})
		}
		db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal("open settings test database")
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
}

func closeContactDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}

func contactServer(t *testing.T, db *gorm.DB) *httptest.Server {
	t.Helper()
	router := gin.New()
	handler := settingshttp.NewAdminHandler(settingsapp.NewService(settingsstore.New(db)))
	settingshttp.RegisterAdminRoutes(router.Group("/api/v1/admin"), handler)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func requestContact(t *testing.T, baseURL, method string, body []byte) jsonmap.JSON {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+"/api/v1/admin/settings?key="+constants.SettingKeySiteConfig, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var result struct {
		StatusCode int          `json:"status_code"`
		Data       jsonmap.JSON `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || result.StatusCode != 0 {
		t.Fatalf("%s settings: HTTP %d, status_code %d", method, res.StatusCode, result.StatusCode)
	}
	return result.Data
}
