package catalogmappingbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	mappingapp "github.com/dujiao-next/internal/modules/catalog/mapping/application"
	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
	siteconnectioncontract "github.com/dujiao-next/internal/modules/siteconnection/contract"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	siteconnectiongormstore "github.com/dujiao-next/internal/modules/siteconnection/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
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
					var categoryRequests atomic.Int32
					handler := func(w http.ResponseWriter, r *http.Request) {
						if strings.HasSuffix(r.URL.Path, "/categories") {
							categoryRequests.Add(1)
							http.Error(w, "categories unavailable", http.StatusServiceUnavailable)
							return
						}
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
					// 固定金额即使保留分类比例，也不能请求分类树或叠加比例。
					categoryMarkups := jsonmap.JSON(nil)
					if mode == constants.ConnectionPriceMarkupFixedAmount {
						categoryMarkups = jsonmap.JSON{"9": 50}
					}
					if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{
						"category_markups":  categoryMarkups,
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
					if categoryRequests.Load() != 0 {
						t.Fatalf("unexpected category API requests: %d", categoryRequests.Load())
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
				if strings.HasSuffix(r.URL.Path, "/categories") {
					t.Error("fixed amount import requested categories")
					http.Error(w, "unavailable", 503)
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
			if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{"price_markup_mode": "fixed_amount", "price_markup_amount": "3.00", "exchange_rate": 2, "category_markups": jsonmap.JSON{"9": 50}}).Error; err != nil {
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

// 旧分段连接仍同步库存和上下架；明确转换以前不允许改价、导入或创建新 SKU。
func TestLegacyMarkupSyncPreservesPrices(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprintf("scheduled=%v", scheduled), func(t *testing.T) {
			source := upstream.UpstreamProduct{ID: 101, PriceAmount: "5.00", IsActive: true, SKUs: []upstream.UpstreamSKU{
				{ID: 201, SKUCode: "SKU-A", PriceAmount: "5.00", IsActive: false, StockQuantity: 9},
				{ID: 202, SKUCode: "NEW", PriceAmount: "8.00", IsActive: true, StockQuantity: 20},
			}}
			svc, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "legacy.db"), func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/categories") {
					t.Error("legacy mode requested categories")
					http.Error(w, "unavailable", 503)
					return
				}
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
			defer pool.Close()
			if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{"price_markup_mode": "segmented", "auto_sync_price": true, "price_markup_percent": 40, "price_markup_amount": "3.00"}).Error; err != nil {
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
			if err := db.Where("product_id = ?", mapping.LocalProductID).Find(&skus).Error; err != nil {
				t.Fatal(err)
			}
			if len(skus) != 1 || skus[0].PriceAmount.String() != "10.00" || skus[0].IsActive {
				t.Fatalf("legacy sync changed price or failed to sync status: %+v", skus)
			}
			var snapshot mappingdomain.SKUMapping
			if err := db.Where("product_mapping_id = ?", mapping.ID).First(&snapshot).Error; err != nil {
				t.Fatal(err)
			}
			if snapshot.UpstreamStock != 0 || snapshot.UpstreamIsActive {
				t.Fatalf("inactive SKU must clear sale stock while preserving prices: %+v", snapshot)
			}
			if _, err := svc.ReapplyMarkup(mapping.ConnectionID); !errors.Is(err, siteconnectioncontract.ErrInvalid) {
				t.Fatalf("legacy reapply: %v", err)
			}
			if _, err := svc.ReapplyMarkupLocal(mapping.ConnectionID); !errors.Is(err, siteconnectioncontract.ErrInvalid) {
				t.Fatalf("legacy local reapply: %v", err)
			}
			if _, err := svc.ImportUpstreamProduct(mapping.ConnectionID, 102, 1, ""); !errors.Is(err, siteconnectioncontract.ErrInvalid) {
				t.Fatalf("legacy import: %v", err)
			}
		})
	}
}

