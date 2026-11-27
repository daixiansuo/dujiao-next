package paymenthttp

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	"github.com/dujiao-next/internal/constants"
	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

func TestStripeWebhookQueryBind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe?channel_id=12", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	var query StripeWebhookQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		t.Fatalf("bind stripe query failed: %v", err)
	}
	if query.ChannelID != 12 {
		t.Fatalf("expected channel id 12, got %d", query.ChannelID)
	}
}

func TestRespondPaymentCallbackError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{
			name: "status invalid",
			err:  ErrPaymentStatusInvalid,
			code: response.CodeBadRequest,
			msg:  "支付状态不合法",
		},
		{
			name: "gateway response invalid",
			err:  ErrPaymentGatewayResponseInvalid,
			code: response.CodeBadRequest,
			msg:  "支付网关响应异常",
		},
		{
			name: "unknown error",
			err:  errors.New("boom"),
			code: response.CodeInternal,
			msg:  "支付回调处理失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

			respondPaymentCallbackError(c, tt.err)

			if recorder.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusOK)
			}
			var body response.Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.StatusCode != tt.code {
				t.Fatalf("status_code = %d, want %d; body=%s", body.StatusCode, tt.code, recorder.Body.String())
			}
			if body.Msg != tt.msg {
				t.Fatalf("msg = %q, want %q; body=%s", body.Msg, tt.msg, recorder.Body.String())
			}
		})
	}
}

func TestReadWebhookBodyRejectsOversizedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", maxWebhookBodyBytes+1)))

	if _, err := readWebhookBody(c); err == nil {
		t.Fatalf("oversized webhook body must be rejected")
	}
}

func TestEpayRedirectHandlerRendersPOSTForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		token     = "0123456789abcdef0123456789abcdef"
		endpoint  = "https://gateway.example.com/api/pay/submit"
		sign      = "ab+cd/ef="
		returnURL = "https://shop.example.com/pay?foo=1&bar=2"
		paymentID = uint(7)
	)
	handler := NewEpayRedirectHandler(fakeEpayRedirectPayments{
		byID: map[uint]*paymentdomain.Payment{
			paymentID: epayRedirectPayment(paymentID, token, endpoint, sign, returnURL),
		},
	})
	router := gin.New()
	api := router.Group("/api/v1")
	RegisterEpayRedirectRoute(api, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/epay-redirect?payment_id=7&token="+token, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	method, action, fields := parseEpayRedirectForm(t, recorder.Body.String())
	if method != "post" {
		t.Fatalf("form method = %s, want post", method)
	}
	if action != endpoint {
		t.Fatalf("form action = %s, want %s", action, endpoint)
	}
	if fields["sign"] != sign {
		t.Fatalf("sign = %q, want raw %q", fields["sign"], sign)
	}
	if fields["return_url"] != returnURL {
		t.Fatalf("return_url = %q, want raw %q", fields["return_url"], returnURL)
	}
	if strings.Contains(fields["sign"], "%") || strings.Contains(fields["return_url"], "%") {
		t.Fatalf("form values must not be percent-encoded: %#v", fields)
	}
	if !epayRedirectHasVisibleSubmit(recorder.Body.String()) {
		t.Fatalf("visible submit control missing: %s", recorder.Body.String())
	}
}

func TestEpayRedirectHandlerRejects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		token     = "0123456789abcdef0123456789abcdef"
		endpoint  = "https://gateway.example.com/api/pay/submit"
		sign      = "ab+cd/ef="
		returnURL = "https://shop.example.com/pay?foo=1&bar=2"
	)
	base := epayRedirectPayment(7, token, endpoint, sign, returnURL)
	expired := epayRedirectPayment(8, token, endpoint, sign, returnURL)
	past := time.Now().Add(-time.Minute)
	expired.ExpiredAt = &past
	superseded := epayRedirectPayment(9, token, endpoint, sign, returnURL)
	now := time.Now()
	superseded.SupersededAt = &now
	success := epayRedirectPayment(10, token, endpoint, sign, returnURL)
	success.Status = constants.PaymentStatusSuccess
	v1 := epayRedirectPayment(11, token, "https://gateway.example.com/submit.php", sign, returnURL)
	delete(v1.ProviderPayload, "submit_method")
	badAction := epayRedirectPayment(12, token, "javascript:alert(1)", sign, returnURL)
	otherProvider := epayRedirectPayment(13, token, endpoint, sign, returnURL)
	otherProvider.ProviderType = constants.PaymentProviderOfficial

	lookup := fakeEpayRedirectPayments{byID: map[uint]*paymentdomain.Payment{
		7:  base,
		8:  expired,
		9:  superseded,
		10: success,
		11: v1,
		12: badAction,
		13: otherProvider,
	}}
	router := gin.New()
	RegisterEpayRedirectRoute(router.Group("/api/v1"), NewEpayRedirectHandler(lookup))

	cases := []struct {
		name string
		url  string
	}{
		{name: "wrong token", url: "/api/v1/payments/epay-redirect?payment_id=7&token=wrong-token"},
		{name: "unknown payment", url: "/api/v1/payments/epay-redirect?payment_id=99&token=" + token},
		{name: "expired", url: "/api/v1/payments/epay-redirect?payment_id=8&token=" + token},
		{name: "superseded", url: "/api/v1/payments/epay-redirect?payment_id=9&token=" + token},
		{name: "not pending", url: "/api/v1/payments/epay-redirect?payment_id=10&token=" + token},
		{name: "v1 redirect", url: "/api/v1/payments/epay-redirect?payment_id=11&token=" + token},
		{name: "non-http action", url: "/api/v1/payments/epay-redirect?payment_id=12&token=" + token},
		{name: "wrong provider", url: "/api/v1/payments/epay-redirect?payment_id=13&token=" + token},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404, body=%s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(strings.ToLower(recorder.Body.String()), "<form") {
				t.Fatalf("rejected response rendered a form: %s", recorder.Body.String())
			}
		})
	}
}

