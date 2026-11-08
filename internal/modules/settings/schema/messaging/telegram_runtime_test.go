package settingsmessaging

import (
	"reflect"
	"testing"
	"time"
)

func TestTelegramRuntimeAppliedVersionRoundTrip(t *testing.T) {
	original := TelegramBotRuntimeStatusSetting{Connected: true, LastSeenAt: "2026-11-08T12:00:00Z", ConfigVersion: 5, AppliedConfigVersion: 4, LastConfigSyncAt: "2026-11-08T11:59:00Z", Warnings: []string{"config_pending"}}
	decoded := DecodeTelegramBotRuntimeStatus(EncodeTelegramBotRuntimeStatus(original), DefaultTelegramBotRuntimeStatus())
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("runtime state lost fields: %+v", decoded)
	}
	legacy := DecodeTelegramBotRuntimeStatus(map[string]interface{}{"connected": true, "config_version": 5}, DefaultTelegramBotRuntimeStatus())
	if legacy.AppliedConfigVersion != 0 || legacy.LastConfigSyncAt != "" {
		t.Fatal("legacy state invented an acknowledgement")
	}
}

func TestTelegramRuntimeLivenessUsesHeartbeatAgeWithoutMutatingStoredState(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, lastSeen  string
		connected, want bool
	}{
		{"fresh", now.Add(-time.Minute).Format(time.RFC3339), true, true},
		{"boundary", now.Add(-180 * time.Second).Format(time.RFC3339), true, true},
		{"expired", now.Add(-181 * time.Second).Format(time.RFC3339), true, false},
		{"future", now.Add(time.Second).Format(time.RFC3339), true, false},
		{"missing", "", true, false},
		{"invalid", "invalid", true, false},
		{"explicitly offline", now.Format(time.RFC3339), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := TelegramBotRuntimeStatusSetting{Connected: tc.connected, LastSeenAt: tc.lastSeen, ConfigVersion: 2}
			result := TelegramBotRuntimeStatusAt(stored, now)
			if result.Connected != tc.want {
				t.Fatalf("connected=%t, want %t", result.Connected, tc.want)
			}
			if stored.Connected != tc.connected || stored.LastSeenAt != tc.lastSeen || result.LastSeenAt != tc.lastSeen {
				t.Fatal("read advanced or rewrote heartbeat")
			}
		})
	}
}