// 两个批量入口都必须沿用单条导入的模式边界，而非仅按闲置分类比例决定预取。
func TestBatchImportMarkupModeIsolation(t *testing.T) {
	for _, byCategory := range []bool{false, true} {
		for _, mode := range []string{"fixed_amount", "segmented"} {
			t.Run(fmt.Sprintf("category=%v/mode=%s", byCategory, mode), func(t *testing.T) {
				source := upstream.UpstreamProduct{ID: 102, CategoryID: 9, Title: jsonmap.JSON{"zh-CN": "import"}, PriceAmount: "5.00", IsActive: true, SKUs: []upstream.UpstreamSKU{{ID: 301, SKUCode: "BATCH", PriceAmount: "5.00", IsActive: true}}}
				var categoryRequests atomic.Int32
				svc, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "batch.db"), func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/categories") {
						categoryRequests.Add(1)
						http.Error(w, "unavailable", 503)
						return
					}
					if strings.HasSuffix(r.URL.Path, "/products") {
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
				defer pool.Close()
				if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Updates(map[string]any{"price_markup_mode": mode, "price_markup_amount": "3.00", "exchange_rate": 2, "category_markups": jsonmap.JSON{"9": 50}}).Error; err != nil {
					t.Fatal(err)
				}
				if byCategory {
					result, importErr := svc.BatchImportByCategory(mapping.ConnectionID, 9, false, 1)
					err = importErr
					if mode == "fixed_amount" && (result == nil || result.SuccessCount != 1) {
						t.Fatalf("batch category: %+v, %v", result, err)
					}
				} else {
					result, importErr := svc.BatchImportUpstreamProducts(mapping.ConnectionID, []uint{102}, 1, false)
					err = importErr
					if mode == "fixed_amount" && (len(result) != 1 || result[0].Err != nil) {
						t.Fatalf("batch IDs: %+v, %v", result, err)
					}
				}
				if mode == "segmented" {
					if !errors.Is(err, siteconnectioncontract.ErrInvalid) {
						t.Fatalf("legacy batch: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					var product productdomain.Product
					if err := db.Where("id <> ?", mapping.LocalProductID).First(&product).Error; err != nil {
						t.Fatal(err)
					}
					if product.PriceAmount.String() != "13.00" || product.CostPriceAmount.String() != "10.00" {
						t.Fatalf("wrong batch price: %+v", product)
					}
				}
				if categoryRequests.Load() != 0 {
					t.Fatalf("unexpected category requests: %d", categoryRequests.Load())
				}
			})
		}
	}
}

// setupAvailabilityTest 复用完整装配，以单写连接验证停售事务没有访问全局连接。
func setupAvailabilityTest(t *testing.T, handler http.HandlerFunc) (*mappingapp.Service, *gorm.DB, *mappingdomain.Mapping, uint) {
	t.Helper()
	svc, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, filepath.Join(t.TempDir(), "availability.db"), handler)
	t.Cleanup(cleanup)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	var sm mappingdomain.SKUMapping
	if err := db.Where("product_mapping_id = ?", mapping.ID).First(&sm).Error; err != nil {
		t.Fatal(err)
	}
	return svc, db, mapping, sm.LocalSKUID
}

func availabilityProduct(stock int) upstream.UpstreamProduct {
	return upstream.UpstreamProduct{
		ID: 101, IsActive: true, PriceAmount: "10.00",
		SKUs: []upstream.UpstreamSKU{{ID: 201, IsActive: true, PriceAmount: "10.00", StockQuantity: stock}},
	}
}

func writeAvailabilityProduct(w http.ResponseWriter, product upstream.UpstreamProduct) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": product})
}

type availabilityState struct {
	mapping mappingdomain.Mapping
	product productdomain.Product
	sku     productdomain.ProductSKU
	stock   mappingdomain.SKUMapping
}

