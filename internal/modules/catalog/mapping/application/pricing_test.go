package application

import (
	"errors"
	"testing"

	"github.com/dujiao-next/internal/constants"
	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

func TestCalculateMarkedUpPrice(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		markup   string
		rounding string
		expected string
	}{
		{"zero markup returns original", "10.00", "0", "none", "10.00"},
		{"50% markup", "10.00", "50", "none", "15.00"},
		{"100% markup doubles price", "10.00", "100", "none", "20.00"},
		{"200% markup triples price", "10.00", "200", "none", "30.00"},
		{"small price with markup", "0.01", "100", "none", "0.02"},
		{"fractional result rounds to 2 decimals", "10.00", "33", "none", "13.30"},
		{"negative markup floors at zero", "5.00", "-200", "none", "0"},
		{"ceil_int rounds up", "10.00", "23.4", "ceil_int", "13.00"},
		{"ceil_int exact integer stays", "10.00", "100", "ceil_int", "20"},
		{"ceil_tenth rounds up to 0.1", "10.00", "23.4", "ceil_tenth", "12.40"},
		{"ceil_tenth exact tenth stays", "10.00", "50", "ceil_tenth", "15.00"},
		{"zero price stays zero", "0.00", "100", "none", "0.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := decimal.RequireFromString(tt.upstream)
			markup := decimal.RequireFromString(tt.markup)
			expected := decimal.RequireFromString(tt.expected)

			result := CalculateMarkedUpPrice(upstream, markup, tt.rounding)
			if !result.Equal(expected) {
				t.Errorf("CalculateMarkedUpPrice(%s, %s%%, %s) = %s, want %s",
					tt.upstream, tt.markup, tt.rounding, result.String(), tt.expected)
			}
		})
	}
}

func TestCalculateLocalPrice(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		rate     string
		markup   string
		rounding string
		expected string
	}{
		{"USD to CNY with markup", "1.00", "7.2", "50", "none", "10.80"},
		{"rate 1 same as no conversion", "10.00", "1", "50", "none", "15.00"},
		{"rate 0 treated as 1", "10.00", "0", "50", "none", "15.00"},
		{"negative rate treated as 1", "10.00", "-1", "50", "none", "15.00"},
		{"rate with no markup", "1.00", "7.2", "0", "none", "7.20"},
		{"rate with ceil_int", "1.00", "7.2", "50", "ceil_int", "11.00"},
		{"rate with ceil_tenth", "1.00", "7.35", "0", "ceil_tenth", "7.35"},
		{"rate with ceil_tenth fractional", "1.00", "7.34", "1", "ceil_tenth", "7.50"},
		{"high precision rate", "100.00", "0.138462", "0", "none", "13.85"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := decimal.RequireFromString(tt.upstream)
			rate := decimal.RequireFromString(tt.rate)
			markup := decimal.RequireFromString(tt.markup)
			expected := decimal.RequireFromString(tt.expected)

			result := CalculateLocalPrice(upstream, rate, markup, decimal.Zero, decimal.NewFromInt(15), "", tt.rounding)
			if !result.Equal(expected) {
				t.Errorf("CalculateLocalPrice(%s, rate=%s, %s%%, %s) = %s, want %s",
					tt.upstream, tt.rate, tt.markup, tt.rounding, result.String(), tt.expected)
			}
		})
	}
}

