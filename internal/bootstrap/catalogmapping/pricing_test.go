package catalogmappingbootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/constants"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	siteconnectiongormstore "github.com/dujiao-next/internal/modules/siteconnection/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"
	"github.com/shopspring/decimal"
)

func TestMarkupManualAndScheduledSync(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		for _, autoPrice := range []bool{false, true} {
			for _, mode := range []string{constants.ConnectionPriceMarkupPercent, constants.ConnectionPriceMarkupFixedAmount, constants.ConnectionPriceMarkupSegmented} {
				t.Run(fmt.Sprintf("scheduled=%v/auto=%v/mode=%s", scheduled, autoPrice, mode), func(t *testing.T) {
					source := upstream.UpstreamProduct{
						ID: 101, Title: jsonmap.JSON{"zh-CN": "upstream title"}, PriceAmount: "5.00", IsActive: true,
						SKUs: []upstream.UpstreamSKU{
							{ID: 201, SKUCode: "SKU-A", PriceAmount: "5.00", IsActive: true, StockQuantity: 20},
							{ID: 202, SKUCode: "SKU-B", PriceAmount: "7.00", IsActive: true, StockQuantity: 30},
						},
						WholesalePrices: productdomain.WholesalePriceTiers{{SKUID: 201, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(4))}},
					}
					handler := func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						if scheduled {
							listProductsHandler([]upstream.UpstreamProduct{source}, true)(w, r)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": source})
					}
					svc, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "mapping.db"), handler)
					defer cleanup()
					pool, err := db.DB()
					if err != nil {
						t.Fatal(err)
					}
					pool.SetMaxOpenConns(1)
					defer pool.Close()
					if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{
						"price_markup_mode": mode, "price_markup_amount": "3.00", "price_markup_percent": 40, "price_markup_threshold": "12.00", "exchange_rate": 2, "auto_sync_price": autoPrice,
					}).Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&productdomain.Product{}).Where("id = ?", mapping.LocalProductID).Updates(map[string]any{
						"title_json": jsonmap.JSON{"zh-CN": "local title"}, "description_json": jsonmap.JSON{"zh-CN": "local description"},
					}).Error; err != nil {
						t.Fatal(err)
					}
					// Repeat to exercise updates of newly discovered SKUs and detect cumulative markup.
					for range 2 {
						if scheduled {
							err = svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
						} else {
							err = svc.SyncProduct(mapping.ID)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					var skus []productdomain.ProductSKU
					if err := db.Where("product_id = ?", mapping.LocalProductID).Order("id").Find(&skus).Error; err != nil {
						t.Fatal(err)
					}
					if len(skus) != 2 {
						t.Fatalf("got %d skus, want 2", len(skus))
					}
					existingPrice, newPrice, wholesale := "13.00", "17.00", "11.00"
					if mode == constants.ConnectionPriceMarkupPercent {
						existingPrice, newPrice, wholesale = "14.00", "19.60", "11.20"
					}
					if mode == constants.ConnectionPriceMarkupSegmented {
						newPrice = "19.60"
					}
					if !autoPrice {
						existingPrice = "10.00"
					}
					if skus[0].PriceAmount.String() != existingPrice || skus[1].PriceAmount.String() != newPrice || skus[1].CostPriceAmount.String() != "14.00" {
						t.Fatalf("unexpected SKU pricing: %+v", skus)
					}
					if autoPrice && skus[0].CostPriceAmount.String() != "10.00" {
						t.Fatalf("cost must exclude markup: %+v", skus[0])
					}
					var product productdomain.Product
					if err := db.First(&product, mapping.LocalProductID).Error; err != nil {
						t.Fatal(err)
					}
					if product.PriceAmount.String() != existingPrice || product.TitleJSON["zh-CN"] != "local title" || product.DescriptionJSON["zh-CN"] != "local description" {
						t.Fatalf("sync changed manual fields or base price: %+v", product)
					}
					if len(product.WholesalePrices) != 1 || product.WholesalePrices[0].SKUID != skus[0].ID || product.WholesalePrices[0].UnitPrice.String() != wholesale {
						t.Fatalf("wholesale pricing/scope: %+v", product.WholesalePrices)
					}
					var snapshot mappingdomain.SKUMapping
					if err := db.Where("local_sku_id = ?", skus[0].ID).First(&snapshot).Error; err != nil {
						t.Fatal(err)
					}
					if snapshot.UpstreamPrice.String() != "5.00" {
						t.Fatalf("source snapshot must exclude FX/markup: %+v", snapshot)
					}

					// Explicit reapply deliberately overwrites ordinary prices with auto-update disabled.
					for range 2 {
						if _, err := svc.ReapplyMarkup(mapping.ConnectionID); err != nil {
							t.Fatal(err)
						}
					}
					want := "13.00"
					if mode == constants.ConnectionPriceMarkupPercent {
						want = "14.00"
					}
					if err := db.First(&skus[0], skus[0].ID).Error; err != nil {
						t.Fatal(err)
					}
					if skus[0].PriceAmount.String() != want {
						t.Fatalf("reapply compounded markup: got %s, want %s", skus[0].PriceAmount, want)
					}
					if err := db.First(&product, product.ID).Error; err != nil {
						t.Fatal(err)
					}
					if product.WholesalePrices[0].UnitPrice.String() != wholesale {
						t.Fatal("reapply must not mutate wholesale tiers")
					}

					if mode == constants.ConnectionPriceMarkupSegmented {
						connSvc := siteconnectionapp.NewService(siteconnectiongormstore.New(db), "test-secret-key", t.TempDir())
						connSvc.SetMarkupReapplier(svc)
						threshold := money.FromDecimal(decimal.NewFromInt(7))
						if _, err := connSvc.Update(mapping.ConnectionID, siteconnectionapp.UpdateInput{PriceMarkupThreshold: &threshold}); err != nil {
							t.Fatal(err)
						}
						if err := db.First(&skus[0], skus[0].ID).Error; err != nil {
							t.Fatal(err)
						}
						if skus[0].PriceAmount.String() != "14.00" {
							t.Fatalf("threshold change did not reapply stored source cost: %+v", skus[0])
						}
						if err := db.First(&product, product.ID).Error; err != nil {
							t.Fatal(err)
						}
						if product.WholesalePrices[0].UnitPrice.String() != "11.00" {
							t.Fatal("save unexpectedly refreshed wholesale")
						}
						if scheduled {
							err = svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
						} else {
							err = svc.SyncProduct(mapping.ID)
						}
						if err != nil {
							t.Fatal(err)
						}
						if err := db.First(&product, product.ID).Error; err != nil {
							t.Fatal(err)
						}
						if len(product.WholesalePrices) != 1 || product.WholesalePrices[0].UnitPrice.String() != "11.20" {
							t.Fatalf("sync did not refresh wholesale with new threshold: %+v", product.WholesalePrices)
						}
					}

					if mode == constants.ConnectionPriceMarkupFixedAmount {
						// Exercise real save -> local reapply wiring, without any upstream HTTP request.
						connSvc := siteconnectionapp.NewService(siteconnectiongormstore.New(db), "test-secret-key", t.TempDir())
						connSvc.SetMarkupReapplier(svc)
						zero := money.FromDecimal(decimal.Zero)
						if _, err := connSvc.Update(mapping.ConnectionID, siteconnectionapp.UpdateInput{PriceMarkupAmount: &zero}); err != nil {
							t.Fatal(err)
						}
						if err := db.First(&skus[0], skus[0].ID).Error; err != nil {
							t.Fatal(err)
						}
						if skus[0].PriceAmount.String() != "10.00" {
							t.Fatalf("zero reset not applied: %+v", skus[0])
						}
						if err := db.First(&product, product.ID).Error; err != nil {
							t.Fatal(err)
						}
						if product.WholesalePrices[0].UnitPrice.String() != wholesale {
							t.Fatal("save unexpectedly refreshed wholesale")
						}
					}
				})
			}
		}
	}
}