func loadAvailabilityState(t *testing.T, db *gorm.DB, mappingID, skuID uint) availabilityState {
	t.Helper()
	var got availabilityState
	for _, query := range []struct {
		model any
		id    uint
	}{{&got.mapping, mappingID}, {&got.sku, skuID}} {
		if err := db.First(query.model, query.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&got.product, got.mapping.LocalProductID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("local_sku_id = ?", skuID).First(&got.stock).Error; err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCheckUpstreamPurchasableAlwaysChecksFreshAvailability(t *testing.T) {
	for _, test := range []struct {
		name   string
		cached int
	}{{"cached stock sufficient", 100}, {"cached unlimited", -1}} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.NotFound(w, r)
			})
			if err := db.Model(&mappingdomain.SKUMapping{}).Where("local_sku_id = ?", skuID).Update("upstream_stock", test.cached).Error; err != nil {
				t.Fatal(err)
			}
			err := svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: 1}})
			if !errors.Is(err, mappingcontract.ErrUpstreamProductNotFound) || calls.Load() != 1 {
				t.Fatalf("check error = %v, detail calls = %d", err, calls.Load())
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if got.product.IsActive || got.sku.IsActive || got.stock.UpstreamIsActive || got.stock.UpstreamStock != 0 || got.mapping.UpstreamStatus != mappingdomain.UpstreamStatusInactive || !got.mapping.IsActive {
				t.Fatalf("HTML 404 did not stop sale while preserving mapping: %+v", got)
			}
		})
	}
}

type availabilitySettingsProvider struct {
	enabled bool
	err     error
}

func (p availabilitySettingsProvider) GetUpstreamSyncConfig(string) (settingsintegration.UpstreamSyncConfig, error) {
	cfg := settingsintegration.DefaultUpstreamSyncConfig()
	cfg.PreOrderStockCheckEnabled = p.enabled
	return cfg, p.err
}

func (p availabilitySettingsProvider) GetUpstreamSyncInterval(string) (time.Duration, error) {
	return 5 * time.Minute, p.err
}

func TestCheckUpstreamPurchasableDisabledRemoteStillChecksCachedSaleState(t *testing.T) {
	for _, test := range []struct {
		name     string
		stock    int
		stop     string
		quantity int
		want     error
	}{
		{"sufficient stock", 10, "", 1, nil},
		{"unlimited stock", -1, "", 100, nil},
		{"stopped product", 100, "product", 1, mappingcontract.ErrUpstreamProductNotFound},
		{"stopped sku", 100, "sku", 1, mappingcontract.ErrUpstreamProductNotFound},
		{"stopped upstream sku", 100, "upstream_sku", 1, mappingcontract.ErrUpstreamProductNotFound},
		{"disabled mapping", 100, "mapping", 1, mappingcontract.ErrMappingInactive},
		{"aggregate stock insufficient", 3, "", 2, mappingcontract.ErrUpstreamStockInsufficient},
		{"invalid cached stock", -2, "", 1, mappingcontract.ErrUpstreamCheckFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.NotFound(w, r)
			})
			svc.SetSettings(availabilitySettingsProvider{enabled: false})
			if err := db.Model(&mappingdomain.SKUMapping{}).Where("local_sku_id = ?", skuID).Update("upstream_stock", test.stock).Error; err != nil {
				t.Fatal(err)
			}
			var query *gorm.DB
			switch test.stop {
			case "product":
				query = db.Model(&productdomain.Product{}).Where("id = ?", mapping.LocalProductID).Update("is_active", false)
			case "sku":
				query = db.Model(&productdomain.ProductSKU{}).Where("id = ?", skuID).Update("is_active", false)
			case "upstream_sku":
				query = db.Model(&mappingdomain.SKUMapping{}).Where("local_sku_id = ?", skuID).Update("upstream_is_active", false)
			case "mapping":
				query = db.Model(&mappingdomain.Mapping{}).Where("id = ?", mapping.ID).Update("is_active", false)
			}
			if query != nil && query.Error != nil {
				t.Fatal(query.Error)
			}
			items := []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: test.quantity}}
			if test.name == "aggregate stock insufficient" {
				items = append(items, items[0])
			}
			err := svc.CheckUpstreamPurchasable(context.Background(), items)
			if !errors.Is(err, test.want) || calls.Load() != 0 {
				t.Fatalf("disabled remote check error = %v, want %v; HTTP calls = %d", err, test.want, calls.Load())
			}
		})
	}
}

func TestCheckUpstreamPurchasableSettingsFailureDefaultsToRemoteCheck(t *testing.T) {
	var calls atomic.Int32
	svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	})
	// 即使失败结果同时携带 false，也不能把读取失败误当成运营关闭核验。
	svc.SetSettings(availabilitySettingsProvider{enabled: false, err: errors.New("settings unavailable")})
	err := svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: 1}})
	if !errors.Is(err, mappingcontract.ErrUpstreamProductNotFound) || calls.Load() != 1 {
		t.Fatalf("settings failure error = %v, HTTP calls = %d", err, calls.Load())
	}
	got := loadAvailabilityState(t, db, mapping.ID, skuID)
	if got.product.IsActive || got.sku.IsActive || got.stock.UpstreamStock != 0 || got.mapping.UpstreamStatus != mappingdomain.UpstreamStatusInactive {
		t.Fatalf("404 must stop sale after settings failure: %+v", got)
	}
}

