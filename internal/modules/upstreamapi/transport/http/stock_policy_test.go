package upstreamhttp

import (
	"errors"
	"testing"

	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/constants"
)

type stockProductMappings struct {
	mappings []mappingdomain.Mapping
	err      error
}

func (r stockProductMappings) ListByLocalProductIDs([]uint) ([]mappingdomain.Mapping, error) {
	return r.mappings, r.err
}

type stockSKUMapping struct {
	mapping *mappingdomain.SKUMapping
	err     error
}

func (r stockSKUMapping) GetByLocalSKUID(uint) (*mappingdomain.SKUMapping, error) {
	return r.mapping, r.err
}

func TestApplyUpstreamStockRequiresActiveProductAndSKUMappings(t *testing.T) {
	for _, scenario := range []string{"normal", "no_product_mapping", "product_mapping_error", "mapping_disabled", "upstream_inactive", "product_disabled", "local_sku_disabled", "no_sku_mapping", "sku_mapping_error", "upstream_sku_disabled", "wrong_mapping", "invalid_stock"} {
		t.Run(scenario, func(t *testing.T) {
			productMappings := stockProductMappings{mappings: []mappingdomain.Mapping{{ID: 10, LocalProductID: 1, IsActive: true, UpstreamStatus: mappingdomain.UpstreamStatusActive}}}
			skuMapping := stockSKUMapping{mapping: &mappingdomain.SKUMapping{ProductMappingID: 10, UpstreamIsActive: true, UpstreamStock: 17}}
			products := []productdomain.Product{{ID: 1, IsActive: true, FulfillmentType: constants.FulfillmentTypeUpstream, SKUs: []productdomain.ProductSKU{{ID: 11, IsActive: true, UpstreamStock: 999}}}}
			switch scenario {
			case "no_product_mapping":
				productMappings.mappings = nil
			case "product_mapping_error":
				productMappings.err = errors.New("query failed")
			case "mapping_disabled":
				productMappings.mappings[0].IsActive = false
			case "upstream_inactive":
				productMappings.mappings[0].UpstreamStatus = mappingdomain.UpstreamStatusInactive
			case "product_disabled":
				products[0].IsActive = false
			case "local_sku_disabled":
				products[0].SKUs[0].IsActive = false
			case "no_sku_mapping":
				skuMapping.mapping = nil
			case "sku_mapping_error":
				skuMapping.err = errors.New("query failed")
			case "upstream_sku_disabled":
				skuMapping.mapping.UpstreamIsActive = false
			case "wrong_mapping":
				skuMapping.mapping.ProductMappingID = 99
			case "invalid_stock":
				skuMapping.mapping.UpstreamStock = -2
			}
			handler := &Handler{Dependencies: Dependencies{ProductMappings: productMappings, SKUMappings: skuMapping}}
			handler.applyUpstreamStockToProducts(products)
			want := 0
			if scenario == "normal" {
				want = 17
			}
			if products[0].SKUs[0].UpstreamStock != want {
				t.Fatalf("stock want %d got %d", want, products[0].SKUs[0].UpstreamStock)
			}
		})
	}
}

func TestComputeSKUStockUsesRemainingManualStockWithoutSubtractingLockedAgain(t *testing.T) {
	product := productdomain.Product{FulfillmentType: constants.FulfillmentTypeManual}
	sku := productdomain.ProductSKU{
		ManualStockTotal:  5,
		ManualStockLocked: 4,
	}

	status, quantity := computeSKUStock(product, sku)
	if quantity != 5 {
		t.Fatalf("manual_stock_total already represents remaining stock; quantity want 5 got %d", quantity)
	}
	if status != constants.ProductStockStatusLowStock {
		t.Fatalf("upstream API threshold should classify quantity 5 as low_stock, got %q", status)
	}
}
