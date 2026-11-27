package gormstore

import (
	"fmt"
	"testing"
	"time"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func setupSKUStoreTest(t *testing.T) (*SKUStore, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:product_sku_repository_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&productdomain.ProductSKU{}); err != nil {
		t.Fatalf("migrate product sku failed: %v", err)
	}
	return NewSKUStore(db), db
}

func TestSKUStoreListByProductSortOrderDescending(t *testing.T) {
	repo, _ := setupSKUStoreTest(t)

	high := &productdomain.ProductSKU{
		ProductID:      1,
		SKUCode:        "HIGH",
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(100)),
		IsActive:       true,
		SortOrder:      100,
		SpecValuesJSON: jsonmap.JSON{},
	}
	low := &productdomain.ProductSKU{
		ProductID:      1,
		SKUCode:        "LOW",
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(100)),
		IsActive:       true,
		SortOrder:      1,
		SpecValuesJSON: jsonmap.JSON{},
	}
	if err := repo.Create(high); err != nil {
		t.Fatalf("create high sort sku failed: %v", err)
	}
	if err := repo.Create(low); err != nil {
		t.Fatalf("create low sort sku failed: %v", err)
	}

	rows, err := repo.ListByProduct(1, true)
	if err != nil {
		t.Fatalf("list skus failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 skus, got %d", len(rows))
	}
	if rows[0].SKUCode != "HIGH" || rows[1].SKUCode != "LOW" {
		t.Fatalf("expected high sort_order first, got %s then %s", rows[0].SKUCode, rows[1].SKUCode)
	}
}

func TestProductSKUManualStockLifecycleMatchesProductSemantics(t *testing.T) {
	repo, db := setupSKUStoreTest(t)
	sku := &productdomain.ProductSKU{
		ProductID:        1,
		SKUCode:          "STOCK-LIFECYCLE",
		PriceAmount:      money.FromDecimal(decimal.NewFromInt(100)),
		ManualStockTotal: 10,
		IsActive:         true,
	}
	if err := repo.Create(sku); err != nil {
		t.Fatalf("create stock sku: %v", err)
	}

	assertAffected := func(operation string, affected int64, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s manual stock: %v", operation, err)
		}
		if affected != 1 {
			t.Fatalf("%s affected rows want 1 got %d", operation, affected)
		}
	}

	affected, err := repo.ReserveManualStock(sku.ID, 3)
	assertAffected("reserve", affected, err)
	affected, err = repo.ConsumeManualStock(sku.ID, 2)
	assertAffected("consume", affected, err)
	affected, err = repo.ReleaseManualStock(sku.ID, 1)
	assertAffected("release", affected, err)

	var reloaded productdomain.ProductSKU
	if err := db.First(&reloaded, sku.ID).Error; err != nil {
		t.Fatalf("reload stock sku: %v", err)
	}
	if reloaded.ManualStockTotal != 8 || reloaded.ManualStockLocked != 0 || reloaded.ManualStockSold != 2 {
		t.Fatalf("unexpected stock lifecycle result: %#v", reloaded)
	}

	unlimited := &productdomain.ProductSKU{
		ProductID:        1,
		SKUCode:          "STOCK-UNLIMITED",
		PriceAmount:      money.FromDecimal(decimal.NewFromInt(100)),
		ManualStockTotal: constants.ManualStockUnlimited,
		IsActive:         true,
	}
	if err := repo.Create(unlimited); err != nil {
		t.Fatalf("create unlimited sku: %v", err)
	}
	if affected, err := repo.ReserveManualStock(unlimited.ID, 1); err != nil || affected != 0 {
		t.Fatalf("unlimited reserve should be no-op, affected=%d err=%v", affected, err)
	}
	if affected, err := repo.ConsumeManualStock(unlimited.ID, 1); err != nil || affected != 0 {
		t.Fatalf("unlimited consume should be no-op, affected=%d err=%v", affected, err)
	}
}

func TestSKUStoreDeleteByProductHidesRowsAndRejectsStockMutations(t *testing.T) {
	repo, db := setupSKUStoreTest(t)
	sku := &productdomain.ProductSKU{
		ProductID:         7,
		SKUCode:           "SOFT-DELETED",
		PriceAmount:       money.FromDecimal(decimal.NewFromInt(100)),
		ManualStockTotal:  10,
		ManualStockLocked: 1,
		IsActive:          true,
	}
	if err := repo.Create(sku); err != nil {
		t.Fatalf("create sku: %v", err)
	}
	if err := repo.DeleteByProduct(sku.ProductID); err != nil {
		t.Fatalf("soft delete skus by product: %v", err)
	}

	var persisted productdomain.ProductSKU
	if err := db.Where("id = ?", sku.ID).First(&persisted).Error; err != nil {
		t.Fatalf("load persisted soft-deleted sku: %v", err)
	}
	if persisted.DeletedAt == nil {
		t.Fatal("expected deleted_at to be persisted")
	}

	byID, err := repo.GetByID(sku.ID)
	if err != nil || byID != nil {
		t.Fatalf("soft-deleted sku must be hidden by id, sku=%#v err=%v", byID, err)
	}
	byCode, err := repo.GetByProductAndCode(sku.ProductID, sku.SKUCode)
	if err != nil || byCode != nil {
		t.Fatalf("soft-deleted sku must be hidden by code, sku=%#v err=%v", byCode, err)
	}
	rows, err := repo.ListByProduct(sku.ProductID, false)
	if err != nil || len(rows) != 0 {
		t.Fatalf("soft-deleted sku must be hidden from list, rows=%#v err=%v", rows, err)
	}

	for operation, mutate := range map[string]func() (int64, error){
		"reserve": func() (int64, error) { return repo.ReserveManualStock(sku.ID, 1) },
		"release": func() (int64, error) { return repo.ReleaseManualStock(sku.ID, 1) },
		"consume": func() (int64, error) { return repo.ConsumeManualStock(sku.ID, 1) },
	} {
		affected, err := mutate()
		if err != nil || affected != 0 {
			t.Fatalf("%s must not mutate soft-deleted sku, affected=%d err=%v", operation, affected, err)
		}
	}
}

