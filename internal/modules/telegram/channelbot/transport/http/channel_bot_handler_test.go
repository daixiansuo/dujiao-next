package telegramhttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/gin-gonic/gin"
)

type heartbeatSettingsStub struct {
	status settingsmessaging.TelegramBotRuntimeStatusSetting
	writes int
}

func (s *heartbeatSettingsStub) GetTelegramBotConfig() (settingsmessaging.TelegramBotConfigSetting, error) {
	return settingsmessaging.DefaultTelegramBotConfig(), nil
}
func (s *heartbeatSettingsStub) GetTelegramBotRuntimeStatus() (settingsmessaging.TelegramBotRuntimeStatusSetting, error) {
	return s.status, nil
}
func (s *heartbeatSettingsStub) UpdateTelegramBotRuntimeStatus(status settingsmessaging.TelegramBotRuntimeStatusSetting) error {
	s.status = status
	s.writes++
	return nil
}

type heartbeatTokenStub struct{}

func (heartbeatTokenStub) DecryptBotTokenByClientID(uint) (string, error) { return "", nil }

func TestHeartbeatAcknowledgesOnlyReportedAppliedVersion(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		code, applied int
		acknowledged  bool
	}{
		{"legacy", `{"bot_version":"old"}`, http.StatusOK, 1, false},
		{"current", `{"applied_config_version":3}`, http.StatusOK, 3, true},
		{"outdated", `{"applied_config_version":2}`, http.StatusOK, 2, true},
		{"zero", `{"applied_config_version":0}`, http.StatusOK, 0, true},
		{"future", `{"applied_config_version":4}`, http.StatusBadRequest, 1, false},
		{"negative", `{"applied_config_version":-1}`, http.StatusBadRequest, 1, false},
		{"invalid type", `{"applied_config_version":"3"}`, http.StatusBadRequest, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			oldSync := "2026-01-01T00:00:00Z"
			settings := &heartbeatSettingsStub{status: settingsmessaging.TelegramBotRuntimeStatusSetting{ConfigVersion: 3, AppliedConfigVersion: 1, LastConfigSyncAt: oldSync}}
			handler := NewChannelBotHandler(settings, heartbeatTokenStub{})
			router := gin.New()
			router.POST("/heartbeat", handler.ReportHeartbeat)
			request := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			started := time.Now().UTC().Truncate(time.Second)
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.code {
				t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
			}
			if settings.status.AppliedConfigVersion != tc.applied || settings.status.ConfigVersion != 3 {
				t.Fatalf("unexpected versions: %+v", settings.status)
			}
			if tc.code != http.StatusOK {
				if settings.writes != 0 {
					t.Fatal("rejected heartbeat persisted state")
				}
				return
			}
			if settings.writes != 1 || !settings.status.Connected {
				t.Fatal("valid heartbeat was not recorded")
			}
			if tc.acknowledged {
				synced, err := time.Parse(time.RFC3339, settings.status.LastConfigSyncAt)
				if err != nil || synced.Before(started) || synced.After(time.Now().UTC()) {
					t.Fatalf("sync time must be server generated: %q", settings.status.LastConfigSyncAt)
				}
			} else if settings.status.LastConfigSyncAt != oldSync {
				t.Fatal("legacy heartbeat falsely acknowledged configuration")
			}
			var response struct {
				Data struct {
					ConfigVersion int `json:"config_version"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Data.ConfigVersion != 3 {
				t.Fatal("heartbeat response lost desired version", err)
			}
		})
	}
}