func TestReportedOutOfStockRecoversAfterSyncWithoutManualActivation(t *testing.T) {
	svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAvailabilityProduct(w, availabilityProduct(8))
	})
	if err := svc.ReportUpstreamUnavailable(context.Background(), mapping.LocalProductID, skuID, "product_out_of_stock"); err != nil {
		t.Fatal(err)
	}
	empty := loadAvailabilityState(t, db, mapping.ID, skuID)
	if !empty.product.IsActive || !empty.sku.IsActive || !empty.stock.UpstreamIsActive || empty.stock.UpstreamStock != 0 {
		t.Fatalf("out of stock must retain sale switches: %+v", empty)
	}
	if err := svc.SyncProduct(mapping.ID); err != nil {
		t.Fatal(err)
	}
	got := loadAvailabilityState(t, db, mapping.ID, skuID)
	if !got.product.IsActive || !got.sku.IsActive || !got.stock.UpstreamIsActive || got.stock.UpstreamStock != 8 {
		t.Fatalf("restocked SKU did not recover naturally: %+v", got)
	}
	if err := svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: 8}}); err != nil {
		t.Fatalf("restocked SKU must be purchasable: %v", err)
	}
}

func TestCheckUpstreamPurchasableRefreshesSKUState(t *testing.T) {
	for _, test := range []struct {
		name          string
		product       upstream.UpstreamProduct
		quantity      int
		want          error
		wantSKUActive bool
		wantStock     int
	}{
		{"unlimited stock", availabilityProduct(-1), 1000, nil, true, -1},
		{"insufficient stock", availabilityProduct(2), 3, mappingcontract.ErrUpstreamStockInsufficient, true, 2},
		{"zero stock", availabilityProduct(0), 1, mappingcontract.ErrUpstreamStockInsufficient, true, 0},
		{"missing sku", upstream.UpstreamProduct{ID: 101, IsActive: true, SKUs: []upstream.UpstreamSKU{}}, 1, mappingcontract.ErrUpstreamProductNotFound, false, 0},
		{"inactive sku", upstream.UpstreamProduct{ID: 101, IsActive: true, SKUs: []upstream.UpstreamSKU{{ID: 201, IsActive: false, StockQuantity: 20}}}, 1, mappingcontract.ErrUpstreamProductNotFound, false, 0},
		{"inactive product", upstream.UpstreamProduct{ID: 101, IsActive: false}, 1, mappingcontract.ErrUpstreamProductNotFound, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, _ *http.Request) { writeAvailabilityProduct(w, test.product) })
			err := svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: test.quantity}})
			if !errors.Is(err, test.want) {
				t.Fatalf("check error = %v, want %v", err, test.want)
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if got.sku.IsActive != test.wantSKUActive || got.stock.UpstreamIsActive != test.wantSKUActive || got.stock.UpstreamStock != test.wantStock {
				t.Fatalf("unexpected refreshed sku state: %+v", got)
			}
			if got.product.IsActive != test.product.IsActive || got.mapping.LastSyncedAt == nil || got.stock.StockSyncedAt == nil {
				t.Fatalf("unexpected refreshed product state: %+v", got)
			}
		})
	}
}