func TestSKUSyncFieldsPreserveStopAndOptionalPrice(t *testing.T) {
	for _, applyPrice := range []bool{false, true} {
		t.Run(fmt.Sprint(applyPrice), func(t *testing.T) {
			store, _ := setupSKUStoreTest(t)
			item := &productdomain.ProductSKU{ProductID: 1, SKUCode: "SYNC", PriceAmount: money.FromDecimal(decimal.NewFromInt(10)), CostPriceAmount: money.FromDecimal(decimal.NewFromInt(5)), IsActive: true}
			if err := store.Create(item); err != nil {
				t.Fatal(err)
			}
			if err := store.SetInactive(item.ID); err != nil {
				t.Fatal(err)
			}
			item.SpecValuesJSON = jsonmap.JSON{"version": "new"}
			item.PriceAmount = money.FromDecimal(decimal.NewFromInt(20))
			item.CostPriceAmount = money.FromDecimal(decimal.NewFromInt(8))
			if err := store.UpdateSyncFields(item, applyPrice); err != nil {
				t.Fatal(err)
			}
			got, err := store.GetByID(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantPrice, wantCost := money.FromDecimal(decimal.NewFromInt(10)), money.FromDecimal(decimal.NewFromInt(5))
			if applyPrice {
				wantPrice, wantCost = item.PriceAmount, item.CostPriceAmount
			}
			if got.IsActive || got.SpecValuesJSON["version"] != "new" || !got.PriceAmount.Equal(wantPrice.Decimal) || !got.CostPriceAmount.Equal(wantCost.Decimal) {
				t.Fatalf("sync fields changed stop or unexpected price: %+v", got)
			}
		})
	}
}

func TestSKUUpdatePricesPreservesCurrentSaleStockAndSpecifications(t *testing.T) {
	store, db := setupSKUStoreTest(t)
	item := &productdomain.ProductSKU{ProductID: 1, SKUCode: "OLD", PriceAmount: money.FromDecimal(decimal.NewFromInt(10)), CostPriceAmount: money.FromDecimal(decimal.NewFromInt(5)), IsActive: true}
	if err := store.Create(item); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&productdomain.ProductSKU{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"is_active": false, "sku_code": "EDITED", "spec_values_json": jsonmap.JSON{"version": "current"},
		"manual_stock_total": 70, "manual_stock_locked": 6, "manual_stock_sold": 4, "sort_order": 99,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, prices := range []struct{ price, cost int64 }{{20, 8}, {0, 0}} {
		if err := store.UpdatePrices(item.ID, money.FromDecimal(decimal.NewFromInt(prices.price)), money.FromDecimal(decimal.NewFromInt(prices.cost))); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetByID(item.ID)
		if err != nil || got == nil {
			t.Fatalf("reload = %+v, %v", got, err)
		}
		if !got.PriceAmount.Equal(decimal.NewFromInt(prices.price)) || !got.CostPriceAmount.Equal(decimal.NewFromInt(prices.cost)) {
			t.Fatalf("prices not written: %+v", got)
		}
		if got.IsActive || got.SKUCode != "EDITED" || got.SpecValuesJSON["version"] != "current" || got.ManualStockTotal != 70 || got.ManualStockLocked != 6 || got.ManualStockSold != 4 || got.SortOrder != 99 {
			t.Fatalf("price update overwrote current SKU fields: %+v", got)
		}
	}
}

func TestSKUUpdatePricesRejectsMissingAndSoftDeletedRows(t *testing.T) {
	store, db := setupSKUStoreTest(t)
	for _, id := range []uint{0, 99999} {
		if err := store.UpdatePrices(id, money.FromDecimal(decimal.NewFromInt(20)), money.FromDecimal(decimal.NewFromInt(8))); err == nil {
			t.Fatalf("missing SKU %d must return error", id)
		}
	}
	item := &productdomain.ProductSKU{ProductID: 1, SKUCode: "DELETED-PRICE", PriceAmount: money.FromDecimal(decimal.NewFromInt(10)), CostPriceAmount: money.FromDecimal(decimal.NewFromInt(5)), IsActive: true}
	if err := store.Create(item); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteByProduct(item.ProductID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePrices(item.ID, money.FromDecimal(decimal.NewFromInt(20)), money.FromDecimal(decimal.NewFromInt(8))); err == nil {
		t.Fatal("soft-deleted SKU must return error")
	}
	var got productdomain.ProductSKU
	if err := db.Where("id = ?", item.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.DeletedAt == nil || got.PriceAmount.String() != "10.00" || got.CostPriceAmount.String() != "5.00" {
		t.Fatalf("soft-deleted prices changed: %+v", got)
	}
}
