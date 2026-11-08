package integrationtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	memberleveldomain "github.com/dujiao-next/internal/modules/memberlevel/domain"

	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/i18n"
	cardsecretgormstore "github.com/dujiao-next/internal/modules/cardsecret/infrastructure/gormstore"
	cartdomain "github.com/dujiao-next/internal/modules/cart/domain"
	cartgormstore "github.com/dujiao-next/internal/modules/cart/infrastructure/gormstore"
	categorygormstore "github.com/dujiao-next/internal/modules/catalog/category/infrastructure/gormstore"
	mappinggormstore "github.com/dujiao-next/internal/modules/catalog/mapping/infrastructure/gormstore"
	productapplication "github.com/dujiao-next/internal/modules/catalog/product/application"
	productadmin "github.com/dujiao-next/internal/modules/catalog/product/application/admin"
	productwrite "github.com/dujiao-next/internal/modules/catalog/product/application/write"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	producthttp "github.com/dujiao-next/internal/modules/catalog/product/transport/http"
	memberlevelgormstore "github.com/dujiao-next/internal/modules/memberlevel/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/jsonslice"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type productHandlerFacade struct {
	*productapplication.Service
	*productadmin.AdminService
	*productwrite.WriteService
}

type productWriteUoW struct {
	products    *productgormstore.ProductStore
	skus        *productgormstore.SKUStore
	cardSecrets *cardsecretgormstore.Store
}

func (unit *productWriteUoW) WithinTransaction(fn func(productwrite.TransactionRepositories) error) error {
	if fn == nil {
		return nil
	}
	return unit.products.Transaction(func(tx *gorm.DB) error {
		return fn(productwrite.TransactionRepositories{
			Products:    unit.products.BindTx(tx),
			SKUs:        unit.skus.BindTx(tx),
			CardSecrets: unit.cardSecrets.BindTx(tx),
		})
	})
}

type productAdminUoW struct {
	products          *productgormstore.ProductStore
	productSKUs       *productgormstore.SKUStore
	cardSecrets       *cardsecretgormstore.Store
	cardSecretBatches *cardsecretgormstore.BatchStore
	memberLevelPrices *memberlevelgormstore.PriceStore
	carts             *cartgormstore.Store
	productMappings   *mappinggormstore.MappingStore
}

func (unit *productAdminUoW) WithinTransaction(fn func(productadmin.DeleteRepositories) error) error {
	if fn == nil {
		return nil
	}
	return unit.products.Transaction(func(tx *gorm.DB) error {
		return fn(productadmin.DeleteRepositories{
			Products:          unit.products.BindTx(tx),
			CardSecrets:       unit.cardSecrets.BindTx(tx),
			CardSecretBatches: unit.cardSecretBatches.BindTx(tx),
			SKUs:              unit.productSKUs.BindTx(tx),
			MemberLevelPrices: memberlevelgormstore.NewPriceStore(tx),
			Carts:             unit.carts.WithTx(tx),
			ProductMappings:   unit.productMappings.WithTx(tx),
		})
	})
}

type orderHistoryStore struct{ db *gorm.DB }

func (s *orderHistoryStore) CountOrderItemsByProduct(productID uint) (int64, error) {
	var count int64
	err := s.db.Model(&orderdomain.OrderItem{}).Where("product_id = ?", productID).Count(&count).Error
	return count, err
}

type paymentChannelStore struct{ db *gorm.DB }

func (s *paymentChannelStore) ListByIDs(ids []uint) ([]paymentdomain.PaymentChannel, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []paymentdomain.PaymentChannel
	err := s.db.Where("id IN ?", ids).Find(&rows).Error
	return rows, err
}

