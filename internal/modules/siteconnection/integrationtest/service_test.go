package siteconnection_test

import (
	"encoding/json"
	"errors"
	"github.com/dujiao-next/internal/shared/money"
	"strings"
	"testing"

	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
	siteconnectioncontract "github.com/dujiao-next/internal/modules/siteconnection/contract"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/crypto"

	"github.com/shopspring/decimal"
)

func TestSiteConnectionServicePingReturnsAdapterCreationError(t *testing.T) {
	appSecretKey := "test-secret-key"
	encrypted, err := crypto.Encrypt(crypto.DeriveKey(appSecretKey), "upstream-secret")
	if err != nil {
		t.Fatalf("encrypt secret failed: %v", err)
	}
	repo := &siteConnectionRepoStub{
		conn: &siteconnectiondomain.Connection{
			ID:        1,
			Name:      "unsupported upstream",
			BaseURL:   "https://upstream.example.com",
			ApiKey:    "upstream-key",
			ApiSecret: encrypted,
			Protocol:  "unsupported-protocol",
			Status:    constants.ConnectionStatusPending,
		},
	}
	svc := siteconnectionapp.NewService(repo, appSecretKey, t.TempDir())

	result, err := svc.Ping(1)

	if err == nil {
		t.Fatalf("expected adapter creation error")
	}
	if result != nil {
		t.Fatalf("expected nil ping result, got %#v", result)
	}
	if !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("expected unsupported protocol error, got %v", err)
	}
	if repo.updated {
		t.Fatalf("connection should not be updated when adapter creation fails")
	}
}

type fakeMarkupReapplier struct {
	calls []uint
}

func (f *fakeMarkupReapplier) ReapplyMarkup(connectionID uint) (int, error) {
	f.calls = append(f.calls, connectionID)
	return 0, nil
}

func newReapplyTestConn() *siteconnectiondomain.Connection {
	return &siteconnectiondomain.Connection{
		ID:                 7,
		Name:               "conn",
		BaseURL:            "https://up.example.com",
		ApiKey:             "key",
		Protocol:           "dujiao-next",
		Status:             constants.ConnectionStatusActive,
		ExchangeRate:       decimal.NewFromInt(1),
		PriceMarkupPercent: decimal.Zero,
		PriceRoundingMode:  "none",
	}
}

func TestSiteConnectionServiceUpdateTriggersReapplyWhenExchangeRateChanges(t *testing.T) {
	repo := &siteConnectionRepoStub{conn: newReapplyTestConn()}
	svc := siteconnectionapp.NewService(repo, "test-secret-key", t.TempDir())
	reapplier := &fakeMarkupReapplier{}
	svc.SetMarkupReapplier(reapplier)

	rate := 6.9
	if _, err := svc.Update(7, siteconnectionapp.UpdateInput{ExchangeRate: &rate}); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if len(reapplier.calls) != 1 || reapplier.calls[0] != 7 {
		t.Fatalf("expected reapply called once for conn 7, got %#v", reapplier.calls)
	}
}

func TestSiteConnectionServiceUpdateSkipsReapplyWhenPriceConfigUnchanged(t *testing.T) {
	repo := &siteConnectionRepoStub{conn: newReapplyTestConn()}
	svc := siteconnectionapp.NewService(repo, "test-secret-key", t.TempDir())
	reapplier := &fakeMarkupReapplier{}
	svc.SetMarkupReapplier(reapplier)

	// 只改名字，汇率传入与现值相同的 1 → 定价配置未变，不应触发重算。
	name := "renamed"
	rate := 1.0
	if _, err := svc.Update(7, siteconnectionapp.UpdateInput{Name: name, ExchangeRate: &rate}); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if len(reapplier.calls) != 0 {
		t.Fatalf("expected no reapply when price config unchanged, got %#v", reapplier.calls)
	}
}

type siteConnectionRepoStub struct {
	conn    *siteconnectiondomain.Connection
	updated bool
}

func (r *siteConnectionRepoStub) GetByID(id uint) (*siteconnectiondomain.Connection, error) {
	if r.conn != nil && r.conn.ID == id {
		copy := *r.conn
		return &copy, nil
	}
	return nil, nil
}

func (r *siteConnectionRepoStub) GetByApiKey(apiKey string) (*siteconnectiondomain.Connection, error) {
	if r.conn != nil && r.conn.ApiKey == apiKey {
		copy := *r.conn
		return &copy, nil
	}
	return nil, nil
}

