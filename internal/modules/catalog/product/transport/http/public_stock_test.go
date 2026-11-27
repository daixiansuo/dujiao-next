package producthttp

import (
	"errors"
	"testing"

	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/constants"
)

type stockProductMapping struct {
	mapping *mappingdomain.Mapping
	err     error
}

func (r stockProductMapping) GetByLocalProductID(uint) (*mappingdomain.Mapping, error) {
	return r.mapping, r.err
}

type stockSKUMappings struct {
	mappings []mappingdomain.SKUMapping
	err      error
}

func (r stockSKUMappings) ListByProductMapping(uint) ([]mappingdomain.SKUMapping, error) {
	return r.mappings, r.err
}

func TestDecorateUpstreamStockRequiresAvailableMappingAndSKU(t *testing.T) {
	for _, scenario := range []string{"normal", "no_mapping", "mapping_error", "mapping_disabled", "upstream_inactive", "upstream_deleted", "no_skus", "sku_error", "upstream_sku_disabled", "local_sku_disabled", "invalid_stock", "wrong_mapping", "product_disabled"} {
		t.Run(scenario, func(t *testing.T) {
			mapping := &mappingdomain.Mapping{ID: 10, LocalProductID: 1, IsActive: true, UpstreamStatus: mappingdomain.UpstreamStatusActive, UpstreamFulfillmentType: constants.FulfillmentTypeAuto}
			productMapping := stockProductMapping{mapping: mapping}
			skuMappings := stockSKUMappings{mappings: []mappingdomain.SKUMapping{{ProductMappingID: 10, LocalSKUID: 11, UpstreamStock: 8, UpstreamIsActive: true}}}
			product := &productdomain.Product{ID: 1, IsActive: true, FulfillmentType: constants.FulfillmentTypeUpstream, SKUs: []productdomain.ProductSKU{{ID: 11, IsActive: true, UpstreamStock: 999, AutoStockAvailable: 999, ManualStockTotal: 999}}}
			switch scenario {
			case "no_mapping":
				productMapping.mapping = nil
			case "mapping_error":
				productMapping.err = errors.New("query failed")
			case "mapping_disabled":
				mapping.IsActive = false
			case "upstream_inactive":
				mapping.UpstreamStatus = mappingdomain.UpstreamStatusInactive
			case "upstream_deleted":
				mapping.UpstreamStatus = mappingdomain.UpstreamStatusDeleted
			case "no_skus":
				skuMappings.mappings = nil
			case "sku_error":
				skuMappings.err = errors.New("query failed")
			case "upstream_sku_disabled":
				skuMappings.mappings[0].UpstreamIsActive = false
			case "local_sku_disabled":
				product.SKUs[0].IsActive = false
			case "invalid_stock":
				skuMappings.mappings[0].UpstreamStock = -2
			case "wrong_mapping":
				skuMappings.mappings[0].ProductMappingID = 99
			case "product_disabled":
				product.IsActive = false
			}
			handler := &PublicHandler{mappings: productMapping, skuMappings: skuMappings}
			view := publicProductView{Product: *product}
			handler.decorateProductStock(product, &view)
			if scenario == "normal" {
				if view.IsSoldOut || view.AutoStockAvailable != 8 || view.Product.SKUs[0].UpstreamStock != 8 {
					t.Fatalf("valid mapping must show current stock: %+v", view)
				}
				return
			}
			if !view.IsSoldOut || view.StockStatus != constants.ProductStockStatusOutOfStock || view.Product.SKUs[0].UpstreamStock != 0 || view.Product.SKUs[0].AutoStockAvailable != 0 || view.Product.SKUs[0].ManualStockTotal != 0 {
				t.Fatalf("unavailable mapping must not expose stale stock: %+v", view)
			}
			if scenario == "no_mapping" && view.Product.FulfillmentType != constants.FulfillmentTypeUpstream {
				t.Fatal("missing mapping must not convert upstream product into a manual product")
			}
		})
	}
}

func TestDecorateUpstreamStockIgnoresInactiveLocalSKUInTotal(t *testing.T) {
	product := &productdomain.Product{ID: 1, IsActive: true, FulfillmentType: constants.FulfillmentTypeUpstream, SKUs: []productdomain.ProductSKU{{ID: 11, IsActive: true}, {ID: 12, IsActive: false}}}
	handler := &PublicHandler{
		mappings: stockProductMapping{mapping: &mappingdomain.Mapping{ID: 10, IsActive: true, UpstreamStatus: mappingdomain.UpstreamStatusActive}},
		skuMappings: stockSKUMappings{mappings: []mappingdomain.SKUMapping{
			{ProductMappingID: 10, LocalSKUID: 11, UpstreamIsActive: true, UpstreamStock: 3},
			{ProductMappingID: 10, LocalSKUID: 12, UpstreamIsActive: true, UpstreamStock: -1},
		}},
	}
	view := publicProductView{Product: *product}
	handler.decorateProductStock(product, &view)
	if view.IsSoldOut || view.ManualStockAvailable != 3 || view.StockStatus == constants.ProductStockStatusUnlimited {
		t.Fatalf("disabled local SKU must not contribute stock: %+v", view)
	}
}