func setupAdminProductHandlerTest(t *testing.T) (*producthttp.AdminProductHandler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:admin_product_handler_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&categorydomain.Category{},
		&productdomain.Product{},
		&productdomain.ProductSKU{},
		&cardsecretdomain.Secret{},
		&cardsecretdomain.Batch{},
		&memberleveldomain.MemberLevelPrice{},
		&cartdomain.Item{},
		&mappingdomain.Mapping{},
		&mappingdomain.SKUMapping{},
		&orderdomain.Order{},
		&orderdomain.OrderItem{},
		&paymentdomain.PaymentChannel{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	productStore := productgormstore.NewProductStore(db)
	skuStore := productgormstore.NewSKUStore(db)
	cardSecretStore := cardsecretgormstore.New(db)
	cardSecretBatchStore := cardsecretgormstore.NewBatch(db)
	categoryStore := categorygormstore.NewCategoryStore(db)
	memberLevelPriceStore := memberlevelgormstore.NewPriceStore(db)
	mappingStore := mappinggormstore.NewMappingStore(db)
	skuMappingStore := mappinggormstore.NewSKUMappingStore(db)
	cartStore := cartgormstore.New(db)

	facade := &productHandlerFacade{
		Service: productapplication.NewService(productapplication.Options{
			Products:   productStore,
			Categories: categoryStore,
			Stock:      cardSecretStore,
		}),
		AdminService: productadmin.NewAdminService(productadmin.Options{
			Products:    productStore,
			Categories:  categoryStore,
			CardSecrets: cardSecretStore,
			Orders:      &orderHistoryStore{db: db},
			Transactions: &productAdminUoW{
				products:          productStore,
				productSKUs:       skuStore,
				cardSecrets:       cardSecretStore,
				cardSecretBatches: cardSecretBatchStore,
				memberLevelPrices: memberLevelPriceStore,
				carts:             cartStore,
				productMappings:   mappingStore,
			},
		}),
		WriteService: productwrite.NewWriteService(productwrite.Options{
			Products:        productStore,
			SKUs:            skuStore,
			Categories:      categoryStore,
			PaymentChannels: &paymentChannelStore{db: db},
			Transactions: &productWriteUoW{
				products:    productStore,
				skus:        skuStore,
				cardSecrets: cardSecretStore,
			},
		}),
	}

	h := producthttp.NewAdminProductHandler(facade, facade, facade, nil, mappingStore, skuMappingStore)
	return h, db
}

func TestBatchUpdateProductStatusReturnsFailureReasons(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	product := productdomain.Product{
		CategoryID:      0,
		Slug:            "batch-uncategorized-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "batch-uncategorized-product"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(10)),
		FulfillmentType: constants.FulfillmentTypeUpstream,
		IsMapped:        true,
		IsActive:        false,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create uncategorized product failed: %v", err)
	}

	body := fmt.Sprintf(`{"ids":[%d],"is_active":true}`, product.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/batch-status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	h.BatchUpdateProductStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Total        int `json:"total"`
			SuccessCount int `json:"success_count"`
			FailedItems  []struct {
				ID        uint   `json:"id"`
				ErrorCode string `json:"error_code"`
				Message   string `json:"message"`
			} `json:"failed_items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Data.Total != 1 || resp.Data.SuccessCount != 0 {
		t.Fatalf("unexpected counts: total=%d success=%d", resp.Data.Total, resp.Data.SuccessCount)
	}
	if len(resp.Data.FailedItems) != 1 {
		t.Fatalf("expected one failed item, got %+v", resp.Data.FailedItems)
	}
	if resp.Data.FailedItems[0].ID != product.ID {
		t.Fatalf("expected failed product id %d, got %d", product.ID, resp.Data.FailedItems[0].ID)
	}
	if resp.Data.FailedItems[0].ErrorCode != "product_category_invalid" {
		t.Fatalf("expected product_category_invalid, got %q", resp.Data.FailedItems[0].ErrorCode)
	}
}

func TestBatchUpdateProductStockDisplay(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	product := productdomain.Product{
		Slug:            "batch-stock-display-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "batch-stock-display-product"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(10)),
		FulfillmentType: constants.FulfillmentTypeUpstream,
		IsMapped:        true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/batch-stock-display", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		h.BatchUpdateProductStockDisplay(c)
		var resp struct {
			StatusCode int `json:"status_code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
		}
		return resp.StatusCode
	}

	if code := post(fmt.Sprintf(`{"ids":[%d],"stock_display_mode":"bogus"}`, product.ID)); code == 0 {
		t.Fatal("expected invalid mode to be rejected")
	}
	if code := post(fmt.Sprintf(`{"ids":[%d],"stock_display_mode":"range"}`, product.ID)); code != 0 {
		t.Fatalf("expected success, got status_code %d", code)
	}
	var reloaded productdomain.Product
	if err := db.First(&reloaded, product.ID).Error; err != nil {
		t.Fatalf("reload product failed: %v", err)
	}
	if reloaded.StockDisplayMode != constants.ProductStockDisplayRange {
		t.Fatalf("expected range, got %q", reloaded.StockDisplayMode)
	}
}

