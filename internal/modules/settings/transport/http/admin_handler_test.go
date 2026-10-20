package settingshttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/i18n"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

type adminSettingsStub struct {
	updateCalls int
	updateErr   error
}

func (s *adminSettingsStub) GetByKey(string) (jsonmap.JSON, error) {
	return nil, nil
}

func (s *adminSettingsStub) UpdateWithEffects(string, map[string]interface{}) (settingsapp.UpdateResult, error) {
	s.updateCalls++
	return settingsapp.UpdateResult{}, s.updateErr
}

func (s *adminSettingsStub) InvalidateCallbackRoutesCache() {}

func TestAdminHandlerRejectsGoogleAuthOnGenericUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminSettingsStub{}
	handler := NewAdminHandler(stub)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{"key":"`+constants.SettingKeyGoogleAuthConfig+`","value":{"enabled":true,"client_id":""}}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	handler.Update(context)

	if stub.updateCalls != 0 {
		t.Fatalf("generic update persisted protected Google auth config")
	}
	var body struct {
		StatusCode int    `json:"status_code"`
		Msg        string `json:"msg"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	if recorder.Code != http.StatusOK || body.StatusCode != 400 {
		t.Fatalf("http=%d status_code=%d body=%s", recorder.Code, body.StatusCode, recorder.Body.String())
	}
	if !strings.Contains(body.Msg, "/admin/settings/google-auth") {
		t.Fatalf("unexpected rejection message: %q", body.Msg)
	}
}

// 有专用接口的配置一律不能走通用接口，否则会跳过校验与运行时生效。
func TestAdminHandlerRejectsDedicatedKeysOnGenericUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for key, endpoint := range dedicatedSettingEndpoints {
		stub := &adminSettingsStub{}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", strings.NewReader(`{"key":"`+key+`","value":{}}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		NewAdminHandler(stub).Update(ctx)
		if stub.updateCalls != 0 || !strings.Contains(recorder.Body.String(), endpoint) {
			t.Fatalf("%s: generic update must be rejected with hint %s, body=%s", key, endpoint, recorder.Body.String())
		}
	}
}

// Backup secrets must never be exposed through the generic raw JSON endpoint.
func TestAdminHandlerRejectsBackupConfigOnGenericRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminSettingsStub{}
	handler := NewAdminHandler(stub)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings?key="+constants.SettingKeyDatabaseBackupConfig, nil)
	handler.Get(context)
	var body struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.StatusCode != 400 {
		t.Fatalf("protected backup settings read returned %s", recorder.Body.String())
	}
}

func TestAdminHandlerContactValidationMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, locale := range []string{"zh-CN", "zh-TW", "en-US"} {
		for field, key := range map[string]string{
			"contact_email": "error.reseller_support_email_invalid",
			"contact_x":     "error.site_contact_x_invalid",
		} {
			t.Run(locale+"/"+field, func(t *testing.T) {
				stub := &adminSettingsStub{updateErr: &settingsapp.SettingFieldError{Field: field}}
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", strings.NewReader(`{"key":"site_config","value":{}}`))
				ctx.Request.Header.Set("Content-Type", "application/json")
				ctx.Request.Header.Set("Accept-Language", locale)
				NewAdminHandler(stub).Update(ctx)
				var body struct {
					StatusCode int    `json:"status_code"`
					Msg        string `json:"msg"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				want := i18n.T(locale, key)
				if stub.updateCalls != 1 || body.StatusCode != 400 || body.Msg != want || want == key {
					t.Fatalf("unexpected validation response: %s (expected %s)", recorder.Body.String(), want)
				}
			})
		}
	}
}
