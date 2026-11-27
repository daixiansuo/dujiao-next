package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/jsonmap"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
)

// CapturePaymentInput 捕获支付输入。
type CapturePaymentInput struct {
	PaymentID uint
	Context   context.Context
}

func (s *PaymentService) CapturePayment(input CapturePaymentInput) (*paymentdomain.Payment, error) {
	if input.PaymentID == 0 {
		return nil, ErrPaymentInvalid
	}
	payment, err := s.paymentRepo.GetByID(input.PaymentID)
	if err != nil {
		return nil, ErrPaymentUpdateFailed
	}
	if payment == nil {
		return nil, ErrPaymentNotFound
	}
	if payment.Status == constants.PaymentStatusSuccess {
		return payment, nil
	}

	channel, err := s.channelRepo.GetByID(payment.ChannelID)
	if err != nil {
		return nil, ErrPaymentUpdateFailed
	}
	if channel == nil {
		return nil, ErrPaymentChannelNotFound
	}

	providerType := strings.ToLower(strings.TrimSpace(channel.ProviderType))
	if providerType != constants.PaymentProviderOfficial {
		return nil, ErrPaymentProviderNotSupported
	}
	if strings.TrimSpace(payment.ProviderRef) == "" {
		return nil, ErrPaymentInvalid
	}

	// 统一通过 Registry 路由。Registry.Lookup 会返回 channel 对应的 adapter,
	// 如果 adapter 不实现 Capturer,type assertion 失败,返回 ErrPaymentProviderNotSupported。
	// 因此无需在此显式检查 channel 是否支持 capture。
	return s.captureViaRegistry(input, payment, channel)
}

// captureViaRegistry 通过 PaymentProviderRegistry 路由调用 QueryPayment。
// stripe + paypal + wechat 实现了 paymentcontract.GatewayCapturer 接口,其它 channel
// (alipay / epay / epusdt / bepusdt / tokenpay / okpay) 仅实现 webhook 回调,
// type assertion 失败时返回 ErrPaymentProviderNotSupported。
func (s *PaymentService) captureViaRegistry(input CapturePaymentInput, payment *paymentdomain.Payment, channel *paymentdomain.PaymentChannel) (*paymentdomain.Payment, error) {
	logger.Infow("payment_capture_via_registry",
		"payment_id", payment.ID,
		"provider_type", channel.ProviderType,
		"channel_type", channel.ChannelType,
	)
	if s.paymentProviderRegistry == nil {
		return nil, ErrPaymentProviderNotSupported
	}
	p, ok := s.paymentProviderRegistry.Lookup(channel.ProviderType, channel.ChannelType)
	if !ok {
		return nil, ErrPaymentProviderNotSupported
	}
	capturer, ok := p.(paymentcontract.GatewayCapturer)
	if !ok {
		logger.Warnw("payment_provider_capture_not_implemented",
			"provider_type", channel.ProviderType,
			"channel_type", channel.ChannelType,
		)
		return nil, ErrPaymentProviderNotSupported
	}

	// 第二参数是 interactionMode,不是 channelType。stripe/wechat adapter
	// 会拒绝任何非法 mode,传 channelType 会导致  一律 ErrConfigInvalid。
	if err := capturer.ValidateConfig(channel.ConfigJSON, channel.InteractionMode); err != nil {
		return nil, mapProviderErrorToService(err)
	}

	ctx, cancel := detachOutboundRequestContext(input.Context)
	defer cancel()

	queryResult, err := capturer.QueryPayment(ctx, channel.ConfigJSON, payment.ProviderRef)
	if err != nil {
		return nil, mapProviderErrorToService(err)
	}

	payload := jsonmap.JSON{}
	if queryResult.Payload != nil {
		payload = queryResult.Payload
	}
	status := strings.TrimSpace(queryResult.Status)
	if status == "" {
		status = constants.PaymentStatusPending
	}

	callbackInput := PaymentCallbackInput{
		PaymentID:   payment.ID,
		ChannelID:   channel.ID,
		Status:      status,
		ProviderRef: pickFirstNonEmpty(queryResult.ProviderRef, payment.ProviderRef),
		Amount:      queryResult.Amount,
		Currency:    strings.ToUpper(strings.TrimSpace(queryResult.Currency)),
		PaidAt:      queryResult.PaidAt,
		Payload:     payload,
	}
	return s.HandleCallback(callbackInput)
}