func TestDecorateProductStock_AutoSkipsInactiveSKUs(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:              1,
		FulfillmentType: constants.FulfillmentTypeAuto,
		SKUs: []productdomain.ProductSKU{
			{
				ID:                 11,
				SKUCode:            productdomain.DefaultSKUCode,
				IsActive:           true,
				AutoStockAvailable: 2,
				AutoStockTotal:     3,
				AutoStockLocked:    1,
				AutoStockSold:      4,
			},
			{
				ID:                 12,
				SKUCode:            "DISABLED",
				IsActive:           false,
				AutoStockAvailable: 100,
				AutoStockTotal:     120,
				AutoStockLocked:    20,
				AutoStockSold:      50,
			},
		},
	}

	item := publicProductView{Product: *product}
	h.decorateProductStock(product, &item)

	if item.AutoStockAvailable != 2 {
		t.Fatalf("expected auto_stock_available=2, got %d", item.AutoStockAvailable)
	}
	if item.AutoStockTotal != 3 {
		t.Fatalf("expected auto_stock_total=3, got %d", item.AutoStockTotal)
	}
	if item.AutoStockLocked != 1 {
		t.Fatalf("expected auto_stock_locked=1, got %d", item.AutoStockLocked)
	}
	if item.AutoStockSold != 4 {
		t.Fatalf("expected auto_stock_sold=4, got %d", item.AutoStockSold)
	}
	if item.IsSoldOut {
		t.Fatalf("expected product not sold out when active sku has stock")
	}
}

func TestPublicProductResponseStatusModeMasksExactStock(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:               1,
		FulfillmentType:  constants.FulfillmentTypeManual,
		StockDisplayMode: constants.ProductStockDisplayStatus,
		SKUs: []productdomain.ProductSKU{
			{
				ID:               11,
				SKUCode:          productdomain.DefaultSKUCode,
				IsActive:         true,
				ManualStockTotal: 37,
			},
		},
	}

	resp, err := h.decoratePublicProduct(product, nil)
	if err != nil {
		t.Fatalf("decoratePublicProduct failed: %v", err)
	}

	if resp.StockDisplayMode != constants.ProductStockDisplayStatus {
		t.Fatalf("expected stock_display_mode=status, got %q", resp.StockDisplayMode)
	}
	if !resp.StockQuantityHidden {
		t.Fatalf("expected product stock quantity to be hidden")
	}
	if resp.ManualStockAvailable == 37 {
		t.Fatalf("expected product manual stock to be masked, got exact value %d", resp.ManualStockAvailable)
	}
	if resp.StockDisplay != constants.ProductStockStatusInStock {
		t.Fatalf("expected product stock display in_stock, got %q", resp.StockDisplay)
	}
	if len(resp.SKUs) != 1 {
		t.Fatalf("expected one sku, got %d", len(resp.SKUs))
	}
	sku := resp.SKUs[0]
	if !sku.StockQuantityHidden {
		t.Fatalf("expected sku stock quantity to be hidden")
	}
	if sku.ManualStockTotal == 37 {
		t.Fatalf("expected sku manual stock to be masked, got exact value %d", sku.ManualStockTotal)
	}
	if sku.StockStatus != constants.ProductStockStatusInStock {
		t.Fatalf("expected sku stock status in_stock, got %q", sku.StockStatus)
	}
	if sku.IsSoldOut {
		t.Fatalf("expected sku to remain purchasable")
	}
}

func TestPublicProductResponseRangeModeReturnsBucketOnly(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:               1,
		FulfillmentType:  constants.FulfillmentTypeManual,
		StockDisplayMode: constants.ProductStockDisplayRange,
		SKUs: []productdomain.ProductSKU{
			{
				ID:               11,
				SKUCode:          productdomain.DefaultSKUCode,
				IsActive:         true,
				ManualStockTotal: 42,
			},
		},
	}

	resp, err := h.decoratePublicProduct(product, nil)
	if err != nil {
		t.Fatalf("decoratePublicProduct failed: %v", err)
	}

	if resp.StockDisplay != constants.ProductStockDisplayRange21To50 {
		t.Fatalf("expected product stock range 21-50, got %q", resp.StockDisplay)
	}
	if resp.StockRangeMin == nil || *resp.StockRangeMin != 21 {
		t.Fatalf("expected range min 21, got %+v", resp.StockRangeMin)
	}
	if resp.StockRangeMax == nil || *resp.StockRangeMax != 50 {
		t.Fatalf("expected range max 50, got %+v", resp.StockRangeMax)
	}
	if resp.ManualStockAvailable == 42 {
		t.Fatalf("expected product manual stock to be masked, got exact value %d", resp.ManualStockAvailable)
	}
	if len(resp.SKUs) != 1 {
		t.Fatalf("expected one sku, got %d", len(resp.SKUs))
	}
	sku := resp.SKUs[0]
	if sku.StockDisplay != constants.ProductStockDisplayRange21To50 {
		t.Fatalf("expected sku stock range 21-50, got %q", sku.StockDisplay)
	}
	if sku.StockRangeMin == nil || *sku.StockRangeMin != 21 {
		t.Fatalf("expected sku range min 21, got %+v", sku.StockRangeMin)
	}
	if sku.StockRangeMax == nil || *sku.StockRangeMax != 50 {
		t.Fatalf("expected sku range max 50, got %+v", sku.StockRangeMax)
	}
	if sku.ManualStockTotal == 42 {
		t.Fatalf("expected sku manual stock to be masked, got exact value %d", sku.ManualStockTotal)
	}
}
