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
			for _, mode := range []string{constants.ConnectionPriceMarkupPercent, constants.ConnectionPriceMarkupFixedAmount} {
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
						"price_markup_mode": mode, "price_markup_amount": "3.00", "price_markup_percent": 40, "exchange_rate": 2, "auto_sync_price": autoPrice,
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