// mapProviderErrorToService 把 provider.ErrXxx 转换为 service 层的 ErrPaymentXxx。
func mapProviderErrorToService(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, paymentcontract.ErrGatewayConfigInvalid):
		return fmt.Errorf("%w: %v", ErrPaymentChannelConfigInvalid, err)
	case errors.Is(err, paymentcontract.ErrGatewayRequestFailed), errors.Is(err, paymentcontract.ErrGatewayAuthFailed):
		return fmt.Errorf("%w: %v", ErrPaymentGatewayRequestFailed, err)
	case errors.Is(err, paymentcontract.ErrGatewayResponseInvalid), errors.Is(err, paymentcontract.ErrGatewaySignatureInvalid):
		return fmt.Errorf("%w: %v", ErrPaymentGatewayResponseInvalid, err)
	case errors.Is(err, paymentcontract.ErrGatewayUnsupportedChannel), errors.Is(err, paymentcontract.ErrGatewayProviderNotFound):
		return ErrPaymentProviderNotSupported
	default:
		return fmt.Errorf("%w: %v", ErrPaymentGatewayRequestFailed, err)
	}
}

// ManualConfirmPaymentInput 后台人工确认支付输入。
type ManualConfirmPaymentInput struct {
	OrderID     uint
	PaymentID   uint // 必填：明确要确认的那一条支付记录
	AdminID     uint
	ProviderRef string // 第三方支付流水号 / 链上交易 Hash（可选）
	Remark      string
}

// ManualConfirmPayment 管理员在支付渠道侧核实到账后，手动把订单恢复为已支付并继续履约。
//
// 它复用支付回调的事务路径（金额/币种校验、库存消耗、分销利润入账），
// 提交后同样触发自动发货、邮件、通知、采购等后续流程，不是简单改订单状态。
// 订单已被超时取消时，会在同一事务内重开订单并重新占用自动发货卡密，库存不足则整体回滚。
func (s *PaymentService) ManualConfirmPayment(input ManualConfirmPaymentInput) (*paymentdomain.Payment, error) {
	if input.OrderID == 0 || input.AdminID == 0 || input.PaymentID == 0 {
		return nil, ErrPaymentInvalid
	}
	order, err := s.orderRepo.GetByID(input.OrderID)
	if err != nil {
		return nil, orderapp.ErrOrderFetchFailed
	}
	if order == nil {
		return nil, orderapp.ErrOrderNotFound
	}
	if order.ParentID != nil || order.PaidAt != nil ||
		(order.Status != constants.OrderStatusPendingPayment && order.Status != constants.OrderStatusCanceled) {
		return nil, orderapp.ErrOrderStatusInvalid
	}

	payment, err := s.loadManualConfirmPayment(order.ID, input.PaymentID)
	if err != nil {
		return nil, err
	}

	ref := strings.TrimSpace(input.ProviderRef)
	callbackRef := ""
	if strings.TrimSpace(payment.ProviderRef) == "" {
		callbackRef = ref
	}
	now := time.Now()
	return s.HandleCallback(PaymentCallbackInput{
		PaymentID:   payment.ID,
		OrderNo:     order.OrderNo,
		ChannelID:   payment.ChannelID,
		Status:      constants.PaymentStatusSuccess,
		ProviderRef: callbackRef,
		Amount:      payment.Amount,
		Currency:    payment.Currency,
		Payload: jsonmap.JSON{
			"manual_confirm": jsonmap.JSON{
				"admin_id":                input.AdminID,
				"remark":                  strings.TrimSpace(input.Remark),
				"provider_ref":            ref,
				"previous_order_status":   order.Status,
				"previous_payment_status": payment.Status,
				"confirmed_at":            now.Format(time.RFC3339),
			},
		},
		manualConfirm: true,
	})
}

