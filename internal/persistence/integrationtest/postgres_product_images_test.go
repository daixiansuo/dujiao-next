//go:build integration
// +build integration

package integrationtest

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/dujiao-next/internal/constants"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/jsonslice"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

func TestPostgresProductQuickUpdateImages(t *testing.T) {
	db := setupPostgresIntegrationDB(t)

	category := &categorydomain.Category{
		Slug:     "pg-product-images-category",
		NameJSON: jsonmap.JSON{"zh-CN": "商品图片分类"},
	}
	if err := db.Create(category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	productRepo := productgormstore.NewProductStore(db)
	loadProduct := func(t *testing.T, id string) *productdomain.Product {
		t.Helper()
		product, err := productRepo.GetByID(id)
		if err != nil {
			t.Fatalf("reload postgres product %s failed: %v", id, err)
		}
		if product == nil {
			t.Fatalf("postgres product %s not found", id)
		}
		return product
	}

	// 前两个商品模拟 batchQuickUpdate 逐个更新，第三个商品验证更新范围。
	before := make([]*productdomain.Product, 3)
	ids := make([]string, len(before))
	for index := range before {
		suffix := strconv.Itoa(index)
		product := &productdomain.Product{
			CategoryID:      category.ID,
			Slug:            "pg-product-images-" + suffix,
			TitleJSON:       jsonmap.JSON{"zh-CN": "商品图片 " + suffix, "en-US": "Product images " + suffix},
			DescriptionJSON: jsonmap.JSON{"zh-CN": "保留商品描述"},
			ContentJSON:     jsonmap.JSON{"en-US": "Keep product content"},
			PriceAmount:     money.FromDecimal(decimal.NewFromInt(99 + int64(index))),
			CostPriceAmount: money.FromDecimal(decimal.NewFromInt(20)),
			WholesalePrices: productdomain.WholesalePriceTiers{
				{MinQuantity: 5, UnitPrice: money.FromDecimal(decimal.NewFromInt(80))},
			},
			Images:              jsonslice.Strings{"/uploads/pg-old-" + suffix + "-cover.png", "/uploads/pg-old-" + suffix + "-detail.png"},
			Tags:                jsonslice.Strings{"keep-tag-" + suffix, "保留标签"},
			PurchaseType:        constants.ProductPurchaseMember,
			MinPurchaseQuantity: 2,
			MaxPurchaseQuantity: 8,
			StockDisplayMode:    constants.ProductStockDisplayHidden,
			FulfillmentType:     constants.FulfillmentTypeManual,
			ManualStockTotal:    12 + index,
			ManualStockLocked:   2,
			ManualStockSold:     3,
			IsAffiliateEnabled:  true,
			IsActive:            index != 1,
			SortOrder:           30 + index,
		}
		if err := productRepo.Create(product); err != nil {
			t.Fatalf("create postgres product %q failed: %v", product.Slug, err)
		}
		ids[index] = strconv.FormatUint(uint64(product.ID), 10)
		before[index] = loadProduct(t, ids[index])
		if !reflect.DeepEqual(before[index].Images, product.Images) {
			t.Fatalf("initial postgres product %s images want %q got %q", ids[index], product.Images, before[index].Images)
		}
	}

	for _, step := range []struct {
		name   string
		images jsonslice.Strings
	}{
		{
			name: "replace_images_in_order",
			images: jsonslice.Strings{
				"/uploads/pg-new-z-cover.webp",
				"https://images.example.test/商品/a-detail.png?size=800&format=webp",
			},
		},
		{
			name: "replace_with_reordered_images",
			images: jsonslice.Strings{
				"https://images.example.test/商品/a-detail.png?size=800&format=webp",
				"/uploads/pg-new-z-cover.webp",
			},
		},
		{
			name:   "replace_with_shorter_images",
			images: jsonslice.Strings{"/uploads/pg-replacement.webp"},
		},
		{
			name:   "repeat_same_images_without_appending",
			images: jsonslice.Strings{"/uploads/pg-replacement.webp"},
		},
	} {
		t.Run(step.name, func(t *testing.T) {
			fields := map[string]interface{}{"images": step.images}
			for _, id := range ids[:2] {
				if err := productRepo.QuickUpdate(id, fields); err != nil {
					t.Fatalf("quick update postgres product %s images failed: %v", id, err)
				}
			}

			for index, id := range ids {
				got := loadProduct(t, id)
				want := *before[index]
				if index < 2 {
					want.Images = step.images
					// QuickUpdate 允许刷新更新时间，其他字段必须保持不变。
					want.UpdatedAt = got.UpdatedAt
				}
				if !reflect.DeepEqual(got.Images, want.Images) {
					t.Fatalf("postgres product %s images want %q got %q", id, want.Images, got.Images)
				}
				if !reflect.DeepEqual(*got, want) {
					t.Fatalf("postgres product %s fields changed unexpectedly: want %+v got %+v", id, want, *got)
				}
			}
		})
	}
}