func TestUpdateProductWholesalePricesHandlerUpdatesTiers(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	product := productdomain.Product{
		CategoryID:  1,
		Slug:        "handler-wholesale-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "handler-wholesale-product"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(100)),
		IsActive:    true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	body := `{"wholesale_prices":[{"min_quantity":10,"unit_price":70},{"min_quantity":5,"unit_price":80}]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/products/1/wholesale-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", product.ID)}}

	h.UpdateProductWholesalePrices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data productdomain.Product `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data.WholesalePrices) != 2 {
		t.Fatalf("expected 2 wholesale tiers, got %+v", resp.Data.WholesalePrices)
	}
	if resp.Data.WholesalePrices[0].MinQuantity != 5 || resp.Data.WholesalePrices[0].UnitPrice.String() != "80.00" {
		t.Fatalf("expected sorted first tier min=5 price=80.00, got %+v", resp.Data.WholesalePrices[0])
	}
}

func TestUpdateProductWholesalePricesHandlerAllowsClear(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	product := productdomain.Product{
		CategoryID:  1,
		Slug:        "handler-wholesale-clear",
		TitleJSON:   jsonmap.JSON{"zh-CN": "handler-wholesale-clear"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(100)),
		WholesalePrices: productdomain.WholesalePriceTiers{
			{MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(80))},
		},
		IsActive: true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/products/1/wholesale-prices", strings.NewReader(`{"wholesale_prices":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", product.ID)}}

	h.UpdateProductWholesalePrices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got productdomain.Product
	if err := db.First(&got, product.ID).Error; err != nil {
		t.Fatalf("reload product failed: %v", err)
	}
	if len(got.WholesalePrices) != 0 {
		t.Fatalf("expected wholesale prices cleared, got %+v", got.WholesalePrices)
	}
}

func TestUpdateProductWholesalePricesHandlerRejectsInvalidTier(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	product := productdomain.Product{
		CategoryID:  1,
		Slug:        "handler-wholesale-invalid",
		TitleJSON:   jsonmap.JSON{"zh-CN": "handler-wholesale-invalid"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(100)),
		IsActive:    true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/products/1/wholesale-prices", strings.NewReader(`{"wholesale_prices":[{"min_quantity":0,"unit_price":80}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", product.ID)}}

	h.UpdateProductWholesalePrices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("project response wrapper should still return HTTP 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		StatusCode int    `json:"status_code"`
		Msg        string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected wholesale invalid response, got status_code=%d body=%s", resp.StatusCode, w.Body.String())
	}
}

func TestUpdateProductWholesalePricesHandlerReturnsNotFound(t *testing.T) {
	h, _ := setupAdminProductHandlerTest(t)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/products/999999/wholesale-prices", strings.NewReader(`{"wholesale_prices":[{"min_quantity":5,"unit_price":80}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "999999"}}

	h.UpdateProductWholesalePrices(c)

	if w.Code != http.StatusOK {
		t.Fatalf("project response wrapper should still return HTTP 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected product not found response, got body=%s", w.Body.String())
	}
}