func TestCalculateFixedAmountPrice(t *testing.T) {
	tests := []struct{ name, upstream, rate, amount, rounding, want string }{
		{"per unit", "5", "1", "3", "none", "8"},
		{"convert before addition", "5", "7.2", "3", "none", "39"},
		{"decimal addition", "0.10", "1", "0.20", "none", "0.30"},
		{"round after addition", "1", "7.345", "0.01", "none", "7.36"},
		{"ceil integer", "5.01", "1", "3.20", "ceil_int", "9"},
		{"ceil tenth", "5.01", "1", "3.20", "ceil_tenth", "8.30"},
		{"exact integer", "5", "1", "3", "ceil_int", "8"},
		{"zero amount skips ceiling", "5.01", "1", "0", "ceil_int", "5.01"},
		{"zero amount still converts", "1", "7.345", "0", "ceil_tenth", "7.35"},
		{"zero upstream is valid", "0", "7.2", "3", "none", "3"},
		{"invalid rate retains fallback", "5", "0", "3", "none", "8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateLocalPrice(decimal.RequireFromString(tt.upstream), decimal.RequireFromString(tt.rate), decimal.NewFromInt(40), decimal.RequireFromString(tt.amount), decimal.NewFromInt(15), constants.ConnectionPriceMarkupFixedAmount, tt.rounding)
			if !got.Equal(decimal.RequireFromString(tt.want)) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
	// The saved value of the inactive mode must never affect the selected rule.
	for _, mode := range []string{"", constants.ConnectionPriceMarkupPercent} {
		got := CalculateLocalPrice(decimal.NewFromInt(5), decimal.NewFromInt(2), decimal.NewFromInt(40), decimal.NewFromInt(100), decimal.NewFromInt(15), mode, "none")
		if !got.Equal(decimal.NewFromInt(14)) {
			t.Fatalf("mode %q: got %s, want 14", mode, got)
		}
	}
}

func TestCalculateSegmentedPrice(t *testing.T) {
	tests := []struct {
		name, upstream, rate, percent, amount, rounding, want string
	}{
		{"below threshold uses fixed amount", "14", "1", "30", "5", "none", "19"},
		{"threshold equality uses fixed amount", "15", "1", "30", "5", "none", "20"},
		{"above threshold uses percentage", "15.01", "1", "30", "5", "none", "19.51"},
		{"convert before threshold comparison", "10", "2", "30", "5", "none", "26"},
		{"compare before rounding converted cost", "1", "15.004", "30", "5", "none", "19.51"},
		{"converted cost equals threshold", "3", "5", "30", "5", "none", "20"},
		{"fixed branch ceiling integer", "14.01", "1", "30", "5", "ceil_int", "20"},
		{"fixed branch ceiling tenth", "14.01", "1", "30", "5", "ceil_tenth", "19.10"},
		{"percentage branch ceiling integer", "15.01", "1", "30", "5", "ceil_int", "20"},
		{"percentage branch ceiling tenth", "15.01", "1", "30", "5", "ceil_tenth", "19.60"},
		{"zero fixed amount skips ceiling", "5.01", "1", "30", "0", "ceil_int", "5.01"},
		{"zero fixed amount still converts", "1", "7.345", "30", "0", "ceil_tenth", "7.35"},
		{"zero percentage skips ceiling", "15.01", "1", "0", "100", "ceil_int", "15.01"},
		{"zero percentage still converts", "1", "15.345", "0", "100", "ceil_tenth", "15.35"},
		{"zero upstream uses fixed branch", "0", "7.2", "30", "5", "none", "5"},
		{"invalid rate retains fallback", "15", "0", "30", "5", "none", "20"},
		{"negative rate retains fallback", "15.01", "-1", "30", "5", "none", "19.51"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateLocalPrice(
				decimal.RequireFromString(tt.upstream),
				decimal.RequireFromString(tt.rate),
				decimal.RequireFromString(tt.percent),
				decimal.RequireFromString(tt.amount),
				decimal.NewFromInt(15),
				constants.ConnectionPriceMarkupSegmented,
				tt.rounding,
			)
			if !got.Equal(decimal.RequireFromString(tt.want)) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

type productPriceUpdateRecorder struct {
	mappingcontract.ProductRepository
	id     string
	fields map[string]interface{}
	err    error
}

func (r *productPriceUpdateRecorder) QuickUpdate(id string, fields map[string]interface{}) error {
	r.id, r.fields = id, fields
	return r.err
}

type productPriceSKUReader struct {
	mappingcontract.SKURepository
	skus       []productdomain.ProductSKU
	err        error
	productID  uint
	onlyActive bool
}

func (r *productPriceSKUReader) ListByProduct(productID uint, onlyActive bool) ([]productdomain.ProductSKU, error) {
	r.productID, r.onlyActive = productID, onlyActive
	return r.skus, r.err
}

func TestRecalcProductPriceUpdatesOnlyIndependentMinimumPrices(t *testing.T) {
	amount := func(value string) money.Amount {
		return money.FromDecimal(decimal.RequireFromString(value))
	}
	skus := []productdomain.ProductSKU{
		{PriceAmount: amount("12.346"), CostPriceAmount: amount("9.871"), IsActive: true},
		{PriceAmount: amount("10.124"), CostPriceAmount: amount("11.286"), IsActive: true},
	}
	writeErr := errors.New("product update failed")
	for _, tc := range []struct {
		name       string
		skus       []productdomain.ProductSKU
		listErr    error
		updateErr  error
		wantUpdate bool
		wantPrice  string
		wantCost   string
	}{
		{name: "independent minima rounded", skus: skus, wantUpdate: true, wantPrice: "10.12", wantCost: "9.87"},
		{name: "zero prices", skus: []productdomain.ProductSKU{{PriceAmount: amount("0"), CostPriceAmount: amount("0"), IsActive: true}}, wantUpdate: true, wantPrice: "0.00", wantCost: "0.00"},
		{name: "empty SKUs", wantPrice: "100.00", wantCost: "90.00"},
		{name: "no active SKUs", skus: []productdomain.ProductSKU{{PriceAmount: amount("1"), CostPriceAmount: amount("1")}}, wantPrice: "100.00", wantCost: "90.00"},
		{name: "ignore inactive SKU", skus: append(append([]productdomain.ProductSKU{}, skus...), productdomain.ProductSKU{PriceAmount: amount("1"), CostPriceAmount: amount("1")}), wantUpdate: true, wantPrice: "10.12", wantCost: "9.87"},
		{name: "SKU query failure", skus: skus, listErr: errors.New("SKU query failed"), wantPrice: "100.00", wantCost: "90.00"},
		{name: "update failure retains memory changes", skus: skus, updateErr: writeErr, wantUpdate: true, wantPrice: "10.12", wantCost: "9.87"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			products := &productPriceUpdateRecorder{err: tc.updateErr}
			skuReader := &productPriceSKUReader{skus: tc.skus, err: tc.listErr}
			svc := &Service{products: products, skus: skuReader}
			product := &productdomain.Product{ID: 42, PriceAmount: amount("100"), CostPriceAmount: amount("90")}

			svc.recalcProductPrice(product)

			if skuReader.productID != product.ID || !skuReader.onlyActive {
				t.Fatalf("must query only active SKUs for product %d, got id=%d active=%v", product.ID, skuReader.productID, skuReader.onlyActive)
			}
			if product.PriceAmount.String() != tc.wantPrice || product.CostPriceAmount.String() != tc.wantCost {
				t.Errorf("in-memory prices = %s/%s, want %s/%s", product.PriceAmount, product.CostPriceAmount, tc.wantPrice, tc.wantCost)
			}
			if !tc.wantUpdate {
				if products.id != "" || products.fields != nil {
					t.Errorf("unexpected update without usable SKUs: id=%q fields=%+v", products.id, products.fields)
				}
				return
			}
			if products.id != "42" || len(products.fields) != 2 {
				t.Fatalf("unexpected product update: id=%q fields=%+v", products.id, products.fields)
			}
			price, priceOK := products.fields["price_amount"].(money.Amount)
			cost, costOK := products.fields["cost_price_amount"].(money.Amount)
			if !priceOK || !costOK || price.String() != tc.wantPrice || cost.String() != tc.wantCost {
				t.Errorf("persisted prices = %+v, want %s/%s", products.fields, tc.wantPrice, tc.wantCost)
			}
		})
	}
}
