package integrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	catalogproductbootstrap "github.com/dujiao-next/internal/bootstrap/catalogproduct"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	categorygormstore "github.com/dujiao-next/internal/modules/catalog/category/infrastructure/gormstore"
	productapplication "github.com/dujiao-next/internal/modules/catalog/product/application"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"

	promotiondomain "github.com/dujiao-next/internal/modules/promotion/domain"

	"github.com/dujiao-next/internal/constants"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	producthttp "github.com/dujiao-next/internal/modules/catalog/product/transport/http"
	productpresenter "github.com/dujiao-next/internal/modules/catalog/product/transport/presenter"
	contentcontract "github.com/dujiao-next/internal/modules/content/contract"
	promotionapp "github.com/dujiao-next/internal/modules/promotion/application"
	promotiongormstore "github.com/dujiao-next/internal/modules/promotion/infrastructure/gormstore"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type staticPublicProductQueries struct {
	product productdomain.Product
}

func (s staticPublicProductQueries) ListPublicForTenant(reseller.TenantContext, string, string, int, int) ([]productdomain.Product, int64, error) {
	return []productdomain.Product{s.product}, 1, nil
}

func (s staticPublicProductQueries) GetPublicBySlugForTenant(reseller.TenantContext, string) (*productdomain.Product, error) {
	product := s.product
	return &product, nil
}

func (staticPublicProductQueries) ApplyAutoStockCounts([]productdomain.Product) error {
	return nil
}

type emptyRelatedPostReader struct{}

func (emptyRelatedPostReader) ListPostsForProduct(context.Context, uint, int) ([]contentcontract.RelatedPost, error) {
	return nil, nil
}