// loadManualConfirmPayment 取出管理员明确指定要确认的支付记录：必须属于该订单。
// 异常码为 underpaid_payment_succeeded 的记录，款项已转入用户余额，不能再拿来履约，否则会重复给付。
func (s *PaymentService) loadManualConfirmPayment(orderID, paymentID uint) (*paymentdomain.Payment, error) {
	payment, err := s.paymentRepo.GetByID(paymentID)
	if err != nil {
		return nil, ErrPaymentUpdateFailed
	}
	if payment == nil {
		return nil, ErrPaymentNotFound
	}
	if payment.OrderID != orderID {
		return nil, ErrPaymentInvalid
	}
	if payment.ExceptionCode == constants.PaymentExceptionUnderpaidSucceeded {
		return nil, ErrPaymentUnderpaidCredited
	}
	return payment, nil
}

// GetWalletRechargeDetail 根据充值单 ID 查询充值单及其关联的支付记录。
// 充值单不存在时返回 ErrRechargeNotFound；关联支付查询失败时保留充值单并返回错误。
func (s *PaymentService) GetWalletRechargeDetail(id uint) (*walletdomain.RechargeOrder, *paymentdomain.Payment, error) {
	recharge, err := s.walletRepo.GetRechargeOrderByID(id)
	if err != nil {
		return nil, nil, ErrPaymentUpdateFailed
	}
	if recharge == nil {
		return nil, nil, walletcontract.ErrRechargeNotFound
	}
	payment, err := s.GetPayment(recharge.PaymentID)
	return recharge, payment, err
}

// ManualConfirmWalletRechargeInput 管理端人工确认充值支付的输入。
type ManualConfirmWalletRechargeInput struct {
	RechargeID  uint
	PaymentID   uint
	AdminID     uint
	ProviderRef string
	Remark      string
}

// ManualConfirmWalletRecharge 在管理员核实渠道到账后，人工确认充值支付并将充值金额计入用户钱包。
func (s *PaymentService) ManualConfirmWalletRecharge(input ManualConfirmWalletRechargeInput) (*paymentdomain.Payment, error) {
	if input.RechargeID == 0 || input.AdminID == 0 || input.PaymentID == 0 {
		return nil, ErrPaymentInvalid
	}
	recharge, payment, err := s.GetWalletRechargeDetail(input.RechargeID)
	if err != nil {
		return nil, err
	}
	if payment == nil || payment.OrderID != 0 || payment.ID != input.PaymentID {
		return nil, ErrPaymentInvalid
	}
	ref := strings.TrimSpace(input.ProviderRef)
	callbackRef := ""
	if strings.TrimSpace(payment.ProviderRef) == "" {
		callbackRef = ref
	}
	return s.HandleCallback(PaymentCallbackInput{
		PaymentID: payment.ID, ChannelID: payment.ChannelID, OrderNo: recharge.RechargeNo,
		ProviderRef: callbackRef, Status: constants.PaymentStatusSuccess, Amount: payment.Amount, Currency: payment.Currency,
		Payload:       jsonmap.JSON{"manual_confirm": jsonmap.JSON{"admin_id": input.AdminID, "remark": strings.TrimSpace(input.Remark), "provider_ref": ref, "confirmed_at": time.Now().Format(time.RFC3339)}},
		manualConfirm: true,
	})
}

// GetWalletRechargeRefundFeeSnapshot 查询充值退款可冲回的商家承担渠道手续费快照。
// 仅返回与充值单币种一致、无支付异常的成功在线支付；客户承担的手续费不属于商家成本。
func (s *PaymentService) GetWalletRechargeRefundFeeSnapshot(id uint) (walletcontract.RechargeRefundFeeSnapshot, error) {
	recharge, payment, err := s.GetWalletRechargeDetail(id)
	if err != nil {
		return walletcontract.RechargeRefundFeeSnapshot{}, err
	}
	if payment != nil && payment.Status == constants.PaymentStatusSuccess &&
		payment.ProviderType != constants.PaymentProviderWallet && payment.FeePolicy == constants.PaymentFeePolicyMerchantAbsorbed &&
		strings.TrimSpace(payment.ExceptionCode) == "" && strings.EqualFold(payment.Currency, recharge.Currency) {
		return walletcontract.RechargeRefundFeeSnapshot{PaymentAmount: payment.Amount, PaymentFee: payment.FeeAmount}, nil
	}
	return walletcontract.RechargeRefundFeeSnapshot{}, nil
}