func TestCheckUpstreamPurchasableFailsClosedWithoutStoppingSaleOnQueryFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"authentication", http.StatusUnauthorized, `{"ok":false,"error_code":"product_not_found"}`},
		{"authorization", http.StatusForbidden, `{"ok":false}`},
		{"malformed detail", http.StatusOK, `{"ok":true,"product":{"id":101,"is_active":true}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			err := svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: 1}})
			if !errors.Is(err, mappingcontract.ErrUpstreamCheckFailed) {
				t.Fatalf("check error = %v, want ErrUpstreamCheckFailed", err)
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if !got.product.IsActive || !got.sku.IsActive || !got.stock.UpstreamIsActive || got.stock.UpstreamStock != 100 || got.mapping.LastSyncedAt != nil {
				t.Fatalf("query failure changed local sale state: %+v", got)
			}
		})
	}
}

func TestCheckUpstreamPurchasableAggregatesSameUpstreamSKU(t *testing.T) {
	for _, duplicateLocalSKU := range []bool{false, true} {
		t.Run(map[bool]string{false: "same local sku", true: "different local skus"}[duplicateLocalSKU], func(t *testing.T) {
			var calls atomic.Int32
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writeAvailabilityProduct(w, availabilityProduct(3))
			})
			secondID := skuID
			if duplicateLocalSKU {
				sku := productdomain.ProductSKU{ProductID: mapping.LocalProductID, SKUCode: "SKU-B", IsActive: true}
				if err := db.Create(&sku).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&mappingdomain.SKUMapping{ProductMappingID: mapping.ID, LocalSKUID: sku.ID, UpstreamSKUID: 201, UpstreamIsActive: true, UpstreamStock: 100}).Error; err != nil {
					t.Fatal(err)
				}
				secondID = sku.ID
			}
			err := svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: 2}, {LocalSKUID: secondID, Quantity: 2}})
			if !errors.Is(err, mappingcontract.ErrUpstreamStockInsufficient) || calls.Load() != 1 {
				t.Fatalf("check error = %v, detail calls = %d, want insufficient with one detail request", err, calls.Load())
			}
		})
	}
}

func TestReportUpstreamUnavailableLimitsScopeAndPreservesMapping(t *testing.T) {
	for _, test := range []struct {
		code          string
		productActive bool
		skuActive     bool
		status        string
	}{
		{"product_not_found", false, false, mappingdomain.UpstreamStatusInactive},
		{"product_unavailable", false, false, mappingdomain.UpstreamStatusInactive},
		{"product_deleted", false, false, mappingdomain.UpstreamStatusDeleted},
		{"sku_not_found", true, false, mappingdomain.UpstreamStatusActive},
		{"sku_unavailable", true, false, mappingdomain.UpstreamStatusActive},
		{"sku_deleted", true, false, mappingdomain.UpstreamStatusActive},
		{"product_out_of_stock", true, true, mappingdomain.UpstreamStatusActive},
		{"sku_out_of_stock", true, true, mappingdomain.UpstreamStatusActive},
		{"insufficient_stock", true, true, mappingdomain.UpstreamStatusActive},
	} {
		t.Run(test.code, func(t *testing.T) {
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("report must not request upstream") })
			sibling := productdomain.ProductSKU{ProductID: mapping.LocalProductID, SKUCode: "SKU-B", IsActive: true}
			if err := db.Create(&sibling).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&mappingdomain.SKUMapping{ProductMappingID: mapping.ID, LocalSKUID: sibling.ID, UpstreamSKUID: 202, UpstreamIsActive: true, UpstreamStock: 30}).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.ReportUpstreamUnavailable(context.Background(), mapping.LocalProductID, skuID, test.code); err != nil {
				t.Fatal(err)
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if got.product.IsActive != test.productActive || got.sku.IsActive != test.skuActive || got.stock.UpstreamIsActive != test.skuActive || got.stock.UpstreamStock != 0 || got.mapping.UpstreamStatus != test.status || !got.mapping.IsActive {
				t.Fatalf("unexpected rejection state: %+v", got)
			}
			other := loadAvailabilityState(t, db, mapping.ID, sibling.ID)
			wantSiblingStock := 0
			if test.productActive {
				wantSiblingStock = 30
			}
			if other.sku.IsActive != test.productActive || other.stock.UpstreamIsActive != test.productActive || other.stock.UpstreamStock != wantSiblingStock {
				t.Fatalf("rejection scope changed sibling sku incorrectly: %+v", other)
			}
		})
	}
}

func TestReportUpstreamUnavailableRollsBackAllWrites(t *testing.T) {
	for _, code := range []string{"product_not_found", "sku_not_found", "sku_out_of_stock"} {
		t.Run(code, func(t *testing.T) {
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("report must not request upstream") })
			failure := errors.New("injected dependent availability write failure")
			failureTable := "product_skus"
			if code == "sku_out_of_stock" {
				failureTable = "sku_mappings"
			}
			if err := db.Callback().Update().Before("gorm:update").Register("test:reject_availability_commit", func(tx *gorm.DB) {
				if tx.Statement.Table == failureTable {
					tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			err := svc.ReportUpstreamUnavailable(context.Background(), mapping.LocalProductID, skuID, code)
			if !errors.Is(err, failure) {
				t.Fatalf("report error = %v, want injected failure", err)
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if !got.product.IsActive || !got.sku.IsActive || !got.stock.UpstreamIsActive || got.stock.UpstreamStock != 100 || got.mapping.LastSyncedAt != nil || got.stock.StockSyncedAt != nil {
				t.Fatalf("failed transaction left partial sale state: %+v", got)
			}
		})
	}
}

func TestAvailabilityOldResponseCannotOverwriteNewPurchaseRejection(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writeAvailabilityProduct(w, availabilityProduct(100))
	})
	result := make(chan error, 1)
	go func() {
		result <- svc.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: skuID, Quantity: 1}})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("purchase check did not reach upstream")
	}
	if err := svc.ReportUpstreamUnavailable(context.Background(), mapping.LocalProductID, skuID, "sku_not_found"); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, mappingcontract.ErrUpstreamCheckFailed) {
			t.Fatalf("old response error = %v, want ErrUpstreamCheckFailed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("purchase check did not finish")
	}
	got := loadAvailabilityState(t, db, mapping.ID, skuID)
	if got.sku.IsActive || got.stock.UpstreamIsActive || got.stock.UpstreamStock != 0 {
		t.Fatalf("old response overwrote newer rejection: %+v", got)
	}
}

func TestSyncRecoveryDoesNotReactivateManuallyStoppedProductOrSKU(t *testing.T) {
	svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, _ *http.Request) { writeAvailabilityProduct(w, availabilityProduct(10)) })
	if err := db.Model(&productdomain.Product{}).Where("id = ?", mapping.LocalProductID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&productdomain.ProductSKU{}).Where("id = ?", skuID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncProduct(mapping.ID); err != nil {
		t.Fatal(err)
	}
	got := loadAvailabilityState(t, db, mapping.ID, skuID)
	if got.product.IsActive || got.sku.IsActive || !got.stock.UpstreamIsActive || got.stock.UpstreamStock != 10 || got.mapping.UpstreamStatus != mappingdomain.UpstreamStatusActive {
		t.Fatalf("sync recovery must refresh upstream state and preserve manual sale switches: %+v", got)
	}
}

func TestSyncConnectionStockRechecksCompleteListMissingProduct(t *testing.T) {
	for _, test := range []struct {
		name             string
		includesInactive bool
		status           int
		code             string
		wantStatus       string
		wantActive       bool
		wantError        bool
	}{
		{"legacy HTML 404", false, http.StatusNotFound, "", mappingdomain.UpstreamStatusInactive, false, false},
		{"complete list HTML 404", true, http.StatusNotFound, "", mappingdomain.UpstreamStatusInactive, false, false},
		{"explicit deletion", true, http.StatusNotFound, "product_deleted", mappingdomain.UpstreamStatusDeleted, false, false},
		{"temporary list omission", true, http.StatusOK, "", mappingdomain.UpstreamStatusActive, true, false},
		{"detail authentication error", false, http.StatusUnauthorized, "", mappingdomain.UpstreamStatusActive, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var detailCalls atomic.Int32
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/upstream/products":
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": []upstream.UpstreamProduct{}, "total": 0, "page": 1, "page_size": 50, "includes_inactive": test.includesInactive})
				case "/api/v1/upstream/products/101":
					detailCalls.Add(1)
					if test.status == http.StatusOK {
						writeAvailabilityProduct(w, availabilityProduct(12))
					} else if test.code != "" {
						w.WriteHeader(test.status)
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": test.code})
					} else {
						w.WriteHeader(test.status)
						_, _ = w.Write([]byte("<html>upstream error</html>"))
					}
				default:
					http.NotFound(w, r)
				}
			})
			err := svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
			if (err != nil) != test.wantError || detailCalls.Load() != 1 {
				t.Fatalf("sync error = %v, detail calls = %d", err, detailCalls.Load())
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if got.mapping.UpstreamStatus != test.wantStatus || !got.mapping.IsActive || got.product.IsActive != test.wantActive || got.sku.IsActive != test.wantActive || got.stock.UpstreamIsActive != test.wantActive {
				t.Fatalf("unexpected complete-list detail result: %+v", got)
			}
			if test.wantActive && !test.wantError && got.stock.UpstreamStock != 12 {
				t.Fatalf("detail stock not refreshed: %d", got.stock.UpstreamStock)
			}
		})
	}
}

func TestSyncConnectionStockDoesNotInferMissingFromIncompletePagination(t *testing.T) {
	for _, repeatPage := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty next page", true: "repeated product"}[repeatPage], func(t *testing.T) {
			var detailCalls atomic.Int32
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/upstream/products" {
					detailCalls.Add(1)
					http.NotFound(w, r)
					return
				}
				items := []upstream.UpstreamProduct{}
				if r.URL.Query().Get("page") == "1" || repeatPage {
					other := availabilityProduct(1)
					other.ID = 999
					items = append(items, other)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": items, "total": 3, "includes_inactive": true})
			})
			if err := svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 3); err == nil {
				t.Fatal("incomplete pagination must report failure")
			}
			if detailCalls.Load() != 0 {
				t.Fatalf("incomplete list must not infer absence, detail calls = %d", detailCalls.Load())
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if !got.product.IsActive || !got.sku.IsActive || !got.stock.UpstreamIsActive || got.stock.UpstreamStock != 100 || got.mapping.LastSyncedAt != nil {
				t.Fatalf("incomplete list changed missing product state: %+v", got)
			}
		})
	}
}

func TestSyncConnectionStockSupportsLegacyPaginationEcho(t *testing.T) {
	for _, wrongPage := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy missing page echoes", true: "wrong explicit page"}[wrongPage], func(t *testing.T) {
			var detailCalls atomic.Int32
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/upstream/products" {
					body := map[string]any{"ok": true, "items": []upstream.UpstreamProduct{}, "total": 0}
					if wrongPage {
						body["page"] = 2
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(body)
					return
				}
				detailCalls.Add(1)
				http.NotFound(w, r)
			})
			err := svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if wrongPage {
				if err == nil || detailCalls.Load() != 0 || !got.product.IsActive || !got.sku.IsActive || got.stock.UpstreamStock != 100 || got.mapping.LastSyncedAt != nil {
					t.Fatalf("wrong explicit page must fail and preserve state: err=%v detail_calls=%d state=%+v", err, detailCalls.Load(), got)
				}
			} else {
				if err != nil || detailCalls.Load() != 1 || got.product.IsActive || got.sku.IsActive || got.stock.UpstreamIsActive || got.stock.UpstreamStock != 0 || got.mapping.UpstreamStatus != mappingdomain.UpstreamStatusInactive {
					t.Fatalf("legacy complete list must check detail and stop sale: err=%v detail_calls=%d state=%+v", err, detailCalls.Load(), got)
				}
			}
		})
	}
}

func TestSyncOldSnapshotCannotOverwriteNewPurchaseRejection(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual detail", true: "scheduled list"}[scheduled], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			svc, db, mapping, skuID := setupAvailabilityTest(t, func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				if scheduled {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": []upstream.UpstreamProduct{availabilityProduct(100)}, "total": 1})
				} else {
					writeAvailabilityProduct(w, availabilityProduct(100))
				}
			})
			result := make(chan error, 1)
			go func() {
				if scheduled {
					result <- svc.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200)
				} else {
					result <- svc.SyncProduct(mapping.ID)
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("sync did not reach upstream")
			}
			if err := svc.ReportUpstreamUnavailable(context.Background(), mapping.LocalProductID, skuID, "sku_not_found"); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			select {
			case <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("sync did not finish")
			}
			got := loadAvailabilityState(t, db, mapping.ID, skuID)
			if got.sku.IsActive || got.stock.UpstreamIsActive || got.stock.UpstreamStock != 0 {
				t.Fatalf("old sync snapshot overwrote newer rejection: %+v", got)
			}
		})
	}
}
