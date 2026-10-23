package application

import (
	"context"
	"testing"

	"github.com/dujiao-next/internal/constants"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"

	"github.com/shopspring/decimal"
)

func TestConvertUpstreamWholesalePricesRemapsUpstreamSKUScope(t *testing.T) {
	tiers := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
		{SKUID: 201, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(80))},
	}, decimal.NewFromInt(1), decimal.Zero, decimal.Zero, decimal.NewFromInt(15), "", "none", buildUpstreamWholesaleSKUIndex(
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
	}, decimal.NewFromInt(1), decimal.Zero, decimal.Zero, decimal.NewFromInt(15), "", "none")

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
	got := convertUpstreamWholesalePrices(tiers, decimal.NewFromInt(2), decimal.NewFromInt(40), decimal.NewFromInt(3), decimal.NewFromInt(15), "fixed_amount", "none", buildUpstreamWholesaleSKUIndex(
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
	}, decimal.NewFromInt(1), decimal.Zero, decimal.NewFromInt(3), decimal.NewFromInt(15), "fixed_amount", "ceil_int")
	if len(got) != 0 {
		t.Fatalf("expected invalid rounded tiers to be rejected, got %+v", got)
	}
}

func TestConvertSegmentedWholesalePrices(t *testing.T) {
	tier := func(quantity int, cost string) productdomain.WholesalePriceTier {
		return productdomain.WholesalePriceTier{MinQuantity: quantity, UnitPrice: money.FromDecimal(decimal.RequireFromString(cost))}
	}
	for _, tt := range []struct {
		name     string
		tiers    productdomain.WholesalePriceTiers
		rate     string
		rounding string
		want     productdomain.WholesalePriceTiers
	}{
		{
			name:  "cross threshold omits more expensive tier",
			tiers: productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(10, "15"), tier(20, "14")},
			rate:  "1", rounding: "none",
			want: productdomain.WholesalePriceTiers{tier(5, "19.63"), tier(20, "19")},
		},
		{
			name:  "equal prices across threshold are redundant",
			tiers: productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(10, "14.63"), tier(20, "14.62")},
			rate:  "1", rounding: "none",
			want: productdomain.WholesalePriceTiers{tier(5, "19.63"), tier(20, "19.62")},
		},
		{
			name:  "consecutive omissions compare against last retained tier",
			tiers: productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(10, "15"), tier(15, "14.90"), tier(20, "14.80"), tier(25, "14.60")},
			rate:  "1", rounding: "none",
			want: productdomain.WholesalePriceTiers{tier(5, "19.63"), tier(25, "19.60")},
		},
		{
			name:  "sort source quantities before comparing",
			tiers: productdomain.WholesalePriceTiers{tier(20, "14"), tier(10, "15"), tier(5, "15.10")},
			rate:  "1", rounding: "none",
			want: productdomain.WholesalePriceTiers{tier(5, "19.63"), tier(20, "19")},
		},
		{
			name:  "compare after integer ceiling",
			tiers: productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(10, "15"), tier(20, "14")},
			rate:  "1", rounding: "ceil_int",
			want: productdomain.WholesalePriceTiers{tier(5, "20"), tier(20, "19")},
		},
		{
			name:  "compare after tenth ceiling",
			tiers: productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(10, "14.70"), tier(20, "14.60")},
			rate:  "1", rounding: "ceil_tenth",
			want: productdomain.WholesalePriceTiers{tier(5, "19.70"), tier(20, "19.60")},
		},
		{
			name:  "compare after two decimal rounding",
			tiers: productdomain.WholesalePriceTiers{tier(5, "4.02"), tier(10, "4.01"), tier(20, "3.91")},
			rate:  "0.1", rounding: "none",
			want: productdomain.WholesalePriceTiers{tier(5, "5.40"), tier(20, "5.39")},
		},
		{
			name:  "convert cost before selecting branch",
			tiers: productdomain.WholesalePriceTiers{tier(5, "7.55"), tier(10, "7.50"), tier(20, "7")},
			rate:  "2", rounding: "none",
			want: productdomain.WholesalePriceTiers{tier(5, "19.63"), tier(20, "19")},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := convertUpstreamWholesalePrices(tt.tiers, decimal.RequireFromString(tt.rate), decimal.NewFromInt(30), decimal.NewFromInt(5), decimal.NewFromInt(15), constants.ConnectionPriceMarkupSegmented, tt.rounding)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i, want := range tt.want {
				if got[i].MinQuantity != want.MinQuantity || !got[i].UnitPrice.Equal(want.UnitPrice.Decimal) {
					t.Errorf("tier %d: got %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

func TestConvertSegmentedWholesalePricesRejectsInvalidSourceBeforePruning(t *testing.T) {
	tier := func(quantity int, cost string) productdomain.WholesalePriceTier {
		return productdomain.WholesalePriceTier{MinQuantity: quantity, UnitPrice: money.FromDecimal(decimal.RequireFromString(cost))}
	}
	for _, tt := range []struct {
		name  string
		tiers productdomain.WholesalePriceTiers
	}{
		{"duplicate quantities", productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(5, "15")}},
		{"increasing source costs despite decreasing sale prices", productdomain.WholesalePriceTiers{tier(5, "15"), tier(10, "15.10")}},
		{"equal source costs", productdomain.WholesalePriceTiers{tier(5, "15.10"), tier(10, "15.10")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := convertUpstreamWholesalePrices(tt.tiers, decimal.NewFromInt(1), decimal.NewFromInt(30), decimal.NewFromInt(5), decimal.NewFromInt(15), constants.ConnectionPriceMarkupSegmented, "none")
			if len(got) != 0 {
				t.Fatalf("expected malformed source tiers to be rejected before pruning, got %+v", got)
			}
		})
	}
}

func TestConvertSegmentedWholesalePricesKeepsScopesIndependent(t *testing.T) {
	amount := func(cost string) money.Amount { return money.FromDecimal(decimal.RequireFromString(cost)) }
	for _, tc := range []struct {
		name  string
		codeA string
		codeB string
	}{
		{"SKU codes", "SKU-A", "SKU-B"},
		{"SKU IDs without codes", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index := upstreamWholesaleSKUIndex{byUpstreamID: map[uint]upstreamWholesaleSKURef{
				201: {ID: 11, SKUCode: tc.codeA},
				202: {ID: 12, SKUCode: tc.codeB},
			}}
			got := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
				{MinQuantity: 5, UnitPrice: amount("10")},
				{SKUID: 201, MinQuantity: 5, UnitPrice: amount("16")},
				{SKUID: 201, MinQuantity: 10, UnitPrice: amount("15")},
				{SKUID: 201, MinQuantity: 20, UnitPrice: amount("14")},
				{SKUID: 202, MinQuantity: 5, UnitPrice: amount("15.10")},
				{SKUID: 202, MinQuantity: 10, UnitPrice: amount("15")},
				{SKUID: 202, MinQuantity: 20, UnitPrice: amount("14")},
			}, decimal.NewFromInt(1), decimal.NewFromInt(30), decimal.NewFromInt(5), decimal.NewFromInt(15), constants.ConnectionPriceMarkupSegmented, "none", index)
			want := map[uint]map[int]string{
				0:  {5: "15.00"},
				11: {5: "20.80", 10: "20.00", 20: "19.00"},
				12: {5: "19.63", 20: "19.00"},
			}
			if len(got) != 6 {
				t.Fatalf("got %+v, want all 6 retained tiers", got)
			}
			for _, tier := range got {
				if tier.UnitPrice.String() != want[tier.SKUID][tier.MinQuantity] {
					t.Errorf("unexpected tier: %+v", tier)
				}
			}
			product := &productdomain.Product{WholesalePrices: got}
			price, _, matched := productdomain.ResolveWholesaleUnitPriceForSKU(product, decimal.NewFromInt(20), 11, tc.codeA, 5, 5)
			if matched || !price.Equal(decimal.NewFromInt(20)) {
				t.Fatalf("first SKU tier must keep precedence over universal tiers even above base price; got %s, matched=%v", price, matched)
			}
		})
	}
}

