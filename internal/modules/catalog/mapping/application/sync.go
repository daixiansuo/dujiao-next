package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	mappingcontract "github.com/dujiao-next/internal/modules/catalog/mapping/contract"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	siteconnectioncontract "github.com/dujiao-next/internal/modules/siteconnection/contract"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"

	"github.com/shopspring/decimal"
)

// SyncProduct 通过详情接口同步单品；停售先持久化，分类与价格失败不影响停售处理。
func (s *Service) SyncProduct(mappingID uint) error {
	mapping, err := s.mappings.GetByID(mappingID)
	if err != nil {
		return err
	}
	if mapping == nil {
		return mappingcontract.ErrMappingNotFound
	}
	conn, err := s.connections.GetByID(mapping.ConnectionID)
	if err != nil {
		return err
	}
	if conn == nil {
		return siteconnectioncontract.ErrNotFound
	}
	adapter, err := s.connections.GetAdapter(conn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observedAt := time.Now()
	product, err := adapter.GetProduct(ctx, mapping.UpstreamProductID)
	if err != nil {
		if errors.Is(err, upstream.ErrUpstreamProductDeleted) {
			return s.markUpstreamUnavailable(mapping, mappingdomain.UpstreamStatusDeleted, observedAt)
		}
		if errors.Is(err, upstream.ErrUpstreamProductUnavailable) {
			return s.markUpstreamUnavailable(mapping, mappingdomain.UpstreamStatusInactive, observedAt)
		}
		return fmt.Errorf("fetch upstream product: %w", err)
	}
	if product == nil || product.ID != mapping.UpstreamProductID {
		return upstream.ErrUpstreamInvalidResponse
	}
	if !product.IsActive {
		return s.markUpstreamUnavailable(mapping, mappingdomain.UpstreamStatusInactive, observedAt)
	}
	parents, ready := s.upstreamCategoryParents(ctx, adapter, conn)
	return s.syncProductFromData(mapping, conn, product, &observedAt, parents, ready)
}

func (s *Service) syncUpstreamWholesalePrices(mapping *mappingdomain.Mapping, localProductID uint, conn *siteconnectiondomain.Connection, upProduct *upstream.UpstreamProduct, markupPercent decimal.Decimal) error {
	if s == nil || mapping == nil || conn == nil || upProduct == nil || localProductID == 0 || len(upProduct.WholesalePrices) == 0 {
		return nil
	}
	localSKUs, err := s.skus.ListByProduct(localProductID, false)
	if err != nil {
		return err
	}
	skuMappings, err := s.skuMappings.ListByProductMapping(mapping.ID)
	if err != nil {
		return err
	}
	wholesalePrices := convertUpstreamWholesalePrices(upProduct.WholesalePrices, conn.ExchangeRate, markupPercent, conn.PriceMarkupAmount.Decimal, conn.PriceMarkupMode, conn.PriceRoundingMode, buildUpstreamWholesaleSKUIndex(localSKUs, upProduct.SKUs, skuMappings))
	if len(wholesalePrices) == 0 {
		logger.Warnw("sync_upstream_wholesale_prices_empty_after_convert",
			"mapping_id", mapping.ID,
			"connection_id", mapping.ConnectionID,
			"upstream_product_id", mapping.UpstreamProductID,
			"local_product_id", localProductID,
			"upstream_tier_count", len(upProduct.WholesalePrices),
		)
		return nil
	}
	return s.products.QuickUpdate(
		strconv.FormatUint(uint64(localProductID), 10),
		map[string]interface{}{"wholesale_prices": wholesalePrices},
	)
}

// markUpstreamUnavailable 原子下架商品和规格、清零缓存；保留映射开关以继续发现恢复。
func (s *Service) markUpstreamUnavailable(mapping *mappingdomain.Mapping, status string, observedAt time.Time) error {
	if mapping == nil {
		return mappingcontract.ErrMappingNotFound
	}
	if err := s.persistAvailability(mapping.ID, &upstream.UpstreamProduct{ID: mapping.UpstreamProductID}, status, observedAt); err != nil {
		return err
	}
	mapping.UpstreamStatus, mapping.LastSyncedAt = status, &observedAt
	logger.Infow("upstream_product_unavailable", "mapping_id", mapping.ID,
		"connection_id", mapping.ConnectionID, "upstream_product_id", mapping.UpstreamProductID,
		"local_product_id", mapping.LocalProductID, "status", status)
	return nil
}

// SyncAllStock 同步所有活跃映射的库存（供定时任务调用）
// 使用 Redis 锁防止任务重叠执行，并发调用上游 API 提升吞吐量
func (s *Service) SyncAllStock(cfg settingsintegration.UpstreamSyncConfig) error {
	ctx := context.Background()
	const lockKey = "upstream:sync_stock_running"

	locked, err := cache.SetNX(ctx, lockKey, "1", 30*time.Minute)
	if err != nil {
		logger.Warnw("sync_stock_lock_error", "error", err)
		// Redis 不可用时降级为直接执行
	} else if !locked {
		logger.Debugw("sync_stock_skip_already_running")
		return nil
	}
	defer cache.Del(ctx, lockKey)

	mappings, err := s.mappings.ListAllActive()
	if err != nil {
		return err
	}
	if len(mappings) == 0 {
		return nil
	}

	// ── 按连接分组 ──
	byConn := make(map[uint][]mappingdomain.Mapping)
	for _, m := range mappings {
		byConn[m.ConnectionID] = append(byConn[m.ConnectionID], m)
	}

	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup

	// 每个连接并发处理，并发数由配置控制
	concurrency := cfg.SyncConnConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	for connID, connMappings := range byConn {
		wg.Add(1)
		sem <- struct{}{}
		go func(connID uint, connMappings []mappingdomain.Mapping) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.SyncConnectionStock(connID, connMappings, cfg.SyncPageSize, cfg.SyncMaxPages); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				logger.Warnw("sync_connection_stock_failed", "connection_id", connID, "error", err)
			}
		}(connID, connMappings)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// EnsureUpstreamStockForOrder 兼容单规格调用，正式购买共用批量核验，查询失败不能放行。
func (s *Service) EnsureUpstreamStockForOrder(localSKUID uint, requiredQty int) error {
	return s.CheckUpstreamPurchasable(context.Background(), []mappingcontract.PurchaseItem{{LocalSKUID: localSKUID, Quantity: requiredQty}})
}

// fullSyncIntervalFloor 保留旧间隔计算的兼容常量；库存任务已改为每轮全量，不再依赖此阈值。
const fullSyncIntervalFloor = 24 * time.Hour

// fullSyncIntervalSyncMultiplier 全量间隔相对增量同步间隔的倍数。
// 例如增量间隔=6h 时，全量间隔=max(24h, 6h*3)=24h；增量=12h 时，全量=max(24h, 36h)=36h。
const fullSyncIntervalSyncMultiplier = 3

// computeFullSyncInterval 保留既有间隔计算接口；SyncConnectionStock 不再使用增量水位。
func (s *Service) computeFullSyncInterval() time.Duration {
	floor := fullSyncIntervalFloor
	if s == nil || s.settings == nil {
		return floor
	}
	syncInterval, err := s.settings.GetUpstreamSyncInterval("")
	if err != nil || syncInterval <= 0 {
		return floor
	}
	scaled := syncInterval * time.Duration(fullSyncIntervalSyncMultiplier)
	if scaled > floor {
		return scaled
	}
	return floor
}

// SyncConnectionStock 每轮读取完整列表。列表缺失只触发详情复核，绝不直接推断删除。
// 分页不完整时仍刷新已取得商品，其余保留；网络和写入失败进入任务汇总错误。
func (s *Service) SyncConnectionStock(connectionID uint, connMappings []mappingdomain.Mapping, pageSize int, maxPages int) error {
	started := time.Now()
	conn, err := s.connections.GetByID(connectionID)
	if err != nil {
		return fmt.Errorf("get connection %d: %w", connectionID, err)
	}
	if conn == nil {
		return siteconnectioncontract.ErrNotFound
	}
	adapter, err := s.connections.GetAdapter(conn)
	if err != nil {
		return err
	}
	if pageSize <= 0 || maxPages <= 0 {
		return fmt.Errorf("invalid sync pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	products := make(map[uint]upstream.UpstreamProduct)
	complete, includesInactive, expectedTotal := false, false, 0
	var failures []error
	updated, missing, inactive, deleted, skipped, failed := 0, 0, 0, 0, 0, 0
	logger.Infow("sync_connection_stock_started", "connection_id", connectionID, "mappings", len(connMappings))
	for page := 1; page <= maxPages; page++ {
		pageCtx, pageCancel := context.WithTimeout(ctx, 2*time.Minute)
		result, listErr := adapter.ListProducts(pageCtx, upstream.ListProductsOpts{Page: page, PageSize: pageSize, IncludeInactive: true})
		pageCancel()
		if listErr != nil {
			failures = append(failures, fmt.Errorf("list page %d: %w", page, listErr))
			break
		}
		if result == nil {
			failures = append(failures, upstream.ErrUpstreamInvalidResponse)
			break
		}
		if page == 1 {
			expectedTotal, includesInactive = result.Total, result.IncludesInactive
		}
		// total 变化、回声页错位、重复页都不能形成完整快照；旧上游可省略回声。
		if result.Total != expectedTotal || result.Total < 0 || (result.Page > 0 && result.Page != page) {
			failures = append(failures, fmt.Errorf("inconsistent pagination on page %d", page))
			break
		}
		before := len(products)
		for _, product := range result.Items {
			products[product.ID] = product
		}
		if len(products) == expectedTotal {
			complete = true
			break
		}
		if len(products) > expectedTotal || len(products) == before {
			failures = append(failures, fmt.Errorf("truncated or repeated pagination on page %d", page))
			break
		}
		if page == maxPages {
			failures = append(failures, fmt.Errorf("sync max pages reached: fetched %d of %d", len(products), expectedTotal))
		}
	}
	parents, priceReady := s.upstreamCategoryParents(ctx, adapter, conn)
	for i := range connMappings {
		mapping := &connMappings[i]
		if !mapping.IsActive {
			skipped++
			continue
		}
		product, exists := products[mapping.UpstreamProductID]
		observedAt := started
		if !exists {
			missing++
			if !complete || mapping.UpstreamStatus == mappingdomain.UpstreamStatusInactive || mapping.UpstreamStatus == mappingdomain.UpstreamStatusDeleted {
				skipped++
				continue
			}
			// 已停售且仍缺失不重复请求。仍在售的缺失商品逐个详情复核，沿用适配器节流。
			observedAt = time.Now()
			detailCtx, detailCancel := context.WithTimeout(ctx, 30*time.Second)
			detail, detailErr := adapter.GetProduct(detailCtx, mapping.UpstreamProductID)
			detailCancel()
			if detailErr != nil {
				status := ""
				if errors.Is(detailErr, upstream.ErrUpstreamProductDeleted) {
					status = mappingdomain.UpstreamStatusDeleted
				}
				if errors.Is(detailErr, upstream.ErrUpstreamProductUnavailable) {
					status = mappingdomain.UpstreamStatusInactive
				}
				if status != "" {
					if writeErr := s.markUpstreamUnavailable(mapping, status, observedAt); writeErr != nil {
						failed++
						failures = append(failures, fmt.Errorf("mapping %d: %w", mapping.ID, writeErr))
					} else if status == mappingdomain.UpstreamStatusDeleted {
						deleted++
					} else {
						inactive++
					}
				} else {
					failed++
					failures = append(failures, fmt.Errorf("mapping %d detail: %w", mapping.ID, detailErr))
					logger.Warnw("sync_missing_product_check_failed", "connection_id", connectionID, "mapping_id", mapping.ID, "upstream_product_id", mapping.UpstreamProductID, "error", detailErr)
				}
				continue
			}
			if detail == nil || detail.ID != mapping.UpstreamProductID {
				failed++
				failures = append(failures, upstream.ErrUpstreamInvalidResponse)
				continue
			}
			product = *detail
		}
		if !product.IsActive {
			if err := s.markUpstreamUnavailable(mapping, mappingdomain.UpstreamStatusInactive, observedAt); err != nil {
				failed++
				failures = append(failures, fmt.Errorf("mapping %d: %w", mapping.ID, err))
			} else {
				inactive++
			}
			continue
		}
		if err := s.syncProductFromData(mapping, conn, &product, &observedAt, parents, priceReady); err != nil {
			failed++
			failures = append(failures, fmt.Errorf("mapping %d: %w", mapping.ID, err))
			logger.Warnw("sync_product_apply_failed", "mapping_id", mapping.ID, "connection_id", connectionID, "error", err)
		} else {
			updated++
		}
	}
	logger.Infow("sync_connection_stock_done", "connection_id", connectionID, "mappings", len(connMappings),
		"upstream_fetched", len(products), "includes_inactive", includesInactive, "fetch_complete", complete,
		"updated", updated, "missing", missing, "inactive", inactive, "deleted", deleted, "skipped", skipped,
		"failed", failed, "errors", len(failures), "duration_ms", time.Since(started).Milliseconds())
	return errors.Join(failures...)
}

// syncProductFromData 先原子刷新可售状态，再写元数据和价格。所有元数据写入均排除启用与库存字段，
// 避免慢速价格同步将较新的停售或运营手动停用覆盖回去。
func (s *Service) syncProductFromData(mapping *mappingdomain.Mapping, conn *siteconnectiondomain.Connection, product *upstream.UpstreamProduct, observedAt *time.Time, parents map[uint]uint, applyPrice bool) error {
	if product == nil || product.ID != mapping.UpstreamProductID {
		return upstream.ErrUpstreamInvalidResponse
	}
	status := mappingdomain.UpstreamStatusActive
	if !product.IsActive {
		status = mappingdomain.UpstreamStatusInactive
	}
	if err := s.persistAvailability(mapping.ID, product, status, *observedAt); err != nil {
		return err
	}
	if !product.IsActive {
		return nil
	}
	applyPrice = applyPrice && supportedPriceMarkupMode(conn)
	markup := ResolveCategoryMarkup(conn.PriceMarkupPercent, conn.CategoryMarkups, product.CategoryID, parents)
	unit := s.transactions.(mappingcontract.AvailabilityUnitOfWork)
	err := unit.WithinAvailabilityTransaction(func(repos mappingcontract.AvailabilityRepositories) error {
		fresh, err := repos.Mappings.GetByID(mapping.ID)
		if err != nil {
			return err
		}
		if fresh == nil {
			return mappingcontract.ErrMappingNotFound
		}
		// 先获取映射写锁并核对时间，避免这轮元数据创建规格时被更新的停售穿插。
		if err := repos.Mappings.UpdateSyncState(fresh.ID, fresh.UpstreamStatus, *observedAt); err != nil {
			return err
		}
		local, err := repos.Products.GetByID(strconv.FormatUint(uint64(fresh.LocalProductID), 10))
		if err != nil {
			return err
		}
		if local == nil {
			return mappingcontract.ErrMappingNotFound
		}
		if product.ManualFormSchema != nil {
			if err := repos.Products.QuickUpdate(strconv.FormatUint(uint64(local.ID), 10), map[string]interface{}{"manual_form_schema_json": product.ManualFormSchema}); err != nil {
				return err
			}
		}
		rows, err := repos.SKUMappings.ListByProductMapping(fresh.ID)
		if err != nil {
			return err
		}
		byID := make(map[uint]mappingdomain.SKUMapping, len(rows))
		for _, row := range rows {
			byID[row.UpstreamSKUID] = row
		}
		for _, sku := range product.SKUs {
			price, priceErr := decimal.NewFromString(sku.PriceAmount)
			validPrice := priceErr == nil && !price.IsNegative()
			if !validPrice {
				logger.Warnw("sync_sku_price_invalid", "mapping_id", fresh.ID, "upstream_sku_id", sku.ID)
			}
			if row, exists := byID[sku.ID]; exists {
				localSKU, err := repos.SKUs.GetByID(row.LocalSKUID)
				if err != nil {
					return err
				}
				if localSKU == nil {
					return fmt.Errorf("local sku %d missing", row.LocalSKUID)
				}
				localSKU.SpecValuesJSON = sku.SpecValues
				if validPrice {
					if err := repos.SKUMappings.UpdatePrice(row.ID, money.FromDecimal(price.Round(2))); err != nil {
						return err
					}
					if conn.AutoSyncPrice && applyPrice {
						localSKU.PriceAmount = money.FromDecimal(CalculateLocalPrice(price, conn.ExchangeRate, markup, conn.PriceMarkupAmount.Decimal, conn.PriceMarkupMode, conn.PriceRoundingMode).Round(2))
						localSKU.CostPriceAmount = money.FromDecimal(convertCurrency(price, conn.ExchangeRate).Round(2))
					}
				}
				if err := repos.SKUs.UpdateSyncFields(localSKU, validPrice && conn.AutoSyncPrice && applyPrice); err != nil {
					return err
				}
				continue
			}
			if !validPrice || !applyPrice {
				continue
			}
			newSKU := productdomain.ProductSKU{ProductID: local.ID, SKUCode: sku.SKUCode, SpecValuesJSON: sku.SpecValues,
				PriceAmount:     money.FromDecimal(CalculateLocalPrice(price, conn.ExchangeRate, markup, conn.PriceMarkupAmount.Decimal, conn.PriceMarkupMode, conn.PriceRoundingMode).Round(2)),
				CostPriceAmount: money.FromDecimal(convertCurrency(price, conn.ExchangeRate).Round(2)),
				IsActive:        local.IsActive && fresh.IsActive && sku.IsActive}
			if err := createUpstreamSKU(repos.SKUs, &newSKU); err != nil {
				return err
			}
			row := &mappingdomain.SKUMapping{ProductMappingID: fresh.ID, LocalSKUID: newSKU.ID, UpstreamSKUID: sku.ID,
				UpstreamPrice: money.FromDecimal(price.Round(2)), UpstreamIsActive: sku.IsActive,
				UpstreamStock: sku.StockQuantity, StockSyncedAt: observedAt}
			if err := createUpstreamSKUMapping(repos.SKUMappings, row); err != nil {
				return err
			}
		}
		if conn.AutoSyncPrice && applyPrice {
			skus, err := repos.SKUs.ListByProduct(local.ID, true)
			if err != nil {
				return err
			}
			if setMinActiveSKUPrices(local, skus) {
				if err := repos.Products.QuickUpdate(strconv.FormatUint(uint64(local.ID), 10), map[string]interface{}{"price_amount": local.PriceAmount, "cost_price_amount": local.CostPriceAmount}); err != nil {
					return err
				}
			}
		}
		fulfillment := product.FulfillmentType
		if fulfillment != constants.FulfillmentTypeAuto {
			fulfillment = constants.FulfillmentTypeManual
		}
		return repos.Mappings.UpdateSyncMetadata(fresh.ID, product.CategoryID, fulfillment)
	})
	if err != nil {
		return err
	}
	mapping.UpstreamCategoryID, mapping.UpstreamStatus, mapping.LastSyncedAt = product.CategoryID, status, observedAt
	if applyPrice && len(product.WholesalePrices) > 0 {
		return s.syncUpstreamWholesalePrices(mapping, mapping.LocalProductID, conn, product, markup)
	}
	return nil
}

// persistAvailability 原子更新库存及停售。at 是发请求前的采样时间，防止旧响应覆盖新拒绝。
// 恢复只刷新上游状态，不重新上架本地商品和规格，也不改变运营的同步开关。
func (s *Service) persistAvailability(mappingID uint, product *upstream.UpstreamProduct, status string, at time.Time) error {
	unit, ok := s.transactions.(mappingcontract.AvailabilityUnitOfWork)
	if !ok {
		return fmt.Errorf("availability transaction is required")
	}
	if product == nil {
		return upstream.ErrUpstreamInvalidResponse
	}
	err := unit.WithinAvailabilityTransaction(func(repos mappingcontract.AvailabilityRepositories) error {
		mapping, err := repos.Mappings.GetByID(mappingID)
		if err != nil {
			return err
		}
		if mapping == nil {
			return mappingcontract.ErrMappingNotFound
		}
		if mapping.LastSyncedAt != nil && mapping.LastSyncedAt.After(at) {
			return fmt.Errorf("mapping %d has newer availability", mappingID)
		}
		// 各入口统一先锁映射、再锁规格，避免 PostgreSQL 并发时形成反向锁等待。
		if err := repos.Mappings.UpdateSyncState(mappingID, status, at); err != nil {
			return err
		}
		local, err := repos.Products.GetByID(strconv.FormatUint(uint64(mapping.LocalProductID), 10))
		if err != nil {
			return err
		}
		if local == nil {
			return mappingcontract.ErrMappingNotFound
		}
		available := status == mappingdomain.UpstreamStatusActive && product.IsActive
		if !available && local.IsActive {
			if err := repos.Products.QuickUpdate(strconv.FormatUint(uint64(local.ID), 10), map[string]interface{}{"is_active": false}); err != nil {
				return err
			}
		}
		byID := make(map[uint]upstream.UpstreamSKU, len(product.SKUs))
		for _, sku := range product.SKUs {
			byID[sku.ID] = sku
		}
		skuMappings, err := repos.SKUMappings.ListByProductMapping(mappingID)
		if err != nil {
			return err
		}
		for _, sm := range skuMappings {
			sku, found := byID[sm.UpstreamSKUID]
			active, stock := available && found && sku.IsActive, 0
			if active {
				stock = sku.StockQuantity
			}
			if err := repos.SKUMappings.UpdateStockState(sm.ID, stock, active, at); err != nil {
				return err
			}
			if !active {
				if err := repos.SKUs.SetInactive(sm.LocalSKUID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		logger.Warnw("upstream_availability_write_failed", "mapping_id", mappingID, "status", status, "error", err)
	}
	return err
}

type purchaseMapping struct {
	mapping *mappingdomain.Mapping
	items   map[uint]int
}

// CheckUpstreamPurchasable 对正式购买作失败即阻断的核验，不修改售价或创建规格。
// 一次核验按上游商品复用详情，按上游规格汇总数量；缓存充足和无限库存都需核验状态。
func (s *Service) CheckUpstreamPurchasable(ctx context.Context, items []mappingcontract.PurchaseItem) error {
	if len(items) == 0 {
		return nil
	}
	if s == nil || s.skuMappings == nil || s.mappings == nil || s.products == nil || s.skus == nil {
		return mappingcontract.ErrUpstreamCheckFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	remote := true
	if s.settings != nil {
		if cfg, err := s.settings.GetUpstreamSyncConfig(""); err == nil {
			remote = cfg.PreOrderStockCheckEnabled
		}
	}
	groups := make(map[uint]*purchaseMapping)
	totals := make(map[string]int)
	for _, item := range items {
		if item.LocalSKUID == 0 || item.Quantity <= 0 {
			return mappingcontract.ErrUpstreamCheckFailed
		}
		sku, err := s.skus.GetByID(item.LocalSKUID)
		if err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if sku == nil || !sku.IsActive {
			return mappingcontract.ErrUpstreamProductNotFound
		}
		local, err := s.products.GetByID(strconv.FormatUint(uint64(sku.ProductID), 10))
		if err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if local == nil || !local.IsActive {
			return mappingcontract.ErrUpstreamProductNotFound
		}
		if local.FulfillmentType != constants.FulfillmentTypeUpstream {
			continue
		}
		sm, err := s.skuMappings.GetByLocalSKUID(item.LocalSKUID)
		if err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if sm == nil {
			return mappingcontract.ErrMappingNotFound
		}
		mapping, err := s.mappings.GetByID(sm.ProductMappingID)
		if err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if mapping == nil || mapping.LocalProductID != local.ID {
			return mappingcontract.ErrMappingNotFound
		}
		if !mapping.IsActive {
			return mappingcontract.ErrMappingInactive
		}
		if mapping.UpstreamStatus != "" && mapping.UpstreamStatus != mappingdomain.UpstreamStatusActive || !sm.UpstreamIsActive {
			return mappingcontract.ErrUpstreamProductNotFound
		}
		group := groups[mapping.ID]
		if group == nil {
			group = &purchaseMapping{mapping: mapping, items: make(map[uint]int)}
			groups[mapping.ID] = group
		}
		group.items[sm.UpstreamSKUID] += item.Quantity
		key := fmt.Sprintf("%d/%d", mapping.ConnectionID, sm.UpstreamSKUID)
		totals[key] += item.Quantity
	}
	type observation struct {
		product *upstream.UpstreamProduct
		at      time.Time
	}
	products := make(map[string]observation)
	for _, group := range groups {
		mapping := group.mapping
		if !remote {
			rows, err := s.skuMappings.ListByProductMapping(mapping.ID)
			if err != nil {
				return mappingcontract.ErrUpstreamCheckFailed
			}
			byID := make(map[uint]mappingdomain.SKUMapping, len(rows))
			for _, sm := range rows {
				byID[sm.UpstreamSKUID] = sm
			}
			for id := range group.items {
				sm, exists := byID[id]
				if !exists || !sm.UpstreamIsActive {
					return mappingcontract.ErrUpstreamProductNotFound
				}
				if sm.UpstreamStock < -1 {
					return mappingcontract.ErrUpstreamCheckFailed
				}
				if sm.UpstreamStock >= 0 && sm.UpstreamStock < totals[fmt.Sprintf("%d/%d", mapping.ConnectionID, id)] {
					return mappingcontract.ErrUpstreamStockInsufficient
				}
			}
			continue
		}
		key := fmt.Sprintf("%d/%d", mapping.ConnectionID, mapping.UpstreamProductID)
		cached := products[key]
		product, at := cached.product, cached.at
		if product == nil {
			at = time.Now()
		}
		if product == nil {
			if s.connections == nil {
				return mappingcontract.ErrUpstreamCheckFailed
			}
			conn, err := s.connections.GetByID(mapping.ConnectionID)
			if err != nil || conn == nil {
				return mappingcontract.ErrUpstreamCheckFailed
			}
			adapter, err := s.connections.GetAdapter(conn)
			if err != nil {
				return mappingcontract.ErrUpstreamCheckFailed
			}
			product, err = adapter.GetProduct(ctx, mapping.UpstreamProductID)
			if err != nil {
				status := ""
				if errors.Is(err, upstream.ErrUpstreamProductDeleted) {
					status = mappingdomain.UpstreamStatusDeleted
				}
				if errors.Is(err, upstream.ErrUpstreamProductUnavailable) {
					status = mappingdomain.UpstreamStatusInactive
				}
				if status != "" {
					if writeErr := s.markUpstreamUnavailable(mapping, status, at); writeErr != nil {
						return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, writeErr)
					}
					return mappingcontract.ErrUpstreamProductNotFound
				}
				logger.Warnw("upstream_purchase_check_failed", "mapping_id", mapping.ID, "connection_id", mapping.ConnectionID, "error", err)
				return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
			}
			if product == nil || product.ID != mapping.UpstreamProductID {
				return mappingcontract.ErrUpstreamCheckFailed
			}
			products[key] = observation{product: product, at: at}
		}
		status := mappingdomain.UpstreamStatusActive
		if !product.IsActive {
			status = mappingdomain.UpstreamStatusInactive
		}
		if err := s.persistAvailability(mapping.ID, product, status, at); err != nil {
			return fmt.Errorf("%w: %v", mappingcontract.ErrUpstreamCheckFailed, err)
		}
		if !product.IsActive {
			return mappingcontract.ErrUpstreamProductNotFound
		}
		byID := make(map[uint]upstream.UpstreamSKU, len(product.SKUs))
		for _, sku := range product.SKUs {
			byID[sku.ID] = sku
		}
		for id := range group.items {
			sku, exists := byID[id]
			if !exists || !sku.IsActive {
				return mappingcontract.ErrUpstreamProductNotFound
			}
			if sku.StockQuantity < -1 {
				return mappingcontract.ErrUpstreamCheckFailed
			}
			if sku.StockQuantity >= 0 && sku.StockQuantity < totals[fmt.Sprintf("%d/%d", mapping.ConnectionID, id)] {
				return mappingcontract.ErrUpstreamStockInsufficient
			}
		}
	}
	return nil
}

// ReportUpstreamUnavailable 消费采购的结构化拒绝，仅更新被拒绝商品或规格。
// 库存不足保持 SKU 启用，以便下一次补货自然恢复；其他停售需运营复售。
func (s *Service) ReportUpstreamUnavailable(ctx context.Context, productID, skuID uint, code string) error {
	code = strings.ToLower(strings.TrimSpace(code))
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	mapping, err := s.mappings.GetByLocalProductID(productID)
	if err != nil {
		return err
	}
	if mapping == nil {
		return mappingcontract.ErrMappingNotFound
	}
	at := time.Now()
	switch code {
	case "product_not_found", "product_unavailable", "product_deleted":
		status := mappingdomain.UpstreamStatusInactive
		if code == "product_deleted" {
			status = mappingdomain.UpstreamStatusDeleted
		}
		return s.markUpstreamUnavailable(mapping, status, at)
	case "sku_not_found", "sku_deleted", "sku_unavailable", "product_out_of_stock", "sku_out_of_stock", "insufficient_stock":
		unit, ok := s.transactions.(mappingcontract.AvailabilityUnitOfWork)
		if !ok {
			return fmt.Errorf("availability transaction is required")
		}
		outOfStock := code == "product_out_of_stock" || code == "sku_out_of_stock" || code == "insufficient_stock"
		err := unit.WithinAvailabilityTransaction(func(repos mappingcontract.AvailabilityRepositories) error {
			fresh, err := repos.Mappings.GetByID(mapping.ID)
			if err != nil {
				return err
			}
			if fresh == nil {
				return mappingcontract.ErrMappingNotFound
			}
			// 与商品同步保持相同锁顺序，旧状态和后续任一步失败都整体回滚。
			if err := repos.Mappings.UpdateSyncState(mapping.ID, fresh.UpstreamStatus, at); err != nil {
				return err
			}
			sm, err := repos.SKUMappings.GetByLocalSKUID(skuID)
			if err != nil {
				return err
			}
			if sm == nil || sm.ProductMappingID != mapping.ID {
				return mappingcontract.ErrMappingNotFound
			}
			if err := repos.SKUMappings.UpdateStockState(sm.ID, 0, outOfStock && sm.UpstreamIsActive, at); err != nil {
				return err
			}
			if !outOfStock {
				if err := repos.SKUs.SetInactive(skuID); err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			logger.Infow("upstream_sku_unavailable", "mapping_id", mapping.ID, "local_sku_id", skuID, "reason", code)
		}
		return err
	default:
		return nil
	}
}
