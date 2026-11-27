package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
)

// 同一个 404 可能表示删除或下架，只有明确删除码才进入 deleted 状态。
func TestGetProductClassifiesUnavailability(t *testing.T) {
	disableRequestPacing(t)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"deleted", 404, `{"error_code":"product_deleted"}`, ErrUpstreamProductDeleted},
		{"not_found", 404, `{"error_code":"product_not_found","error_message":"商品不存在或已下架"}`, ErrUpstreamProductUnavailable},
		{"unavailable", 400, `{"error_code":"product_unavailable"}`, ErrUpstreamProductUnavailable},
		{"html_not_found", 404, `<html><body>Not Found</body></html>`, ErrUpstreamProductUnavailable},
		{"json_unknown_not_found", 404, `{"error_code":"permission_denied"}`, nil},
		{"unauthorized", 401, `{"error_code":"product_not_found"}`, nil},
		{"forbidden", 403, `{"error_code":"product_not_found"}`, nil},
		{"rate_limited", 429, `{"error_code":"product_not_found"}`, nil},
		{"server_failure", 503, `{"error_code":"product_deleted"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: server.URL}, t.TempDir())
			_, err := adapter.GetProduct(context.Background(), 7)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (errors.Is(err, ErrUpstreamProductDeleted) || errors.Is(err, ErrUpstreamProductUnavailable)) {
				t.Fatalf("connection or unknown business failure must not stop a product: %v", err)
			}
			if tc.name == "html_not_found" {
				if _, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 1, PageSize: 50}); errors.Is(err, ErrUpstreamProductUnavailable) {
					t.Fatal("list 404 must not be classified as product unavailability")
				}
			}
		})
	}
}

// 缺字段与显式零值必须区分，否则同步可能误下架商品或误删整批规格。
func TestGetProductValidatesAvailabilityFields(t *testing.T) {
	disableRequestPacing(t)
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"complete", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":9,"is_active":true,"stock_quantity":0}]}}`, true},
		{"unlimited", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":9,"is_active":true,"stock_quantity":-1}]}}`, true},
		{"empty_skus", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[]}}`, true},
		{"inactive_without_skus", `{"ok":true,"product":{"id":7,"is_active":false}}`, true},
		{"inactive_with_partial_skus", `{"ok":true,"product":{"id":7,"is_active":false,"skus":[{"id":9,"is_active":false}]}}`, true},
		{"false_ok", `{"ok":false,"product":{"id":7,"is_active":true,"skus":[]}}`, false},
		{"missing_ok", `{"product":{"id":7,"is_active":true,"skus":[]}}`, false},
		{"missing_product", `{"ok":true}`, false},
		{"wrong_id", `{"ok":true,"product":{"id":8,"is_active":true,"skus":[]}}`, false},
		{"missing_status", `{"ok":true,"product":{"id":7,"skus":[]}}`, false},
		{"missing_skus", `{"ok":true,"product":{"id":7,"is_active":true}}`, false},
		{"null_skus", `{"ok":true,"product":{"id":7,"is_active":true,"skus":null}}`, false},
		{"missing_sku_status", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":9,"stock_quantity":1}]}}`, false},
		{"missing_stock", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":9,"is_active":true}]}}`, false},
		{"invalid_stock", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":9,"is_active":true,"stock_quantity":-2}]}}`, false},
		{"zero_sku_id", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":0,"is_active":true,"stock_quantity":1}]}}`, false},
		{"duplicate_skus", `{"ok":true,"product":{"id":7,"is_active":true,"skus":[{"id":9,"is_active":true,"stock_quantity":1},{"id":9,"is_active":false,"stock_quantity":0}]}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: server.URL}, t.TempDir())
			product, err := adapter.GetProduct(context.Background(), 7)
			if tc.ok {
				if err != nil || product == nil {
					t.Fatalf("valid product rejected: product=%+v error=%v", product, err)
				}
			} else if !errors.Is(err, ErrUpstreamInvalidResponse) {
				t.Fatalf("error = %v, want invalid response", err)
			}
		})
	}
}

func TestListProductsValidatesEnvelopeAndPage(t *testing.T) {
	disableRequestPacing(t)
	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{"legacy_without_echo", `{"ok":true,"total":0,"items":[]}`, true},
		{"valid", `{"ok":true,"total":1,"items":[{"id":7,"is_active":true,"skus":[]}],"page":1,"page_size":50}`, true},
		{"false_ok", `{"ok":false,"total":0,"items":[]}`, false},
		{"missing_total", `{"ok":true,"items":[]}`, false},
		{"negative_total", `{"ok":true,"total":-1,"items":[]}`, false},
		{"missing_items", `{"ok":true,"total":0}`, false},
		{"null_items", `{"ok":true,"total":0,"items":null}`, false},
		{"wrong_page", `{"ok":true,"total":0,"items":[],"page":2}`, false},
		{"negative_page", `{"ok":true,"total":0,"items":[],"page":-1}`, false},
		{"small_page_size", `{"ok":true,"total":2,"items":[{"id":7,"is_active":true,"skus":[]},{"id":8,"is_active":false}],"page_size":1}`, false},
		{"small_total", `{"ok":true,"total":0,"items":[{"id":7,"is_active":true,"skus":[]}]}`, false},
		{"incomplete_product", `{"ok":true,"total":1,"items":[{"id":7,"is_active":true}]}`, false},
		{"duplicate_products", `{"ok":true,"total":2,"items":[{"id":7,"is_active":true,"skus":[]},{"id":7,"is_active":false}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: server.URL}, t.TempDir())
			_, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 1, PageSize: 50})
			if tc.ok && err != nil {
				t.Fatalf("valid list rejected: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrUpstreamInvalidResponse) {
				t.Fatalf("error = %v, want invalid response", err)
			}
		})
	}
}