func (r *siteConnectionRepoStub) Create(conn *siteconnectiondomain.Connection) error {
	copy := *conn
	r.conn = &copy
	return nil
}

func (r *siteConnectionRepoStub) Update(conn *siteconnectiondomain.Connection) error {
	r.updated = true
	copy := *conn
	r.conn = &copy
	return nil
}

func (r *siteConnectionRepoStub) Delete(id uint) error {
	if r.conn != nil && r.conn.ID == id {
		r.conn = nil
	}
	return nil
}

func (r *siteConnectionRepoStub) List(siteconnectioncontract.ListFilter) ([]siteconnectiondomain.Connection, int64, error) {
	if r.conn == nil {
		return nil, 0, nil
	}
	return []siteconnectiondomain.Connection{*r.conn}, 1, nil
}

func (r *siteConnectionRepoStub) ListActive() ([]siteconnectiondomain.Connection, error) {
	if r.conn == nil || r.conn.Status != constants.ConnectionStatusActive {
		return nil, nil
	}
	return []siteconnectiondomain.Connection{*r.conn}, nil
}

func TestConnectionMarkupModeUpdates(t *testing.T) {
	percent := constants.ConnectionPriceMarkupPercent
	fixed := constants.ConnectionPriceMarkupFixedAmount
	invalid := "unknown"
	amount := money.FromDecimal(decimal.RequireFromString("3.25"))
	zero := money.FromDecimal(decimal.Zero)
	negative := money.FromDecimal(decimal.NewFromInt(-1))
	forty := 40.0
	tests := []struct {
		name                     string
		mode                     string
		input                    siteconnectionapp.UpdateInput
		wantMode, wantAmount     string
		wantPercent              float64
		wantReapply, wantInvalid bool
	}{
		{name: "legacy omission", input: siteconnectionapp.UpdateInput{Name: "rename"}, wantMode: percent, wantAmount: "3.25"},
		{name: "inactive amount", mode: percent, input: siteconnectionapp.UpdateInput{PriceMarkupAmount: &zero}, wantMode: percent, wantAmount: "0.00"},
		{name: "inactive percent", mode: fixed, input: siteconnectionapp.UpdateInput{PriceMarkupPercent: &forty}, wantMode: fixed, wantAmount: "3.25", wantPercent: 40},
		{name: "switch to fixed preserves amount", mode: percent, input: siteconnectionapp.UpdateInput{PriceMarkupMode: &fixed}, wantMode: fixed, wantAmount: "3.25", wantReapply: true},
		{name: "switch to percent", mode: fixed, input: siteconnectionapp.UpdateInput{PriceMarkupMode: &percent}, wantMode: percent, wantAmount: "3.25", wantReapply: true},
		{name: "active amount zero", mode: fixed, input: siteconnectionapp.UpdateInput{PriceMarkupAmount: &zero}, wantMode: fixed, wantAmount: "0.00", wantReapply: true},
		{name: "unchanged fixed", mode: fixed, input: siteconnectionapp.UpdateInput{PriceMarkupMode: &fixed, PriceMarkupAmount: &amount}, wantMode: fixed, wantAmount: "3.25"},
		{name: "active percent", mode: percent, input: siteconnectionapp.UpdateInput{PriceMarkupPercent: &forty}, wantMode: percent, wantAmount: "3.25", wantPercent: 40, wantReapply: true},
		{name: "invalid mode", mode: percent, input: siteconnectionapp.UpdateInput{PriceMarkupMode: &invalid}, wantInvalid: true},
		{name: "negative amount", mode: fixed, input: siteconnectionapp.UpdateInput{PriceMarkupAmount: &negative}, wantInvalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newReapplyTestConn()
			conn.PriceMarkupMode = tt.mode
			conn.PriceMarkupAmount = amount
			repo := &siteConnectionRepoStub{conn: conn}
			svc := siteconnectionapp.NewService(repo, "test-secret", t.TempDir())
			reapplier := &fakeMarkupReapplier{}
			svc.SetMarkupReapplier(reapplier)
			got, err := svc.Update(conn.ID, tt.input)
			if tt.wantInvalid {
				if !errors.Is(err, siteconnectioncontract.ErrInvalid) || repo.updated || len(reapplier.calls) != 0 {
					t.Fatalf("invalid input changed state: %v, updated=%v, calls=%v", err, repo.updated, reapplier.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.PriceMarkupMode != tt.wantMode || got.PriceMarkupAmount.String() != tt.wantAmount || !got.PriceMarkupPercent.Equal(decimal.NewFromFloat(tt.wantPercent)) {
				t.Fatalf("unexpected pricing: %+v", got)
			}
			if (len(reapplier.calls) == 1) != tt.wantReapply {
				t.Fatalf("reapply calls=%v, want reapply=%v (AutoSyncPrice=false)", reapplier.calls, tt.wantReapply)
			}
		})
	}
}

func TestCreateConnectionMarkupDefaultsAndValidation(t *testing.T) {
	for _, tt := range []struct {
		name, fields, wantMode, wantAmount string
		invalid                            bool
	}{
		{"old client", `{}`, constants.ConnectionPriceMarkupPercent, "0.00", false},
		{"string amount", `{"price_markup_mode":"fixed_amount","price_markup_amount":"3.25"}`, constants.ConnectionPriceMarkupFixedAmount, "3.25", false},
		{"numeric amount", `{"price_markup_mode":"fixed_amount","price_markup_amount":0.25}`, constants.ConnectionPriceMarkupFixedAmount, "0.25", false},
		{"segmented string threshold", `{"price_markup_mode":"segmented","price_markup_threshold":"15.00","price_markup_amount":"5.00","price_markup_percent":30}`, constants.ConnectionPriceMarkupSegmented, "5.00", false},
		{"segmented numeric threshold", `{"price_markup_mode":"segmented","price_markup_threshold":15,"price_markup_amount":0,"price_markup_percent":0}`, constants.ConnectionPriceMarkupSegmented, "0.00", false},
		{"segmented missing threshold", `{"price_markup_mode":"segmented"}`, "", "", true},
		{"segmented negative threshold", `{"price_markup_mode":"segmented","price_markup_threshold":"-1"}`, "", "", true},
		{"segmented negative percent", `{"price_markup_mode":"segmented","price_markup_threshold":"15","price_markup_percent":-1}`, "", "", true},
		{"segmented negative amount", `{"price_markup_mode":"segmented","price_markup_threshold":"15","price_markup_amount":"-1"}`, "", "", true},
		{"invalid mode", `{"price_markup_mode":"both"}`, "", "", true},
		{"negative amount", `{"price_markup_mode":"fixed_amount","price_markup_amount":"-0.01"}`, "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := siteconnectionapp.CreateInput{Name: "upstream", BaseURL: "https://example.com", ApiKey: "key", ApiSecret: "secret"}
			if err := json.Unmarshal([]byte(tt.fields), &input); err != nil {
				t.Fatal(err)
			}
			repo := &siteConnectionRepoStub{}
			svc := siteconnectionapp.NewService(repo, "test-secret", t.TempDir())
			got, err := svc.Create(input)
			if tt.invalid {
				if !errors.Is(err, siteconnectioncontract.ErrInvalid) || repo.conn != nil {
					t.Fatalf("invalid create: %v, %+v", err, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.PriceMarkupMode != tt.wantMode || got.PriceMarkupAmount.String() != tt.wantAmount {
				t.Fatalf("unexpected pricing: %+v", got)
			}
		})
	}
}

func TestSegmentedMarkupUpdates(t *testing.T) {
	for _, tt := range []struct {
		name, mode, fields, wantThreshold, wantAmount, wantPercent string
		missingThreshold, invalid, reapply                         bool
	}{
		{name: "unrelated update", fields: `{"name":"renamed"}`, wantThreshold: "15.00", wantAmount: "5.00", wantPercent: "30"},
		{name: "threshold change", fields: `{"price_markup_threshold":"16"}`, wantThreshold: "16.00", wantAmount: "5.00", wantPercent: "30", reapply: true},
		{name: "amount change", fields: `{"price_markup_amount":"6"}`, wantThreshold: "15.00", wantAmount: "6.00", wantPercent: "30", reapply: true},
		{name: "percent change", fields: `{"price_markup_percent":40}`, wantThreshold: "15.00", wantAmount: "5.00", wantPercent: "40", reapply: true},
		{name: "zero amount", fields: `{"price_markup_amount":0}`, wantThreshold: "15.00", wantAmount: "0.00", wantPercent: "30", reapply: true},
		{name: "zero percent", fields: `{"price_markup_percent":0}`, wantThreshold: "15.00", wantAmount: "5.00", wantPercent: "0", reapply: true},
		{name: "unchanged", fields: `{"price_markup_mode":"segmented","price_markup_threshold":"15","price_markup_amount":"5","price_markup_percent":30}`, wantThreshold: "15.00", wantAmount: "5.00", wantPercent: "30"},
		{name: "switch with preserved values", mode: "fixed_amount", fields: `{"price_markup_mode":"segmented"}`, wantThreshold: "15.00", wantAmount: "5.00", wantPercent: "30", reapply: true},
		{name: "switch merges new threshold", mode: "percent", missingThreshold: true, fields: `{"price_markup_mode":"segmented","price_markup_threshold":"15"}`, wantThreshold: "15.00", wantAmount: "5.00", wantPercent: "30", reapply: true},
		{name: "inactive threshold percent", mode: "percent", fields: `{"price_markup_threshold":"16"}`, wantThreshold: "16.00", wantAmount: "5.00", wantPercent: "30"},
		{name: "inactive threshold fixed", mode: "fixed_amount", fields: `{"price_markup_threshold":"16"}`, wantThreshold: "16.00", wantAmount: "5.00", wantPercent: "30"},
		{name: "leave segmented and clear threshold", fields: `{"price_markup_mode":"percent","price_markup_threshold":0}`, wantThreshold: "0.00", wantAmount: "5.00", wantPercent: "30", reapply: true},
		{name: "switch missing threshold", mode: "fixed_amount", missingThreshold: true, fields: `{"price_markup_mode":"segmented"}`, invalid: true},
		{name: "zero threshold", fields: `{"price_markup_threshold":0}`, invalid: true},
		{name: "negative threshold", fields: `{"price_markup_threshold":"-1"}`, invalid: true},
		{name: "negative amount", fields: `{"price_markup_amount":"-1"}`, invalid: true},
		{name: "negative percent", fields: `{"price_markup_percent":-1}`, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn := newReapplyTestConn()
			conn.PriceMarkupMode = constants.ConnectionPriceMarkupSegmented
			if tt.mode != "" {
				conn.PriceMarkupMode = tt.mode
			}
			conn.PriceMarkupThreshold = money.FromDecimal(decimal.NewFromInt(15))
			if tt.missingThreshold {
				conn.PriceMarkupThreshold = money.FromDecimal(decimal.Zero)
			}
			conn.PriceMarkupAmount = money.FromDecimal(decimal.NewFromInt(5))
			conn.PriceMarkupPercent = decimal.NewFromInt(30)
			before := *conn
			repo := &siteConnectionRepoStub{conn: conn}
			svc := siteconnectionapp.NewService(repo, "test-secret", t.TempDir())
			reapplier := &fakeMarkupReapplier{}
			svc.SetMarkupReapplier(reapplier)
			var input siteconnectionapp.UpdateInput
			if err := json.Unmarshal([]byte(tt.fields), &input); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Update(conn.ID, input)
			if tt.invalid {
				if !errors.Is(err, siteconnectioncontract.ErrInvalid) || repo.updated || len(reapplier.calls) != 0 {
					t.Fatalf("invalid update saved or repriced: err=%v, updated=%v, calls=%v", err, repo.updated, reapplier.calls)
				}
				if !repo.conn.PriceMarkupThreshold.Equal(before.PriceMarkupThreshold.Decimal) || !repo.conn.PriceMarkupAmount.Equal(before.PriceMarkupAmount.Decimal) || !repo.conn.PriceMarkupPercent.Equal(before.PriceMarkupPercent) || repo.conn.PriceMarkupMode != before.PriceMarkupMode {
					t.Fatal("invalid input changed persisted pricing")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.PriceMarkupThreshold.String() != tt.wantThreshold || got.PriceMarkupAmount.String() != tt.wantAmount || got.PriceMarkupPercent.String() != tt.wantPercent {
				t.Fatalf("unexpected pricing: %+v", got)
			}
			if (len(reapplier.calls) == 1) != tt.reapply {
				t.Fatalf("reapply calls=%v, want=%v", reapplier.calls, tt.reapply)
			}
		})
	}
}
