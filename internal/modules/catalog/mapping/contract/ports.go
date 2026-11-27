package contract

import (
	"context"
	"time"

	categoryapp "github.com/dujiao-next/internal/modules/catalog/category/application"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"
)

// ConnectionProvider 隔离连接读取与上游协议适配器构造。
type ConnectionProvider interface {
	GetByID(id uint) (*siteconnectiondomain.Connection, error)
	GetAdapter(conn *siteconnectiondomain.Connection) (upstream.Adapter, error)
}

// MediaRecorder 记录导入后落盘的上游媒体。
type MediaRecorder interface {
	RecordLocalFile(ctx context.Context, localPath, scene string)
}

// CategoryCreator 自动创建导入商品所需分类。
type CategoryCreator interface {
	Create(input categoryapp.UpsertInput) (*categorydomain.Category, error)
}

// SettingsProvider 提供上游同步动态配置。
type SettingsProvider interface {
	GetUpstreamSyncConfig(fallbackInterval string) (settingsintegration.UpstreamSyncConfig, error)
	GetUpstreamSyncInterval(fallbackInterval string) (time.Duration, error)
}

// PurchaseItem 表示购买核验所需的本地规格与数量；调用方传入全部订单行。
type PurchaseItem struct {
	LocalSKUID uint
	Quantity   int
}

// PurchaseChecker 在收款前核验上游商品、规格及库存；查询失败必须返回错误。
type PurchaseChecker interface {
	CheckUpstreamPurchasable(context.Context, []PurchaseItem) error
}

// UnavailabilityReporter 将采购明确拒绝回写为商品或规格停售，保留同步映射。
type UnavailabilityReporter interface {
	ReportUpstreamUnavailable(context.Context, uint, uint, string) error
}

// AvailabilityMappingRepository 只更新同步字段，不覆盖运营手动启停的映射开关。
type AvailabilityMappingRepository interface {
	MappingRepository
	UpdateSyncState(id uint, status string, syncedAt time.Time) error
	UpdateSyncMetadata(id uint, categoryID uint, fulfillment string) error
}

// AvailabilitySKUMappingRepository 将库存状态与价格更新分开，避免并发同步覆盖停售。
type AvailabilitySKUMappingRepository interface {
	SKUMappingRepository
	UpdateStockState(id uint, quantity int, active bool, syncedAt time.Time) error
	UpdatePrice(id uint, price money.Amount) error
}

// AvailabilitySKURepository 停售仅写启用字段，不回写旧价格和其他规格属性。
type AvailabilitySKURepository interface {
	SKURepository
	SetInactive(id uint) error
	UpdateSyncFields(item *productdomain.ProductSKU, applyPrice bool) error
}

// AvailabilityRepositories 的所有仓储必须绑定到同一个短事务。
type AvailabilityRepositories struct {
	Products    ProductRepository
	SKUs        AvailabilitySKURepository
	Mappings    AvailabilityMappingRepository
	SKUMappings AvailabilitySKUMappingRepository
}

// AvailabilityUnitOfWork 复用现有映射事务装配，外部请求必须在调用前完成。
type AvailabilityUnitOfWork interface {
	WithinAvailabilityTransaction(func(AvailabilityRepositories) error) error
}
