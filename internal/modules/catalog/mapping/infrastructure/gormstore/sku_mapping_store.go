package gormstore

import (
	"errors"
	"fmt"
	"time"

	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	"github.com/dujiao-next/internal/shared/money"

	"gorm.io/gorm"
)

// SKUMappingStore 是 Catalog 上游 SKU 映射端口的 GORM 实现。
type SKUMappingStore struct {
	db *gorm.DB
}

var _ mappingcontract.SKUMappingRepository = (*SKUMappingStore)(nil)

func NewSKUMappingStore(db *gorm.DB) *SKUMappingStore {
	return &SKUMappingStore{db: db}
}

// WithTx 绑定事务
func (r *SKUMappingStore) WithTx(tx *gorm.DB) *SKUMappingStore {
	if tx == nil {
		return r
	}
	return NewSKUMappingStore(tx)
}

// BindTx 将事务内 store 暴露为导入用例所需的窄写入端口。
func (r *SKUMappingStore) BindTx(tx *gorm.DB) mappingcontract.SKUMappingRepository {
	return r.WithTx(tx)
}

func (r *SKUMappingStore) GetByLocalSKUID(skuID uint) (*mappingdomain.SKUMapping, error) {
	var m mappingdomain.SKUMapping
	if err := r.db.Where("deleted_at IS NULL AND local_sku_id = ?", skuID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *SKUMappingStore) ListByProductMapping(productMappingID uint) ([]mappingdomain.SKUMapping, error) {
	var mappings []mappingdomain.SKUMapping
	if err := r.db.Where("deleted_at IS NULL AND product_mapping_id = ?", productMappingID).Find(&mappings).Error; err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *SKUMappingStore) ListByProductMappingIDs(productMappingIDs []uint) ([]mappingdomain.SKUMapping, error) {
	if len(productMappingIDs) == 0 {
		return nil, nil
	}
	var mappings []mappingdomain.SKUMapping
	if err := r.db.Where("deleted_at IS NULL AND product_mapping_id IN ?", productMappingIDs).Find(&mappings).Error; err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *SKUMappingStore) Create(mapping *mappingdomain.SKUMapping) error {
	return r.db.Create(mapping).Error
}

func (r *SKUMappingStore) Update(mapping *mappingdomain.SKUMapping) error {
	return r.db.Save(mapping).Error
}

func (r *SKUMappingStore) DeleteByProductMapping(productMappingID uint) error {
	return r.db.Model(&mappingdomain.SKUMapping{}).
		Where("deleted_at IS NULL AND product_mapping_id = ?", productMappingID).
		Update("deleted_at", time.Now()).Error
}

// UpdateStockState 不覆盖成本价；以采样开始时间保护较新的停售结果。
func (r *SKUMappingStore) UpdateStockState(id uint, quantity int, active bool, at time.Time) error {
	result := r.db.Model(&mappingdomain.SKUMapping{}).
		Where("id = ? AND deleted_at IS NULL AND (stock_synced_at IS NULL OR stock_synced_at <= ?)", id, at).
		Updates(map[string]interface{}{"upstream_stock": quantity, "upstream_is_active": active, "stock_synced_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("sku mapping %d has newer state or no longer exists", id)
	}
	return nil
}

// UpdatePrice 仅更新上游成本价，不覆盖较新的库存和停售状态。
func (r *SKUMappingStore) UpdatePrice(id uint, price money.Amount) error {
	return r.db.Model(&mappingdomain.SKUMapping{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("upstream_price", price).Error
}