func TestFixedAmountImportPricesAndRollback(t *testing.T) {
	for _, tt := range []struct {
		name, base, sku, wantPrice, wantCost string
		fail                                 bool
	}{
		{"zero product uses SKU price", "0", "5", "13.00", "10.00", false},
		{"missing product uses SKU price", "", "5", "13.00", "10.00", false},
		{"valid zero SKU", "0", "0", "3.00", "0.00", false},
		{"missing product and valid zero SKU", "", "0", "3.00", "0.00", false},
		{"invalid SKU rolls back", "5", "invalid", "", "", true},
		{"invalid base and SKU", "invalid", "invalid", "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := upstream.UpstreamProduct{ID: 102, Title: jsonmap.JSON{"zh-CN": "import"}, PriceAmount: tt.base, IsActive: true, SKUs: []upstream.UpstreamSKU{{ID: 301, SKUCode: "IMPORT", PriceAmount: tt.sku, IsActive: true}}}
			svc, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "import.db"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": source})
			})
			defer cleanup()
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			defer pool.Close()
			if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{"price_markup_mode": "fixed_amount", "price_markup_amount": "3.00", "exchange_rate": 2}).Error; err != nil {
				t.Fatal(err)
			}
			imported, err := svc.ImportUpstreamProduct(mapping.ConnectionID, 102, 1, "")
			if tt.fail {
				if err == nil || !strings.Contains(err.Error(), "parse upstream") {
					t.Fatalf("invalid source price must fail parsing: %v", err)
				}
				// The setup has one product/SKU/mapping. Failed imports must add none.
				for _, model := range []any{&productdomain.Product{}, &productdomain.ProductSKU{}, &mappingdomain.Mapping{}, &mappingdomain.SKUMapping{}} {
					var count int64
					if err := db.Model(model).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					if count != 1 {
						t.Fatalf("partial import left %T rows: %d", model, count)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var product productdomain.Product
			if err := db.First(&product, imported.LocalProductID).Error; err != nil {
				t.Fatal(err)
			}
			var sku productdomain.ProductSKU
			if err := db.Where("product_id = ?", product.ID).First(&sku).Error; err != nil {
				t.Fatal(err)
			}
			if product.PriceAmount.String() != tt.wantPrice || sku.PriceAmount.String() != tt.wantPrice || product.CostPriceAmount.String() != tt.wantCost || sku.CostPriceAmount.String() != tt.wantCost {
				t.Fatalf("product/SKU price or cost: %+v / %+v", product, sku)
			}
			if product.IsActive {
				t.Fatal("import must retain existing unpublished default")
			}
		})
	}
}

