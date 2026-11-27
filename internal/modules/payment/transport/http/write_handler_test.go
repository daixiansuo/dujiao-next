package paymenthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/i18n"
	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

func TestRespondPaymentCreateError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{
			name: "gateway response invalid",
			err:  ErrPaymentGatewayResponseInvalid,
			code: response.CodeBadRequest,
			msg:  "支付网关响应异常",
		},
		{
			name: "recharge channel not allowed",
			err:  ErrPaymentChannelNotAllowedForRecharge,
			code: response.CodeBadRequest,
			msg:  "钱包充值不支持此支付渠道",
		},
		{
			name: "upstream stock insufficient",
			err:  mappingcontract.ErrUpstreamStockInsufficient,
			code: response.CodeBadRequest,
			msg:  i18n.T("zh-CN", "error.upstream_stock_insufficient"),
		},
		{
			name: "upstream check failed",
			err:  mappingcontract.ErrUpstreamCheckFailed,
			code: response.CodeBadRequest,
			msg:  i18n.T("zh-CN", "error.upstream_check_failed"),
		},
		{
			name: "upstream unavailable",
			err:  mappingcontract.ErrUpstreamProductNotFound,
			code: response.CodeBadRequest,
			msg:  i18n.T("zh-CN", "error.product_not_available"),
		},
		{
			name: "unknown error",
			err:  errors.New("boom"),
			code: response.CodeInternal,
			msg:  "创建支付失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := newResponseTestContext()

			respondPaymentCreateError(c, tt.err)

			assertErrorResponse(t, recorder, tt.code, tt.msg)
		})
	}
}

func TestRespondPaymentCaptureError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{
			name: "amount mismatch",
			err:  ErrPaymentAmountMismatch,
			code: response.CodeBadRequest,
			msg:  "支付金额不匹配",
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
			c, recorder := newResponseTestContext()

			respondPaymentCaptureError(c, tt.err)

			assertErrorResponse(t, recorder, tt.code, tt.msg)
		})
	}
}

func newResponseTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c, recorder
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantCode int, wantMsg string) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.StatusCode != wantCode {
		t.Fatalf("status_code = %d, want %d; body=%s", body.StatusCode, wantCode, recorder.Body.String())
	}
	if body.Msg != wantMsg {
		t.Fatalf("msg = %q, want %q; body=%s", body.Msg, wantMsg, recorder.Body.String())
	}
}

type pendingAvailabilityStub struct {
	payment *paymentdomain.Payment
	err     error
	checks  int
}

func (s *pendingAvailabilityStub) GetLatestPendingByOrder(uint, time.Time) (*paymentdomain.Payment, error) {
	return s.payment, nil
}

func (s *pendingAvailabilityStub) CheckPendingPaymentAvailability(context.Context, uint) error {
	s.checks++
	return s.err
}

func TestLatestPaymentChecksAvailabilityBeforeReturningLink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, checkErr := range []error{mappingcontract.ErrUpstreamProductNotFound, mappingcontract.ErrUpstreamStockInsufficient, mappingcontract.ErrUpstreamCheckFailed} {
		t.Run(checkErr.Error(), func(t *testing.T) {
			lookup := &pendingAvailabilityStub{payment: &paymentdomain.Payment{ID: 1, PayURL: "https://pay.example/old-link", QRCode: "old-qr"}, err: checkErr}
			handler := &LatestHandler{payments: lookup}
			ctx, recorder := newResponseTestContext()
			handler.respondLatestPayment(ctx, &orderdomain.Order{ID: 1, Status: constants.OrderStatusPendingPayment})
			var body response.Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if lookup.checks != 1 || body.StatusCode != response.CodeBadRequest || strings.Contains(recorder.Body.String(), "old-link") || strings.Contains(recorder.Body.String(), "old-qr") {
				t.Fatalf("unavailable payment link restored: checks=%d body=%s", lookup.checks, recorder.Body.String())
			}
		})
	}
}

func TestLatestPaymentSuccessAndPaidOrderSkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, paid := range []bool{false, true} {
		lookup := &pendingAvailabilityStub{payment: &paymentdomain.Payment{ID: 1, PayURL: "https://pay.example/valid"}}
		handler := &LatestHandler{payments: lookup}
		ctx, recorder := newResponseTestContext()
		status := constants.OrderStatusPendingPayment
		if paid {
			status = constants.OrderStatusPaid
		}
		handler.respondLatestPayment(ctx, &orderdomain.Order{ID: 1, Status: status})
		if paid {
			if lookup.checks != 0 || strings.Contains(recorder.Body.String(), "pay.example") {
				t.Fatalf("paid order was checked or exposed pending link: %s", recorder.Body.String())
			}
		} else if lookup.checks != 1 || !strings.Contains(recorder.Body.String(), "https://pay.example/valid") {
			t.Fatalf("available pending link missing: checks=%d body=%s", lookup.checks, recorder.Body.String())
		}
	}
}