func TestConvertSegmentedWholesalePricesUsesCanonicalMappedScope(t *testing.T) {
	amount := func(cost string) money.Amount { return money.FromDecimal(decimal.RequireFromString(cost)) }
	index := buildUpstreamWholesaleSKUIndex(
		[]productdomain.ProductSKU{{ID: 11, SKUCode: "SKU-A"}},
		[]upstream.UpstreamSKU{{ID: 201, SKUCode: "SKU-A"}}, nil,
	)
	for _, tt := range []struct {
		name     string
		quantity int
		wantLen  int
	}{
		{"mixed ID and code scope compare together", 10, 1},
		{"duplicate quantities after SKU remapping rejected", 5, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
				{SKUID: 201, MinQuantity: 5, UnitPrice: amount("15.10")},
				{SKUCode: " sku-a ", MinQuantity: tt.quantity, UnitPrice: amount("15")},
			}, decimal.NewFromInt(1), decimal.NewFromInt(30), decimal.NewFromInt(5), decimal.NewFromInt(15), constants.ConnectionPriceMarkupSegmented, "none", index)
			if len(got) != tt.wantLen {
				t.Fatalf("got %+v, want %d tiers", got, tt.wantLen)
			}
			if len(got) > 0 && (got[0].SKUID != 11 || got[0].SKUCode != "SKU-A" || got[0].UnitPrice.String() != "19.63") {
				t.Fatalf("expected canonical local SKU scope, got %+v", got)
			}
		})
	}
}

func TestConvertWholesaleRetainsExistingValidationAfterPercentRounding(t *testing.T) {
	for _, mode := range []string{"", constants.ConnectionPriceMarkupPercent} {
		got := convertUpstreamWholesalePrices(productdomain.WholesalePriceTiers{
			{MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.RequireFromString("4.02"))},
			{MinQuantity: 10, UnitPrice: money.FromDecimal(decimal.RequireFromString("4.01"))},
		}, decimal.NewFromInt(1), decimal.NewFromInt(30), decimal.NewFromInt(5), decimal.NewFromInt(15), mode, "ceil_int")
		if len(got) != 0 {
			t.Fatalf("mode %q: expected rounded equal tiers to remain invalid, got %+v", mode, got)
		}
	}
}