func TestPublicProductHTTPPromotionUsesDisplayPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:catalog_public_product_display_price_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&promotiondomain.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	promotion := promotiondomain.Promotion{
		Name:       "fixed-10",
		ScopeType:  constants.ScopeTypeProduct,
		ScopeRefID: 1,
		Type:       constants.PromotionTypeFixed,
		Value:      money.FromDecimal(decimal.NewFromInt(10)),
		MinAmount:  money.FromDecimal(decimal.Zero),
		IsActive:   true,
	}
	if err := db.Create(&promotion).Error; err != nil {
		t.Fatalf("create promotion failed: %v", err)
	}

	queries := staticPublicProductQueries{product: productdomain.Product{
		ID:          1,
		Slug:        "display-price",
		PriceAmount: money.FromDecimal(decimal.RequireFromString("59.90")),
		SKUs: []productdomain.ProductSKU{
			{
				ID:          21,
				IsActive:    true,
				SortOrder:   100,
				PriceAmount: money.FromDecimal(decimal.RequireFromString("89.90")),
			},
			{
				ID:          22,
				IsActive:    true,
				SortOrder:   10,
				PriceAmount: money.FromDecimal(decimal.RequireFromString("49.90")),
			},
		},
	}}
	promotions := promotionapp.NewService(promotiongormstore.New(db))
	handler := producthttp.NewPublicHandler(queries, nil, promotions, nil, nil, nil, emptyRelatedPostReader{})

	router := gin.New()
	producthttp.RegisterPublicRoutes(router, handler)
	request := httptest.NewRequest(http.MethodGet, "/products/display-price", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data productpresenter.Product `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode product response: %v", err)
	}
	if envelope.Data.PromotionPriceAmount == nil {
		t.Fatal("expected promotion price amount")
	}

	expectedDisplay := decimal.RequireFromString("89.90")
	expectedPromotion := decimal.RequireFromString("79.90")
	if !envelope.Data.PriceAmount.Decimal.Equal(expectedDisplay) {
		t.Fatalf("expected display price %s, got %s", expectedDisplay, envelope.Data.PriceAmount.String())
	}
	if !envelope.Data.PromotionPriceAmount.Decimal.Equal(expectedPromotion) {
		t.Fatalf("expected promotion display price %s, got %s", expectedPromotion, envelope.Data.PromotionPriceAmount.String())
	}
}

func TestPublicProductListUsesStorefrontOrderWithRealQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:storefront_http_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}); err != nil {
		t.Fatal(err)
	}
	parent := categorydomain.Category{Slug: "parent", NameJSON: jsonmap.JSON{"zh-CN": "parent"}, IsActive: true}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	categories := []categorydomain.Category{
		{Slug: "child", ParentID: parent.ID, NameJSON: jsonmap.JSON{"zh-CN": "child"}, IsActive: true},
		{Slug: "other", NameJSON: jsonmap.JSON{"zh-CN": "other"}, IsActive: true},
		{Slug: "inactive", NameJSON: jsonmap.JSON{"zh-CN": "inactive"}, IsActive: true},
	}
	if err := db.Create(&categories).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&categories[2]).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	products := []productdomain.Product{
		{Slug: "expensive", CategoryID: parent.ID, PriceAmount: money.FromDecimal(decimal.NewFromInt(20)), IsActive: true},
		{Slug: "cheap", CategoryID: categories[0].ID, PriceAmount: money.FromDecimal(decimal.NewFromInt(5)), IsActive: true,
			SKUs: []productdomain.ProductSKU{{SKUCode: "MATCH-SKU", PriceAmount: money.FromDecimal(decimal.NewFromInt(5)), IsActive: true}}},
		{Slug: "middle", CategoryID: categories[1].ID, PriceAmount: money.FromDecimal(decimal.NewFromInt(10)), IsActive: true},
		{Slug: "pinned", CategoryID: parent.ID, PriceAmount: money.FromDecimal(decimal.NewFromInt(99)), SortOrder: 100, IsActive: true},
		{Slug: "multi", CategoryID: categories[0].ID, PriceAmount: money.FromDecimal(decimal.NewFromInt(1)), IsActive: true,
			SKUs: []productdomain.ProductSKU{
				{SKUCode: "FIRST", PriceAmount: money.FromDecimal(decimal.NewFromInt(30)), SortOrder: 100, IsActive: true},
				{SKUCode: "MIN", PriceAmount: money.FromDecimal(decimal.NewFromInt(1)), SortOrder: 10, IsActive: true},
			}},
		{Slug: "inactive-product", CategoryID: parent.ID, IsActive: false},
		{Slug: "deleted-product", CategoryID: parent.ID, IsActive: true, DeletedAt: &created},
		{Slug: "inactive-category-product", CategoryID: categories[2].ID, IsActive: true},
	}
	for i := range products {
		products[i].TitleJSON = jsonmap.JSON{"zh-CN": products[i].Slug}
		products[i].CreatedAt = created.Add(time.Duration(i) * time.Minute)
	}
	if err := db.Create(&products).Error; err != nil {
		t.Fatal(err)
	}
	service := productapplication.NewService(productapplication.Options{
		Products: productgormstore.NewProductStore(db), Categories: categorygormstore.NewCategoryStore(db),
	})
	handler := catalogproductbootstrap.NewPublicHTTP(catalogproductbootstrap.PublicHTTPDependencies{Products: service, RelatedPosts: emptyRelatedPostReader{}})
	router := gin.New()
	producthttp.RegisterPublicRoutes(router, handler)
	for _, test := range []struct {
		query string
		want  []string
		total int64
	}{
		{"?page=1&page_size=20", []string{"pinned", "cheap", "middle", "expensive", "multi"}, 5},
		{fmt.Sprintf("?category_id=%d", parent.ID), []string{"pinned", "cheap", "expensive", "multi"}, 4},
		{fmt.Sprintf("?category_id=%d", categories[0].ID), []string{"cheap", "multi"}, 2},
		{"?page=2&page_size=2", []string{"middle", "expensive"}, 5},
		{"?search=MATCH-SKU", []string{"cheap"}, 1},
		{"?search=not-found", []string{}, 0},
	} {
		t.Run(test.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/products"+test.query, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			var envelope struct {
				Data       []productpresenter.Product `json:"data"`
				Pagination struct {
					Total int64 `json:"total"`
				} `json:"pagination"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(envelope.Data))
			for _, product := range envelope.Data {
				got = append(got, product.Slug)
				if product.Slug == "multi" && product.PriceAmount.String() != "30.00" {
					t.Fatalf("multi SKU display price want 30.00 got %s", product.PriceAmount.String())
				}
			}
			if !reflect.DeepEqual(got, test.want) || envelope.Pagination.Total != test.total {
				t.Fatalf("want %v total=%d got %v total=%d body=%s", test.want, test.total, got, envelope.Pagination.Total, recorder.Body.String())
			}
		})
	}
}