func TestFixedMarkupSyncDoesNotTreatInvalidSourcePriceAsZero(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprintf("scheduled=%v", scheduled), func(t *testing.T) {
			source := upstream.UpstreamProduct{ID: 101, PriceAmount: "5.00", IsActive: true, SKUs: []upstream.UpstreamSKU{
				{ID: 201, SKUCode: "SKU-A", PriceAmount: "invalid", IsActive: true, StockQuantity: 9},
				{ID: 202, SKUCode: "BAD", PriceAmount: "invalid", IsActive: true},
				{ID: 203, SKUCode: "ZERO", PriceAmount: "0.00", IsActive: true},
			}}
			svc, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "invalid.db"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if scheduled {
					listProductsHandler([]upstream.UpstreamProduct{source}, true)(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": source})
			})
			defer cleanup()
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			defer pool.Close()
			if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{"price_markup_mode": "fixed_amount", "price_markup_amount": "3.00", "auto_sync_price": true}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&mappingdomain.SKUMapping{}).Where("product_mapping_id = ?", mapping.ID).Update("upstream_price", "5.00").Error; err != nil {
				t.Fatal(err)
			}
			if scheduled {
				err = svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
			} else {
				err = svc.SyncProduct(mapping.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			var skus []productdomain.ProductSKU
			if err := db.Where("product_id = ?", mapping.LocalProductID).Order("id").Find(&skus).Error; err != nil {
				t.Fatal(err)
			}
			if len(skus) != 2 || skus[0].PriceAmount.String() != "10.00" || skus[1].SKUCode != "ZERO" || skus[1].PriceAmount.String() != "3.00" {
				t.Fatalf("invalid/zero prices handled incorrectly: %+v", skus)
			}
			var snapshot mappingdomain.SKUMapping
			if err := db.Where("upstream_sku_id = ?", 201).First(&snapshot).Error; err != nil {
				t.Fatal(err)
			}
			if snapshot.UpstreamPrice.String() != "5.00" || snapshot.UpstreamStock != 9 {
				t.Fatalf("invalid price must retain previous price but refresh stock: %+v", snapshot)
			}
		})
	}
}