// 后台商品列表的上架状态筛选：is_active=1 只看已上架，is_active=0 只看已下架。
func TestAdminProductListFiltersByActiveState(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	seed := func(slug string, isActive bool) productdomain.Product {
		row := productdomain.Product{
			CategoryID:       1,
			Slug:             slug,
			TitleJSON:        jsonmap.JSON{"zh-CN": "上架状态筛选商品"},
			PriceAmount:      money.FromDecimal(decimal.NewFromInt(10)),
			FulfillmentType:  constants.FulfillmentTypeManual,
			ManualStockTotal: 5,
			IsActive:         isActive,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create product %s failed: %v", slug, err)
		}
		return row
	}
	active := seed("api-state-active", true)
	inactive := seed("api-state-inactive", false)

	listSlugs := func(query string) map[string]bool {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products?"+query, nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		h.GetAdminProducts(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200 for %q, got %d body=%s", query, w.Code, w.Body.String())
		}
		var resp struct {
			Data []productdomain.Product `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
		}
		got := make(map[string]bool, len(resp.Data))
		for _, row := range resp.Data {
			got[row.Slug] = true
		}
		return got
	}

	got := listSlugs("is_active=1")
	if !got[active.Slug] || got[inactive.Slug] {
		t.Fatalf("is_active=1 should return only active rows, got %+v", got)
	}

	got = listSlugs("is_active=0")
	if !got[inactive.Slug] || got[active.Slug] {
		t.Fatalf("is_active=0 should return only inactive rows, got %+v", got)
	}

	got = listSlugs("is_active=all")
	if !got[active.Slug] || !got[inactive.Slug] {
		t.Fatalf("is_active=all should return both rows, got %+v", got)
	}

	// 与其他条件联动：is_active=0 叠加 search，仍应 AND 生效
	got = listSlugs("is_active=0&search=" + url.QueryEscape("上架状态筛选"))
	if !got[inactive.Slug] || got[active.Slug] {
		t.Fatalf("is_active=0 + search should still exclude active rows, got %+v", got)
	}

	got = listSlugs("is_active=0&search=" + url.QueryEscape("绝不匹配的关键词"))
	if len(got) != 0 {
		t.Fatalf("is_active=0 + non-matching search should be empty, got %+v", got)
	}
}

func setupBatchProductImagesTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	h, db := setupAdminProductHandlerTest(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection failed: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite failed: %v", err)
		}
	})
	router := gin.New()
	producthttp.RegisterAdminRoutes(router.Group("/api/v1/admin"), h)
	return router, db
}

type batchProductImagesResponse struct {
	StatusCode int    `json:"status_code"`
	Msg        string `json:"msg"`
	Data       struct {
		Total        int `json:"total"`
		SuccessCount int `json:"success_count"`
		FailedItems  []struct {
			ID        uint   `json:"id"`
			ErrorCode string `json:"error_code"`
			Message   string `json:"message"`
		} `json:"failed_items"`
	} `json:"data"`
}

func requestBatchProductImages(t *testing.T, router *gin.Engine, body, locale string) batchProductImagesResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/batch-images", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", locale)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp batchProductImagesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch images response failed: %v body=%s", err, w.Body.String())
	}
	return resp
}

func loadBatchImagesProduct(t *testing.T, db *gorm.DB, id uint) productdomain.Product {
	t.Helper()
	var product productdomain.Product
	if err := db.Preload("SKUs").First(&product, id).Error; err != nil {
		t.Fatalf("reload product %d failed: %v", id, err)
	}
	return product
}

func TestBatchUpdateProductImagesPersistsOnlyImages(t *testing.T) {
	router, db := setupBatchProductImagesTest(t)
	products := make([]productdomain.Product, 3)
	for i := range products {
		products[i] = productdomain.Product{
			CategoryID:           7,
			Slug:                 fmt.Sprintf("batch-images-preserve-%d", i),
			TitleJSON:            jsonmap.JSON{"zh-CN": "商品标题", "en-US": "Product title"},
			DescriptionJSON:      jsonmap.JSON{"zh-CN": "商品描述"},
			ContentJSON:          jsonmap.JSON{"en-US": "Product content"},
			InstructionsJSON:     jsonmap.JSON{"en-US": "Usage instructions"},
			PriceAmount:          money.FromDecimal(decimal.NewFromInt(29)),
			CostPriceAmount:      money.FromDecimal(decimal.NewFromInt(11)),
			WholesalePrices:      productdomain.WholesalePriceTiers{{MinQuantity: 3, UnitPrice: money.FromDecimal(decimal.NewFromInt(25))}},
			Images:               jsonslice.Strings{"/uploads/old-cover.png", "/uploads/old-detail.png"},
			Tags:                 jsonslice.Strings{"featured", "original"},
			PurchaseType:         "member",
			MinPurchaseQuantity:  2,
			MaxPurchaseQuantity:  10,
			StockDisplayMode:     "hidden",
			FulfillmentType:      constants.FulfillmentTypeManual,
			ManualFormSchemaJSON: jsonmap.JSON{"version": float64(1)},
			ManualStockTotal:     15,
			ManualStockLocked:    2,
			ManualStockSold:      4,
			PaymentChannelIDs:    "[2,3]",
			IsAffiliateEnabled:   true,
			IsActive:             i == 0,
			SortOrder:            42 + i,
			SKUs: []productdomain.ProductSKU{{
				SKUCode:          "original-sku",
				PriceAmount:      money.FromDecimal(decimal.NewFromInt(29)),
				ManualStockTotal: 15,
				IsActive:         true,
			}},
		}
		if err := db.Create(&products[i]).Error; err != nil {
			t.Fatalf("seed product %d failed: %v", i, err)
		}
		products[i] = loadBatchImagesProduct(t, db, products[i].ID)
	}

	images := jsonslice.Strings{"/uploads/z-cover.png", "https://images.example.com/a-detail.png", "uploads/详情.webp", "/uploads/z-cover.png"}
	payload, err := json.Marshal(map[string]interface{}{
		"ids": []uint{products[1].ID, products[0].ID}, "images": images,
		"category_id": 0, "is_active": false, "sort_order": 0,
		"price_amount": "0", "slug": "overwritten", "tags": []string{"overwritten"},
	})
	if err != nil {
		t.Fatalf("encode request failed: %v", err)
	}
	resp := requestBatchProductImages(t, router, string(payload), "en-US")
	if resp.StatusCode != 0 || resp.Data.Total != 2 || resp.Data.SuccessCount != 2 || resp.Data.FailedItems == nil || len(resp.Data.FailedItems) != 0 {
		t.Fatalf("unexpected batch result: %+v", resp)
	}
	for i, before := range products {
		after := loadBatchImagesProduct(t, db, before.ID)
		if i < 2 {
			if !reflect.DeepEqual(after.Images, images) {
				t.Fatalf("product %d image order/content want %v, got %v", before.ID, images, after.Images)
			}
			after.Images = before.Images
			after.UpdatedAt = before.UpdatedAt
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("product %d fields changed unexpectedly:\nbefore=%+v\nafter=%+v", before.ID, before, after)
		}
	}
}

func TestBatchUpdateProductImagesReturnsPartialFailures(t *testing.T) {
	router, db := setupBatchProductImagesTest(t)
	products := []productdomain.Product{
		{Slug: "batch-images-first", Images: jsonslice.Strings{"/uploads/first.png"}},
		{Slug: "batch-images-deleted", Images: jsonslice.Strings{"/uploads/deleted.png"}},
		{Slug: "batch-images-blocked", Images: jsonslice.Strings{"/uploads/blocked.png"}},
		{Slug: "batch-images-last", Images: jsonslice.Strings{"/uploads/last.png"}},
	}
	for i := range products {
		products[i].TitleJSON = jsonmap.JSON{"en-US": products[i].Slug}
	}
	if err := db.Create(&products).Error; err != nil {
		t.Fatalf("seed products failed: %v", err)
	}
	if err := db.Model(&productdomain.Product{}).Where("id = ?", products[1].ID).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("soft delete product failed: %v", err)
	}
	trigger := fmt.Sprintf(`CREATE TRIGGER reject_batch_images BEFORE UPDATE OF images ON products
		WHEN NEW.id = %d BEGIN SELECT RAISE(FAIL, 'forced image update failure'); END`, products[2].ID)
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("create update failure trigger failed: %v", err)
	}
	for i := range products {
		products[i] = loadBatchImagesProduct(t, db, products[i].ID)
	}
	missingID := products[3].ID + 100
	for _, tc := range []struct {
		name         string
		ids          []uint
		successCount int
	}{
		{"partial failure", []uint{products[0].ID, missingID, products[1].ID, products[2].ID, products[3].ID}, 2},
		{"all failed", []uint{missingID, products[1].ID, products[2].ID}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]interface{}{"ids": tc.ids, "images": []string{"/uploads/new.png"}})
			if err != nil {
				t.Fatalf("encode request failed: %v", err)
			}
			resp := requestBatchProductImages(t, router, string(payload), "en-US")
			if resp.StatusCode != 0 || resp.Data.Total != len(tc.ids) || resp.Data.SuccessCount != tc.successCount || len(resp.Data.FailedItems) != 3 {
				t.Fatalf("unexpected batch result: %+v", resp)
			}
			for i, expected := range []struct {
				id   uint
				code string
			}{
				{missingID, "product_not_found"},
				{products[1].ID, "product_not_found"},
				{products[2].ID, "product_update_failed"},
			} {
				failure := resp.Data.FailedItems[i]
				if failure.ID != expected.id || failure.ErrorCode != expected.code || failure.Message != i18n.T("en-US", "error."+expected.code) {
					t.Fatalf("unexpected failed item %d: %+v", i, failure)
				}
			}
		})
	}
	for i, before := range products {
		after := loadBatchImagesProduct(t, db, before.ID)
		if i == 0 || i == 3 {
			if !reflect.DeepEqual(after.Images, jsonslice.Strings{"/uploads/new.png"}) {
				t.Fatalf("valid product %d was not updated: %v", after.ID, after.Images)
			}
			after.Images = before.Images
			after.UpdatedAt = before.UpdatedAt
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("product %d changed unexpectedly:\nbefore=%+v\nafter=%+v", before.ID, before, after)
		}
	}
}

func TestBatchUpdateProductImagesRejectsInvalidRequests(t *testing.T) {
	router, db := setupBatchProductImagesTest(t)
	product := productdomain.Product{
		Slug: "batch-images-invalid", TitleJSON: jsonmap.JSON{"en-US": "Original product"},
		Images: jsonslice.Strings{"/uploads/original.png"},
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product failed: %v", err)
	}
	before := loadBatchImagesProduct(t, db, product.ID)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"malformed JSON", `{"ids":[`},
		{"null body", `null`},
		{"array body", `[]`},
		{"missing ids", `{"images":["/uploads/new.png"]}`},
		{"null ids", `{"ids":null,"images":["/uploads/new.png"]}`},
		{"empty ids", `{"ids":[],"images":["/uploads/new.png"]}`},
		{"zero id", `{"ids":[$id,0],"images":["/uploads/new.png"]}`},
		{"negative id", `{"ids":[$id,-1],"images":["/uploads/new.png"]}`},
		{"fractional id", `{"ids":[$id,1.5],"images":["/uploads/new.png"]}`},
		{"string id", `{"ids":[$id,"1"],"images":["/uploads/new.png"]}`},
		{"null id", `{"ids":[$id,null],"images":["/uploads/new.png"]}`},
		{"overflow id", `{"ids":[$id,18446744073709551616],"images":["/uploads/new.png"]}`},
		{"invalid ids type", `{"ids":"1","images":["/uploads/new.png"]}`},
		{"missing images", `{"ids":[$id]}`},
		{"null images", `{"ids":[$id],"images":null}`},
		{"empty images", `{"ids":[$id],"images":[]}`},
		{"empty path", `{"ids":[$id],"images":[""]}`},
		{"blank path", `{"ids":[$id],"images":[" \t\n "]}`},
		{"invalid later path", `{"ids":[$id],"images":["/uploads/new.png",""]}`},
		{"blank later path", `{"ids":[$id],"images":["/uploads/new.png","  "]}`},
		{"null path", `{"ids":[$id],"images":[null]}`},
		{"number path", `{"ids":[$id],"images":[1]}`},
		{"invalid images type", `{"ids":[$id],"images":"/uploads/new.png"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.ReplaceAll(tc.body, "$id", fmt.Sprint(product.ID))
			resp := requestBatchProductImages(t, router, body, "en-US")
			if resp.StatusCode != http.StatusBadRequest || resp.Msg == "" || strings.Contains(resp.Msg, "validation.") || strings.Contains(resp.Msg, "error.") {
				t.Fatalf("expected localized bad request, got %+v", resp)
			}
			if after := loadBatchImagesProduct(t, db, product.ID); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid request mutated product: %+v", after)
			}
		})
	}
	for _, locale := range []string{"zh-CN", "zh-TW", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			body := fmt.Sprintf(`{"ids":[%d],"images":["  "]}`, product.ID)
			resp := requestBatchProductImages(t, router, body, locale)
			if resp.StatusCode != http.StatusBadRequest || resp.Msg != i18n.T(locale, "error.bad_request") {
				t.Fatalf("unexpected localized error: %+v", resp)
			}
		})
	}
}

