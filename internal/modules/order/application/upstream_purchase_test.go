package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dujiao-next/internal/constants"
	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
)

type purchaseCheckerFunc func(context.Context, []mappingcontract.PurchaseItem) error

func (f purchaseCheckerFunc) CheckUpstreamPurchasable(ctx context.Context, items []mappingcontract.PurchaseItem) error {
	return f(ctx, items)
}

type purchaseEnabledQueue struct{ ordercontract.Queue }

func (purchaseEnabledQueue) Enabled() bool { return true }

func upstreamOrderFixture(t *testing.T, name string) wholesaleOrderFixture {
	t.Helper()
	fixture := setupWholesaleOrderFixture(t, name, nil, nil, nil)
	if err := fixture.db.Model(&productdomain.Product{}).Where("id = ?", fixture.product.ID).
		Update("fulfillment_type", constants.FulfillmentTypeUpstream).Error; err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestUpstreamOrderPreviewDoesNotQueryProvider(t *testing.T) {
	fixture := upstreamOrderFixture(t, "preview_no_provider")
	calls := 0
	fixture.svc.SetUpstreamPurchaseChecker(purchaseCheckerFunc(func(context.Context, []mappingcontract.PurchaseItem) error {
		calls++
		return mappingcontract.ErrUpstreamProductNotFound
	}))
	_, err := fixture.svc.previewOrder(orderCreateParams{Items: []CreateOrderItem{{ProductID: fixture.product.ID, SKUID: fixture.sku.ID, Quantity: 1}}})
	if err != nil {
		t.Fatalf("local preview rejected: %v", err)
	}
	if calls != 0 {
		t.Fatalf("preview made %d provider checks", calls)
	}
}

func TestCreateUpstreamOrderFailsBeforePersistence(t *testing.T) {
	for _, checkErr := range []error{mappingcontract.ErrUpstreamProductNotFound, mappingcontract.ErrUpstreamStockInsufficient, mappingcontract.ErrUpstreamCheckFailed} {
		t.Run(checkErr.Error(), func(t *testing.T) {
			fixture := upstreamOrderFixture(t, "create_blocked")
			fixture.svc.queueClient = purchaseEnabledQueue{}
			calls := 0
			fixture.svc.SetUpstreamPurchaseChecker(purchaseCheckerFunc(func(_ context.Context, items []mappingcontract.PurchaseItem) error {
				calls++
				if len(items) != 1 || items[0].LocalSKUID != fixture.sku.ID || items[0].Quantity != 2 {
					t.Fatalf("unexpected purchase items: %+v", items)
				}
				return checkErr
			}))
			_, err := fixture.svc.CreateOrder(CreateOrderInput{UserID: 1, Items: []CreateOrderItem{{ProductID: fixture.product.ID, SKUID: fixture.sku.ID, Quantity: 2}}, SkipRiskControl: true})
			if !errors.Is(err, checkErr) || calls != 1 {
				t.Fatalf("create err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestCreateUpstreamOrderRequiresChecker(t *testing.T) {
	fixture := upstreamOrderFixture(t, "create_checker_required")
	fixture.svc.queueClient = purchaseEnabledQueue{}
	_, err := fixture.svc.CreateOrder(CreateOrderInput{UserID: 1, Items: []CreateOrderItem{{ProductID: fixture.product.ID, SKUID: fixture.sku.ID, Quantity: 1}}, SkipRiskControl: true})
	if !errors.Is(err, mappingcontract.ErrUpstreamCheckFailed) {
		t.Fatalf("missing checker err=%v", err)
	}
}

func TestCreateUpstreamOrderRereadsStatusAfterProviderCheck(t *testing.T) {
	fixture := upstreamOrderFixture(t, "create_concurrent_stop")
	fixture.svc.queueClient = purchaseEnabledQueue{}
	fixture.svc.SetUpstreamPurchaseChecker(purchaseCheckerFunc(func(context.Context, []mappingcontract.PurchaseItem) error {
		return fixture.db.Model(&productdomain.Product{}).Where("id = ?", fixture.product.ID).Update("is_active", false).Error
	}))
	_, err := fixture.svc.CreateOrder(CreateOrderInput{UserID: 1, Items: []CreateOrderItem{{ProductID: fixture.product.ID, SKUID: fixture.sku.ID, Quantity: 1}}, SkipRiskControl: true})
	if !errors.Is(err, ErrProductNotAvailable) {
		t.Fatalf("concurrent stop err=%v", err)
	}
}