func TestSegmentedImportAndSyncWholesalePricing(t *testing.T) {
	source := upstream.UpstreamProduct{
		ID: 102, Title: jsonmap.JSON{"zh-CN": "segmented import"}, PriceAmount: "8.00", IsActive: true,
		SKUs: []upstream.UpstreamSKU{
			{ID: 301, SKUCode: "SEGMENT-A", PriceAmount: "8.00", IsActive: true, StockQuantity: 100},
			{ID: 302, SKUCode: "SEGMENT-B", PriceAmount: "7.50", IsActive: true, StockQuantity: 100},
		},
		WholesalePrices: productdomain.WholesalePriceTiers{
			{SKUID: 301, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.RequireFromString("7.55"))},
			{SKUID: 301, MinQuantity: 10, UnitPrice: money.FromDecimal(decimal.RequireFromString("7.50"))},
			{SKUID: 301, MinQuantity: 20, UnitPrice: money.FromDecimal(decimal.RequireFromString("7.00"))},
		},
	}
	svc, db, originalMapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "segmented.db"), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": source})
	})
	defer cleanup()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	connSvc := siteconnectionapp.NewService(siteconnectiongormstore.New(db), "test-secret-key", t.TempDir())
	mode := constants.ConnectionPriceMarkupSegmented
	threshold := money.FromDecimal(decimal.NewFromInt(15))
	amount := money.FromDecimal(decimal.NewFromInt(5))
	percent, exchangeRate := 30.0, 2.0
	if _, err := connSvc.Update(originalMapping.ConnectionID, siteconnectionapp.UpdateInput{
		PriceMarkupMode: &mode, PriceMarkupThreshold: &threshold, PriceMarkupAmount: &amount,
		PriceMarkupPercent: &percent, ExchangeRate: &exchangeRate,
	}); err != nil {
		t.Fatal(err)
	}
	mapping, err := svc.ImportUpstreamProduct(originalMapping.ConnectionID, source.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	var product productdomain.Product
	if err := db.First(&product, mapping.LocalProductID).Error; err != nil {
		t.Fatal(err)
	}
	var skus []productdomain.ProductSKU
	if err := db.Where("product_id = ?", product.ID).Order("id").Find(&skus).Error; err != nil {
		t.Fatal(err)
	}
	if len(skus) != 2 || product.PriceAmount.String() != "20.00" || product.CostPriceAmount.String() != "15.00" || skus[0].PriceAmount.String() != "20.80" || skus[1].PriceAmount.String() != "20.00" || skus[0].CostPriceAmount.String() != "16.00" || skus[1].CostPriceAmount.String() != "15.00" {
		t.Fatalf("import did not price each SKU by its converted cost: %+v / %+v", product, skus)
	}
	if len(product.WholesalePrices) != 2 || product.WholesalePrices[0].MinQuantity != 5 || product.WholesalePrices[0].UnitPrice.String() != "19.63" || product.WholesalePrices[1].MinQuantity != 20 || product.WholesalePrices[1].UnitPrice.String() != "19.00" {
		t.Fatalf("unexpected imported wholesale tiers: %+v", product.WholesalePrices)
	}
	for _, tt := range []struct {
		quantity int
		want     string
	}{{1, "20.80"}, {5, "19.63"}, {10, "19.63"}, {20, "19.00"}} {
		unitPrice, _, _ := productdomain.ResolveWholesaleUnitPriceForSKU(&product, skus[0].PriceAmount.Decimal, skus[0].ID, skus[0].SKUCode, tt.quantity, tt.quantity)
		if unitPrice.StringFixed(2) != tt.want {
			t.Fatalf("quantity %d: got %s, want %s", tt.quantity, unitPrice, tt.want)
		}
	}
	// AutoSyncPrice is false: a repeated sync refreshes the same wholesale prices without touching manually maintained ordinary prices.
	for range 2 {
		if err := svc.SyncProduct(mapping.ID); err != nil {
			t.Fatal(err)
		}
		if err := db.First(&product, product.ID).Error; err != nil {
			t.Fatal(err)
		}
		if product.PriceAmount.String() != "20.00" || product.CostPriceAmount.String() != "15.00" || len(product.WholesalePrices) != 2 || product.WholesalePrices[0].UnitPrice.String() != "19.63" || product.WholesalePrices[1].UnitPrice.String() != "19.00" {
			t.Fatalf("sync changed or compounded segmented prices: %+v", product)
		}
	}
	// Missing upstream tiers preserve the local set, as before.
	source.WholesalePrices = nil
	if err := svc.SyncProduct(mapping.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&product, product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(product.WholesalePrices) != 2 || product.WholesalePrices[0].UnitPrice.String() != "19.63" {
		t.Fatalf("missing upstream tiers cleared local prices: %+v", product.WholesalePrices)
	}
	// Invalid source tiers must also preserve the previous local configuration.
	source.WholesalePrices = productdomain.WholesalePriceTiers{
		{SKUID: 301, MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.RequireFromString("7.50"))},
		{SKUID: 301, MinQuantity: 10, UnitPrice: money.FromDecimal(decimal.RequireFromString("7.55"))},
	}
	if err := svc.SyncProduct(mapping.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&product, product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(product.WholesalePrices) != 2 || product.WholesalePrices[0].UnitPrice.String() != "19.63" {
		t.Fatalf("invalid upstream tiers replaced local prices: %+v", product.WholesalePrices)
	}
}

func TestSegmentedImportSummarizesActiveSKUsAcrossThreshold(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprintf("scheduled=%v", scheduled), func(t *testing.T) {
			source := upstream.UpstreamProduct{
				ID: 102, Title: jsonmap.JSON{"zh-CN": "segmented threshold reversal"}, PriceAmount: "15.00", IsActive: true,
				SKUs: []upstream.UpstreamSKU{
					{ID: 301, SKUCode: "DISABLED", PriceAmount: "1.00", IsActive: false},
					{ID: 302, SKUCode: "AT-THRESHOLD", PriceAmount: "15.00", IsActive: true},
					{ID: 303, SKUCode: "ABOVE-THRESHOLD", PriceAmount: "15.01", IsActive: true},
				},
			}
			svc, db, originalMapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "threshold.db"), func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/upstream/products" {
					listProductsHandler([]upstream.UpstreamProduct{source}, true)(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": source})
			})
			defer cleanup()
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			defer pool.Close()
			connSvc := siteconnectionapp.NewService(siteconnectiongormstore.New(db), "test-secret-key", t.TempDir())
			mode := constants.ConnectionPriceMarkupSegmented
			threshold := money.FromDecimal(decimal.NewFromInt(15))
			amount := money.FromDecimal(decimal.NewFromInt(5))
			percent, exchangeRate := 30.0, 1.0
			if _, err := connSvc.Update(originalMapping.ConnectionID, siteconnectionapp.UpdateInput{
				PriceMarkupMode: &mode, PriceMarkupThreshold: &threshold, PriceMarkupAmount: &amount,
				PriceMarkupPercent: &percent, ExchangeRate: &exchangeRate,
			}); err != nil {
				t.Fatal(err)
			}
			mapping, err := svc.ImportUpstreamProduct(originalMapping.ConnectionID, source.ID, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			assertSummary := func() {
				t.Helper()
				var product productdomain.Product
				if err := db.First(&product, mapping.LocalProductID).Error; err != nil {
					t.Fatal(err)
				}
				// Minimum sale price and minimum cost come from different active SKUs.
				if product.PriceAmount.String() != "19.51" || product.CostPriceAmount.String() != "15.00" {
					t.Fatalf("summary must independently use minimum active SKU price/cost: %+v", product)
				}
			}
			var skus []productdomain.ProductSKU
			if err := db.Where("product_id = ?", mapping.LocalProductID).Order("id").Find(&skus).Error; err != nil {
				t.Fatal(err)
			}
			if len(skus) != len(source.SKUs) {
				t.Fatalf("got %d SKUs, want %d", len(skus), len(source.SKUs))
			}
			for i, want := range []struct{ price, cost string }{{"6.00", "1.00"}, {"20.00", "15.00"}, {"19.51", "15.01"}} {
				if skus[i].SKUCode != source.SKUs[i].SKUCode || skus[i].IsActive != source.SKUs[i].IsActive || skus[i].PriceAmount.String() != want.price || skus[i].CostPriceAmount.String() != want.cost {
					t.Fatalf("SKU %s price, cost or active status changed: %+v", source.SKUs[i].SKUCode, skus[i])
				}
			}
			var initialSnapshot mappingdomain.SKUMapping
			if err := db.Where("product_mapping_id = ? AND local_sku_id = ?", mapping.ID, skus[0].ID).First(&initialSnapshot).Error; err != nil {
				t.Fatal(err)
			}
			if initialSnapshot.UpstreamSKUID != source.SKUs[0].ID || initialSnapshot.UpstreamIsActive {
				t.Fatalf("imported disabled SKU must retain its inactive source snapshot: %+v", initialSnapshot)
			}
			assertSummary()
			// Unchanged source costs and rules must leave the imported summary stable.
			for range 2 {
				if _, err := svc.ReapplyMarkup(mapping.ConnectionID); err != nil {
					t.Fatal(err)
				}
				assertSummary()
			}
			// New upstream SKUs must preserve their disabled state on both sync paths.
			autoPrice := true
			if _, err := connSvc.Update(mapping.ConnectionID, siteconnectionapp.UpdateInput{AutoSyncPrice: &autoPrice}); err != nil {
				t.Fatal(err)
			}
			source.SKUs = append(source.SKUs, upstream.UpstreamSKU{
				ID: 304, SKUCode: "NEW-DISABLED", PriceAmount: "0.50", IsActive: false, StockQuantity: 100,
			})
			for range 2 {
				if scheduled {
					err = svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
				} else {
					err = svc.SyncProduct(mapping.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				var newSKU productdomain.ProductSKU
				if err := db.Where("product_id = ? AND sku_code = ?", mapping.LocalProductID, "NEW-DISABLED").First(&newSKU).Error; err != nil {
					t.Fatal(err)
				}
				if newSKU.IsActive || newSKU.PriceAmount.String() != "5.50" || newSKU.CostPriceAmount.String() != "0.50" {
					t.Fatalf("new disabled SKU must retain its status and converted prices: %+v", newSKU)
				}
				assertSummary()
				var snapshot mappingdomain.SKUMapping
				if err := db.Where("product_mapping_id = ? AND local_sku_id = ?", mapping.ID, newSKU.ID).First(&snapshot).Error; err != nil {
					t.Fatal(err)
				}
				if snapshot.UpstreamSKUID != 304 || snapshot.UpstreamIsActive || snapshot.UpstreamPrice.String() != "0.50" {
					t.Fatalf("new disabled SKU source snapshot changed: %+v", snapshot)
				}
				var count int64
				if err := db.Model(&productdomain.ProductSKU{}).Where("product_id = ?", mapping.LocalProductID).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != int64(len(source.SKUs)) {
					t.Fatalf("repeated sync created duplicate or missing SKUs: got %d, want %d", count, len(source.SKUs))
				}
			}
		})
	}
}