func TestBatchUpdateProductImagesHandlesHundredProducts(t *testing.T) {
	router, db := setupBatchProductImagesTest(t)
	products := make([]productdomain.Product, 100)
	for i := range products {
		products[i] = productdomain.Product{
			Slug:      fmt.Sprintf("batch-images-hundred-%03d", i),
			TitleJSON: jsonmap.JSON{"en-US": fmt.Sprintf("Product %d", i)},
			Images:    jsonslice.Strings{fmt.Sprintf("/uploads/old-%d.png", i)},
		}
	}
	if err := db.Create(&products).Error; err != nil {
		t.Fatalf("seed hundred products failed: %v", err)
	}
	ids := make([]uint, len(products))
	for i := range products {
		ids[i] = products[i].ID
	}
	images := jsonslice.Strings{"/uploads/cover.png", "/uploads/detail.png"}
	payload, err := json.Marshal(map[string]interface{}{"ids": ids, "images": images})
	if err != nil {
		t.Fatalf("encode request failed: %v", err)
	}
	resp := requestBatchProductImages(t, router, string(payload), "en-US")
	if resp.StatusCode != 0 || resp.Data.Total != 100 || resp.Data.SuccessCount != 100 || resp.Data.FailedItems == nil || len(resp.Data.FailedItems) != 0 {
		t.Fatalf("unexpected hundred product batch result: %+v", resp)
	}
	for _, product := range products {
		if after := loadBatchImagesProduct(t, db, product.ID); !reflect.DeepEqual(after.Images, images) {
			t.Fatalf("product %d image order/content want %v, got %v", product.ID, images, after.Images)
		}
	}
}