type fakeEpayRedirectPayments struct {
	byID  map[uint]*paymentdomain.Payment
	check func(context.Context, uint) error
}

func (f fakeEpayRedirectPayments) CheckPendingPaymentAvailability(ctx context.Context, orderID uint) error {
	if f.check != nil {
		return f.check(ctx, orderID)
	}
	return nil
}

func (f fakeEpayRedirectPayments) GetByID(id uint) (*paymentdomain.Payment, error) {
	if f.byID == nil {
		return nil, nil
	}
	return f.byID[id], nil
}

func epayRedirectPayment(id uint, token, endpoint, sign, returnURL string) *paymentdomain.Payment {
	return &paymentdomain.Payment{
		ID:              id,
		ProviderType:    constants.PaymentProviderEpay,
		InteractionMode: constants.PaymentInteractionRedirect,
		Status:          constants.PaymentStatusPending,
		ProviderPayload: jsonmap.JSON{
			"submit_method": http.MethodPost,
			"endpoint":      endpoint,
			"token":         token,
			"params": map[string]interface{}{
				"pid":        "1002",
				"sign":       sign,
				"return_url": returnURL,
				"sign_type":  "RSA",
			},
		},
	}
}

func parseEpayRedirectForm(t *testing.T, body string) (string, string, map[string]string) {
	t.Helper()
	formTag := regexp.MustCompile(`(?is)<form\b([^>]*)>`).FindStringSubmatch(body)
	if formTag == nil {
		t.Fatalf("form missing: %s", body)
	}
	formAttrs := htmlAttributeMap(formTag[1])
	fields := map[string]string{}
	for _, match := range regexp.MustCompile(`(?is)<input\b([^>]*)>`).FindAllStringSubmatch(body, -1) {
		attrs := htmlAttributeMap(match[1])
		if !strings.EqualFold(attrs["type"], "hidden") {
			continue
		}
		fields[html.UnescapeString(attrs["name"])] = html.UnescapeString(attrs["value"])
	}
	return strings.ToLower(formAttrs["method"]), html.UnescapeString(formAttrs["action"]), fields
}

func epayRedirectHasVisibleSubmit(body string) bool {
	for _, match := range regexp.MustCompile(`(?is)<(button|input)\b([^>]*)>`).FindAllStringSubmatch(body, -1) {
		attrs := htmlAttributeMap(match[2])
		if !strings.EqualFold(attrs["type"], "submit") {
			continue
		}
		if strings.Contains(strings.ToLower(match[0]), "hidden") && strings.EqualFold(match[1], "input") && strings.EqualFold(attrs["type"], "hidden") {
			continue
		}
		return true
	}
	return false
}

func htmlAttributeMap(tag string) map[string]string {
	out := map[string]string{}
	for _, match := range regexp.MustCompile(`([^\s=]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`).FindAllStringSubmatch(tag, -1) {
		value := match[2]
		if match[3] != "" {
			value = match[3]
		}
		out[strings.ToLower(match[1])] = value
	}
	return out
}

func TestEpayRedirectChecksOnlyAuthorizedPendingProductPayment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name    string
		orderID uint
		status  string
		token   string
		err     error
		checks  int
		form    bool
	}{
		{name: "available product", orderID: 18, status: constants.PaymentStatusPending, token: token, checks: 1, form: true},
		{name: "unavailable product", orderID: 18, status: constants.PaymentStatusPending, token: token, err: mappingcontract.ErrUpstreamProductNotFound, checks: 1},
		{name: "insufficient stock", orderID: 18, status: constants.PaymentStatusPending, token: token, err: mappingcontract.ErrUpstreamStockInsufficient, checks: 1},
		{name: "unknown availability", orderID: 18, status: constants.PaymentStatusPending, token: token, err: mappingcontract.ErrUpstreamCheckFailed, checks: 1},
		{name: "wrong token", orderID: 18, status: constants.PaymentStatusPending, token: "wrong", checks: 0},
		{name: "paid payment", orderID: 18, status: constants.PaymentStatusSuccess, token: token, checks: 0},
		{name: "wallet recharge", orderID: 0, status: constants.PaymentStatusPending, token: token, checks: 0, form: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payment := epayRedirectPayment(7, token, "https://gateway.example.com/api/pay/submit", "private-sign", "https://shop.example/return")
			payment.OrderID, payment.Status = tc.orderID, tc.status
			checks := 0
			lookup := fakeEpayRedirectPayments{byID: map[uint]*paymentdomain.Payment{7: payment}, check: func(_ context.Context, orderID uint) error {
				checks++
				if orderID != tc.orderID {
					t.Fatalf("wrong order: %d", orderID)
				}
				return tc.err
			}}
			router := gin.New()
			RegisterEpayRedirectRoute(router.Group("/api/v1"), NewEpayRedirectHandler(lookup))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/payments/epay-redirect?payment_id=7&token="+tc.token, nil))
			form := strings.Contains(strings.ToLower(recorder.Body.String()), "<form")
			if checks != tc.checks || form != tc.form || (!tc.form && strings.Contains(recorder.Body.String(), "private-sign")) {
				t.Fatalf("unsafe redirect: checks=%d form=%t body=%s", checks, form, recorder.Body.String())
			}
		})
	}
}