func TestSegmentedImportPreservesProductFallback(t *testing.T) {
	for _, tt := range []struct {
		name          string
		skus          []upstream.UpstreamSKU
		wantSKUPrice  string
		wantSKUCost   string
		wantSKUActive bool
	}{
		{
			name:         "no active upstream SKU",
			skus:         []upstream.UpstreamSKU{{ID: 301, SKUCode: "DISABLED", PriceAmount: "1.00", IsActive: false}},
			wantSKUPrice: "6.00", wantSKUCost: "1.00", wantSKUActive: false,
		},
		{
			name:         "no upstream SKU keeps product cost despite zero-cost default SKU",
			wantSKUPrice: "20.00", wantSKUCost: "0.00", wantSKUActive: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := upstream.UpstreamProduct{
				ID: 102, Title: jsonmap.JSON{"zh-CN": "segmented fallback"}, PriceAmount: "15.00", IsActive: true, SKUs: tt.skus,
			}
			svc, db, originalMapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "fallback.db"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": source})
			})
			defer cleanup()
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			defer pool.Close()
			connSvc := siteconnectionapp.NewService(siteconnectiongormstore.New(db), "test-secret-key", t.TempDir())
			mode := constants.ConnectionPriceMarkupSegmented
			threshold := money.FromDecimal(decimal.NewFromInt(15))
			amount := money.FromDecimal(decimal.NewFromInt(5))
			percent, exchangeRate := 30.0, 1.0
			if _, err := connSvc.Update(originalMapping.ConnectionID, siteconnectionapp.UpdateInput{
				PriceMarkupMode: &mode, PriceMarkupThreshold: &threshold, PriceMarkupAmount: &amount,
				PriceMarkupPercent: &percent, ExchangeRate: &exchangeRate,
			}); err != nil {
				t.Fatal(err)
			}
			mapping, err := svc.ImportUpstreamProduct(originalMapping.ConnectionID, source.ID, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			var product productdomain.Product
			if err := db.First(&product, mapping.LocalProductID).Error; err != nil {
				t.Fatal(err)
			}
			if product.PriceAmount.String() != "20.00" || product.CostPriceAmount.String() != "15.00" {
				t.Fatalf("import must preserve product-level fallback price/cost: %+v", product)
			}
			var skus []productdomain.ProductSKU
			if err := db.Where("product_id = ?", product.ID).Find(&skus).Error; err != nil {
				t.Fatal(err)
			}
			if len(skus) != 1 || skus[0].PriceAmount.String() != tt.wantSKUPrice || skus[0].CostPriceAmount.String() != tt.wantSKUCost || skus[0].IsActive != tt.wantSKUActive {
				t.Fatalf("import changed fallback SKU behavior: %+v", skus)
			}
		})
	}
}
