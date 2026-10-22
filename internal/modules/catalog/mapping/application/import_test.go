package application

import (
	"context"
	"testing"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"

	"github.com/shopspring/decimal"
)

func TestConvertUpstreamWholesalePricesRemapsUpstreamSKUScope(t *testing.T) {
	tiers := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
		{SKUID: 201, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(80))},
	}, decimal.NewFromInt(1), decimal.Zero, decimal.Zero, "", "none", buildUpstreamWholesaleSKUIndex(
		[]productdomain.ProductSKU{{ID: 11, SKUCode: "SKU-A"}},
		[]upstream.UpstreamSKU{{ID: 201, SKUCode: "SKU-A"}},
		nil,
	))

	if len(tiers) != 1 {
		t.Fatalf("expected 1 tier, got %d", len(tiers))
	}
	if tiers[0].SKUID != 11 || tiers[0].SKUCode != "SKU-A" {
		t.Fatalf("expected upstream SKU scope to be remapped, got %+v", tiers[0])
	}
}

func TestConvertUpstreamWholesalePricesDropsUnmappedUpstreamSKUID(t *testing.T) {
	tiers := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
		{SKUID: 201, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(80))},
	}, decimal.NewFromInt(1), decimal.Zero, decimal.Zero, "", "none")

	if len(tiers) != 0 {
		t.Fatalf("expected unmapped upstream sku_id tier to be dropped, got %+v", tiers)
	}
}

// pagedProductsAdapter 只实现 ListProducts：前 total 条商品按页返回，ignorePage 模拟上游忽略 page 参数。
type pagedProductsAdapter struct {
	upstream.Adapter
	total      int
	ignorePage bool
	calls      int
}

func (a *pagedProductsAdapter) ListProducts(_ context.Context, opts upstream.ListProductsOpts) (*upstream.ProductListResult, error) {
	a.calls++
	page := opts.Page
	if a.ignorePage {
		page = 1
	}
	var items []upstream.UpstreamProduct
	for id := (page-1)*opts.PageSize + 1; id <= a.total && len(items) < opts.PageSize; id++ {
		items = append(items, upstream.UpstreamProduct{ID: uint(id), CategoryID: uint(id % 3)})
	}
	if a.ignorePage {
		return &upstream.ProductListResult{Total: 1 << 30, Items: items}, nil
	}
	return &upstream.ProductListResult{Total: a.total, Items: items}, nil
}

func TestListAllUpstreamProductsWalksEveryPage(t *testing.T) {
	adapter := &pagedProductsAdapter{total: 120}
	products, err := listAllUpstreamProducts(context.Background(), adapter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(products) != 120 || adapter.calls != 3 {
		t.Fatalf("expected 120 products in 3 calls, got %d in %d", len(products), adapter.calls)
	}
}

func TestListAllUpstreamProductsStopsAtPageLimit(t *testing.T) {
	adapter := &pagedProductsAdapter{total: 50, ignorePage: true}
	if _, err := listAllUpstreamProducts(context.Background(), adapter); err == nil {
		t.Fatal("expected error when upstream pagination never terminates")
	}
	if adapter.calls != maxUpstreamProductPages {
		t.Fatalf("expected %d calls, got %d", maxUpstreamProductPages, adapter.calls)
	}
}

func TestConvertWholesaleFixedAmountPrices(t *testing.T) {
	tiers := productdomain.WholesalePriceTiers{
		{MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(4))},
		{MinQuantity: 10, UnitPrice: money.FromDecimal(decimal.NewFromInt(3))},
		{SKUID: 201, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(2))},
	}
	got := convertUpstreamWholesalePrices(tiers, decimal.NewFromInt(2), decimal.NewFromInt(40), decimal.NewFromInt(3), "fixed_amount", "none", buildUpstreamWholesaleSKUIndex(
		[]productdomain.ProductSKU{{ID: 11, SKUCode: "SKU-A"}}, []upstream.UpstreamSKU{{ID: 201, SKUCode: "SKU-A"}}, nil,
	))
	if len(got) != 3 {
		t.Fatalf("tiers: %+v", got)
	}
	want := map[uint]map[int]string{0: {5: "11.00", 10: "9.00"}, 11: {5: "7.00"}}
	for _, tier := range got {
		if tier.UnitPrice.String() != want[tier.SKUID][tier.MinQuantity] {
			t.Fatalf("unexpected tier: %+v", tier)
		}
	}
}

func TestConvertWholesaleRetainsExistingValidationAfterFixedRounding(t *testing.T) {
	// Existing validation rejects tiers collapsed by rounding; do not invent new tiers.
	got := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
		{MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.RequireFromString("4.02"))},
		{MinQuantity: 10, UnitPrice: money.FromDecimal(decimal.RequireFromString("4.01"))},
	}, decimal.NewFromInt(1), decimal.Zero, decimal.NewFromInt(3), "fixed_amount", "ceil_int")
	if len(got) != 0 {
		t.Fatalf("expected invalid rounded tiers to be rejected, got %+v", got)
	}
}