type availabilityRoundTripper func(*http.Request) (*http.Response, error)

func (f availabilityRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type truncatedAvailabilityBody struct{ closed *bool }

func (b truncatedAvailabilityBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b truncatedAvailabilityBody) Close() error {
	*b.closed = true
	return nil
}

// 读取重试必须同时覆盖发送失败和响应体截断，并在重试前关闭旧响应。
func TestReadRequestRetriesTransientTransportFailures(t *testing.T) {
	disableRequestPacing(t)
	for _, readFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "send", true: "read"}[readFailure], func(t *testing.T) {
			calls := 0
			closed := false
			adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: "http://upstream.test"}, t.TempDir())
			adapter.client.Transport = availabilityRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if readFailure {
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: truncatedAvailabilityBody{closed: &closed}}, nil
					}
					return nil, io.ErrUnexpectedEOF
				}
				if readFailure && !closed {
					t.Error("previous response body was not closed")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"categories":[]}`))}, nil
			})
			if _, err := adapter.ListCategories(context.Background()); err != nil || calls != 2 {
				t.Fatalf("calls=%d error=%v, want successful second attempt", calls, err)
			}
		})
	}
}

func TestReadRetryHonorsLimitsAndDoesNotRetryPurchase(t *testing.T) {
	disableRequestPacing(t)
	for _, tc := range []struct {
		name    string
		method  string
		err     error
		timeout time.Duration
		calls   int
	}{
		{"get_bounded", http.MethodGet, io.ErrUnexpectedEOF, 0, maxRequestAttempts},
		{"post_no_retry", http.MethodPost, io.ErrUnexpectedEOF, 0, 1},
		{"permanent_error", http.MethodGet, errors.New("invalid TLS certificate"), 0, 1},
		{"deadline_short", http.MethodGet, io.ErrUnexpectedEOF, 20 * time.Millisecond, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: "http://upstream.test"}, t.TempDir())
			adapter.client.Transport = availabilityRoundTripper(func(r *http.Request) (*http.Response, error) { calls++; return nil, tc.err })
			ctx := context.Background()
			if tc.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			if err := adapter.doRequest(ctx, tc.method, "/api/v1/upstream/products", nil, nil); err == nil || calls != tc.calls {
				t.Fatalf("calls=%d error=%v, want calls=%d and error", calls, err, tc.calls)
			}
		})
	}
}
