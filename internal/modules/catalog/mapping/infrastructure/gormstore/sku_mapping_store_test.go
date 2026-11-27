package gormstore

import (
	"testing"
	"time"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"

	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
)

func TestSKUMappingStoreListByProductMappingIDs(t *testing.T) {
	_, skuStore, _ := setupMappingStoreTest(t)

	for i, mappingID := range []uint{11, 11, 22} {
		if err := skuStore.Create(&mappingdomain.SKUMapping{
			ProductMappingID: mappingID,
			LocalSKUID:       uint(100 + i),
			UpstreamSKUID:    uint(200 + i),
		}); err != nil {
			t.Fatalf("create sku mapping failed: %v", err)
		}
	}

	if rows, err := skuStore.ListByProductMappingIDs(nil); err != nil || rows != nil {
		t.Fatalf("empty input should short-circuit: rows=%v err=%v", rows, err)
	}
	rows, err := skuStore.ListByProductMappingIDs([]uint{11})
	if err != nil || len(rows) != 2 {
		t.Fatalf("list by mapping ids: rows=%d err=%v, want 2", len(rows), err)
	}
}

func TestSKUMappingStoreGetByLocalSKUIDReturnsNilWhenMissing(t *testing.T) {
	_, skuStore, _ := setupMappingStoreTest(t)

	mapping, err := skuStore.GetByLocalSKUID(999)
	if err != nil || mapping != nil {
		t.Fatalf("missing sku mapping should be nil without error: mapping=%v err=%v", mapping, err)
	}
}

func TestSKUMappingStoreDeleteHidesRowsAndPersistsMarker(t *testing.T) {
	_, skuStore, db := setupMappingStoreTest(t)
	mapping := &mappingdomain.SKUMapping{
		ProductMappingID: 77,
		LocalSKUID:       88,
		UpstreamSKUID:    99,
	}
	if err := skuStore.Create(mapping); err != nil {
		t.Fatalf("create sku mapping: %v", err)
	}
	if err := skuStore.DeleteByProductMapping(mapping.ProductMappingID); err != nil {
		t.Fatalf("delete sku mapping: %v", err)
	}
	if got, err := skuStore.GetByLocalSKUID(mapping.LocalSKUID); err != nil || got != nil {
		t.Fatalf("GetByLocalSKUID after delete = %#v, %v; want nil, nil", got, err)
	}
	var persisted mappingdomain.SKUMapping
	if err := db.Where("id = ?", mapping.ID).First(&persisted).Error; err != nil {
		t.Fatalf("load persisted sku mapping: %v", err)
	}
	if persisted.DeletedAt == nil {
		t.Fatal("DeleteByProductMapping must persist deleted_at")
	}
}

func TestSKUMappingPriceWritesCannotUndoNewerStop(t *testing.T) {
	_, store, _ := setupMappingStoreTest(t)
	item := &mappingdomain.SKUMapping{ProductMappingID: 1, LocalSKUID: 10, UpstreamSKUID: 20, UpstreamStock: 99, UpstreamIsActive: true}
	if err := store.Create(item); err != nil {
		t.Fatal(err)
	}
	stoppedAt := time.Now().UTC()
	if err := store.UpdateStockState(item.ID, 0, false, stoppedAt); err != nil {
		t.Fatal(err)
	}
	price := money.FromDecimal(decimal.NewFromInt(12))
	if err := store.UpdatePrice(item.ID, price); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStockState(item.ID, 99, true, stoppedAt.Add(-time.Second)); err == nil {
		t.Fatal("stale stock must not overwrite newer stop")
	}
	got, err := store.GetByLocalSKUID(item.LocalSKUID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UpstreamIsActive || got.UpstreamStock != 0 || !got.StockSyncedAt.Equal(stoppedAt) || !got.UpstreamPrice.Equal(price.Decimal) {
		t.Fatalf("price sync overwrote newer stop: %+v", got)
	}
}