// SetUpstreamPurchaseChecker 注入付款前核验，所有在线、余额及混合支付共用此入口。
func (s *PaymentService) SetUpstreamPurchaseChecker(checker mappingcontract.PurchaseChecker) {
	if s != nil {
		s.upstreamPurchaseChecker = checker
	}
}

// CheckPendingPaymentAvailability 只核验待付订单，供恢复旧支付链接使用，不创建支付或扣款。
func (s *PaymentService) CheckPendingPaymentAvailability(ctx context.Context, orderID uint) error {
	return s.checkUpstreamPayment(CreatePaymentInput{OrderID: orderID, Context: ctx})
}

// upstreamPaymentItems 汇总父订单与全部子订单的上游行，避免遗漏拆单后的商品。
func upstreamPaymentItems(order *orderdomain.Order) []orderdomain.OrderItem {
	if order == nil {
		return nil
	}
	var items []orderdomain.OrderItem
	for _, item := range order.Items {
		if item.FulfillmentType == constants.FulfillmentTypeUpstream {
			items = append(items, item)
		}
	}
	for i := range order.Children {
		items = append(items, upstreamPaymentItems(&order.Children[i])...)
	}
	return items
}

// checkUpstreamPayment 在任何扣款或支付链接复用之前查询远端，事务内不发 HTTP 请求。
func (s *PaymentService) checkUpstreamPayment(input CreatePaymentInput) error {
	order, err := s.orderRepo.GetByID(input.OrderID)
	if err != nil {
		return orderapp.ErrOrderFetchFailed
	}
	if order == nil {
		return orderapp.ErrOrderNotFound
	}
	if order.ParentID != nil {
		return ErrPaymentInvalid
	}
	if order.Status != constants.OrderStatusPendingPayment || (order.ExpiresAt != nil && !order.ExpiresAt.After(time.Now())) {
		return orderapp.ErrOrderStatusInvalid
	}
	items := upstreamPaymentItems(order)
	if len(items) == 0 {
		return nil
	}
	if s.upstreamPurchaseChecker == nil {
		paymentLogger("order_id", order.ID).Errorw("payment_upstream_checker_missing")
		return mappingcontract.ErrUpstreamCheckFailed
	}
	purchases := make([]mappingcontract.PurchaseItem, 0, len(items))
	for _, item := range items {
		purchases = append(purchases, mappingcontract.PurchaseItem{LocalSKUID: item.SKUID, Quantity: item.Quantity})
	}
	parent := input.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := s.upstreamPurchaseChecker.CheckUpstreamPurchasable(ctx, purchases); err != nil {
		paymentLogger("order_id", order.ID).Warnw("payment_upstream_purchase_check_failed", "items", len(purchases), "error", err)
		return err
	}
	return nil
}

// validateUpstreamPaymentInTx 在收款事务中重读本地停售状态，不能复用核验前的商品快照。
func validateUpstreamPaymentInTx(order *orderdomain.Order, tx paymentcontract.Transaction) error {
	for _, item := range upstreamPaymentItems(order) {
		product, err := tx.Products().GetByID(strconv.FormatUint(uint64(item.ProductID), 10))
		if err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if product == nil || !product.IsActive || product.FulfillmentType != constants.FulfillmentTypeUpstream {
			return mappingcontract.ErrUpstreamProductNotFound
		}
		sku, err := tx.ProductSKUs().GetByID(item.SKUID)
		if err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if sku == nil || !sku.IsActive || sku.ProductID != product.ID {
			return mappingcontract.ErrUpstreamProductNotFound
		}
	}
	return nil
}
