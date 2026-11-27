package siteconnection_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/i18n"
	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	siteconnectiongormstore "github.com/dujiao-next/internal/modules/siteconnection/infrastructure/gormstore"
	siteconnectionhttp "github.com/dujiao-next/internal/modules/siteconnection/transport/http"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAdminConnectionMarkupSaveAndReload(t *testing.T) {
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
	if err := db.AutoMigrate(&siteconnectiondomain.Connection{}); err != nil {
		t.Fatal(err)
	}
	svc := siteconnectionapp.NewService(siteconnectiongormstore.New(db), "test-secret", t.TempDir())
	reapplier := &fakeMarkupReapplier{}
	svc.SetMarkupReapplier(reapplier)
	handler := siteconnectionhttp.NewAdminHandler(svc, reapplier)
	router := gin.New()
	router.POST("/connections", handler.CreateSiteConnection)
	router.GET("/connections", handler.GetSiteConnections)
	router.GET("/connections/:id", handler.GetSiteConnection)
	router.PUT("/connections/:id", handler.UpdateSiteConnection)
	request := func(method, path, body string) response.Response {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("http status %d: %s", recorder.Code, recorder.Body.String())
		}
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	created := request(http.MethodPost, "/connections", `{"name":"upstream","base_url":"https://example.com","api_key":"key","api_secret":"secret","price_markup_mode":"fixed_amount","price_markup_amount":"3.25","price_markup_percent":40}`)
	if created.StatusCode != 0 {
		t.Fatalf("create: %+v", created)
	}
	id := created.Data.(map[string]any)["id"].(float64)
	path := fmt.Sprintf("/connections/%.0f", id)
	for _, tt := range []struct{ body, mode, amount, percent string }{
		{`{"name":"renamed"}`, "fixed_amount", "3.25", "40"},
		{`{"price_markup_amount":0}`, "fixed_amount", "0.00", "40"},
		{`{"price_markup_amount":"3.25"}`, "fixed_amount", "3.25", "40"},
		{`{"price_markup_mode":"percent"}`, "percent", "3.25", "40"},
		{`{"price_markup_mode":"fixed_amount","price_markup_amount":0}`, "fixed_amount", "0.00", "40"},
	} {
		result := request(http.MethodPut, path, tt.body)
		if result.StatusCode != 0 {
			t.Fatalf("update: %+v", result)
		}
		saved := request(http.MethodGet, path, "")
		data := saved.Data.(map[string]any)
		if data["price_markup_mode"] != tt.mode || data["price_markup_amount"] != tt.amount || data["price_markup_percent"] != tt.percent {
			t.Fatalf("reload after %s: %+v", tt.body, data)
		}
		listed := request(http.MethodGet, "/connections", "")
		row := listed.Data.([]any)[0].(map[string]any)
		if row["price_markup_mode"] != tt.mode || row["price_markup_amount"] != tt.amount || row["price_markup_percent"] != tt.percent {
			t.Fatalf("list after update: %+v", row)
		}
		if _, exists := data["api_secret"]; exists {
			t.Fatal("response leaked API secret")
		}
	}
	for _, body := range []string{`{"price_markup_mode":"both"}`, `{"price_markup_amount":"-1.00"}`, `{"price_markup_amount":"invalid"}`, `{"price_markup_mode":"segmented","price_markup_amount":"5"}`} {
		result := request(http.MethodPut, path, body)
		if result.StatusCode != response.CodeBadRequest {
			t.Fatalf("invalid update %s returned %+v", body, result)
		}
	}
	data := request(http.MethodGet, path, "").Data.(map[string]any)
	if data["price_markup_mode"] != "fixed_amount" || data["price_markup_amount"] != "0.00" {
		t.Fatalf("非法请求修改了配置: %+v", data)
	}
	if _, exists := data["price_markup_threshold"]; exists {
		t.Fatal("响应不应暴露已移除的分界价")
	}
}

// markupFailureStub 用于验证重算失败的业务分类和国际化，避免暴露底层错误。
type markupFailureStub struct{ err error }

func (s markupFailureStub) ReapplyMarkup(uint) (int, error) { return 0, s.err }

func TestAdminReapplyMarkupFailuresAreTranslated(t *testing.T) {
	for _, locale := range []string{"zh-CN", "zh-TW", "en-US"} {
		for _, tt := range []struct {
			name string
			err  error
			key  string
		}{
			{"category", fmt.Errorf("fetch categories: %w", mappingcontract.ErrUpstreamCategoryUnavailable), "error.reapply_categories_unavailable"},
			{"write", errors.New("private database failure"), "error.reapply_markup_failed"},
		} {
			t.Run(locale+"/"+tt.name, func(t *testing.T) {
				svc := siteconnectionapp.NewService(&siteConnectionRepoStub{}, "test-secret", t.TempDir())
				handler := siteconnectionhttp.NewAdminHandler(svc, markupFailureStub{tt.err})
				router := gin.New()
				router.POST("/connections/:id/reapply-markup", handler.ReapplyConnectionMarkup)
				req := httptest.NewRequest(http.MethodPost, "/connections/1/reapply-markup", nil)
				req.Header.Set("X-Lang", locale)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				var result response.Response
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				wantMessage := i18n.T(locale, tt.key)
				if result.StatusCode != response.CodeInternal || result.Msg != wantMessage || result.Msg == tt.key {
					t.Fatalf("重算失败提示未正确翻译: %+v, want %q", result, wantMessage)
				}
			})
		}
	}
}
