package application

import (
	"strconv"

	siteconnectioncontract "github.com/dujiao-next/internal/modules/siteconnection/contract"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/money"
)

// ReapplyMarkup 对指定连接的所有映射商品重新应用加价规则
func (s *Service) ReapplyMarkup(connectionID uint) (int, error) {
	conn, err := s.connections.GetByID(connectionID)
	if err != nil {
		return 0, err
	}
	if conn == nil {
		return 0, siteconnectioncontract.ErrNotFound
	}

	mappings, err := s.mappings.ListActiveByConnection(connectionID)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, mapping := range mappings {
		skuMappings, err := s.skuMappings.ListByProductMapping(mapping.ID)
		if err != nil {
			continue
		}

		for _, sm := range skuMappings {
			newLocalPrice := CalculateLocalPrice(sm.UpstreamPrice.Decimal, conn.ExchangeRate, conn.PriceMarkupPercent, conn.PriceMarkupAmount.Decimal, conn.PriceMarkupThreshold.Decimal, conn.PriceMarkupMode, conn.PriceRoundingMode)
			localSKU, err := s.skus.GetByID(sm.LocalSKUID)
			if err != nil || localSKU == nil {
				continue
			}
			localSKU.PriceAmount = money.FromDecimal(newLocalPrice.Round(2))
			localSKU.CostPriceAmount = money.FromDecimal(convertCurrency(sm.UpstreamPrice.Decimal, conn.ExchangeRate).Round(2)) // 成本价 = 上游价格 × 汇率（本地币种）
			_ = s.skus.Update(localSKU)
		}

		// 更新 Product.PriceAmount
		localProduct, err := s.products.GetByID(strconv.FormatUint(uint64(mapping.LocalProductID), 10))
		if err == nil && localProduct != nil {
			s.recalcProductPrice(localProduct)
			updated++
		}
	}

	return updated, nil
}

// recalcProductPrice 重新计算商品基准价格和成本价为最低活跃 SKU 价格
func (s *Service) recalcProductPrice(product *productdomain.Product) {
	allSKUs, err := s.skus.ListByProduct(product.ID, true)
	if err != nil || !setMinActiveSKUPrices(product, allSKUs) {
		return
	}
	_ = s.products.QuickUpdate(
		strconv.FormatUint(uint64(product.ID), 10),
		map[string]interface{}{
			"price_amount":      product.PriceAmount,
			"cost_price_amount": product.CostPriceAmount,
		},
	)
}

// setMinActiveSKUPrices 从已加载的 SKU 汇总售价和成本，不进行数据库查询。
func setMinActiveSKUPrices(product *productdomain.Product, skus []productdomain.ProductSKU) bool {
	found := false
	for _, sku := range skus {
		if !sku.IsActive {
			continue
		}
		if !found || sku.PriceAmount.LessThan(product.PriceAmount.Decimal) {
			product.PriceAmount = sku.PriceAmount
		}
		if !found || sku.CostPriceAmount.LessThan(product.CostPriceAmount.Decimal) {
			product.CostPriceAmount = sku.CostPriceAmount
		}
		found = true
	}
	if found {
		product.PriceAmount = money.FromDecimal(product.PriceAmount.Round(2))
		product.CostPriceAmount = money.FromDecimal(product.CostPriceAmount.Round(2))
	}
	return found
}
