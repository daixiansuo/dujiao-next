package productapplication

import (
	"errors"
	"reflect"
	"testing"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"

	"github.com/dujiao-next/internal/constants"
	cardsecretcontract "github.com/dujiao-next/internal/modules/cardsecret/contract"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
)

type productRepositoryStub struct {
	storefront bool
	filter     productcontract.ListFilter
	rows       []productdomain.Product
	total      int64
	bySlug     *productdomain.Product
	byAdmin    *productdomain.Product
}

func (stub *productRepositoryStub) List(filter productcontract.ListFilter) ([]productdomain.Product, int64, error) {
	stub.storefront = false
	stub.filter = filter
	return stub.rows, stub.total, nil
}

func (stub *productRepositoryStub) ListForStorefront(filter productcontract.ListFilter) ([]productdomain.Product, int64, error) {
	stub.storefront = true
	stub.filter = filter
	return stub.rows, stub.total, nil
}

func (stub *productRepositoryStub) GetBySlug(string, bool) (*productdomain.Product, error) {
	return stub.bySlug, nil
}

func (stub *productRepositoryStub) GetAdminByID(string) (*productdomain.Product, error) {
	return stub.byAdmin, nil
}

type categoryRepositoryStub struct {
	byID map[string]*categorydomain.Category
	rows []categorydomain.Category
}

func (stub categoryRepositoryStub) GetByID(id string) (*categorydomain.Category, error) {
	return stub.byID[id], nil
}

func (stub categoryRepositoryStub) List() ([]categorydomain.Category, error) {
	return stub.rows, nil
}

type hiddenProductRepositoryStub struct {
	ids []uint
}

func (stub hiddenProductRepositoryStub) ListHiddenProductIDs(uint) ([]uint, error) {
	return stub.ids, nil
}

type stockCounterStub struct {
	counts []cardsecretcontract.SKUStockCount
}

func (stub stockCounterStub) CountStockByProductIDs([]uint) ([]cardsecretcontract.SKUStockCount, error) {
	return stub.counts, nil
}

func TestListPublicForTenantBuildsVisibilityFilterBeforePagination(t *testing.T) {
	products := &productRepositoryStub{total: 2}
	categories := categoryRepositoryStub{
		byID: map[string]*categorydomain.Category{
			"10": {ID: 10, IsActive: true},
		},
		rows: []categorydomain.Category{
			{ID: 10, IsActive: true},
			{ID: 11, ParentID: 10, IsActive: true},
			{ID: 12, ParentID: 10, IsActive: false},
		},
	}
	service := NewService(Options{Products: products, Categories: categories})
	resellerID := uint(9)

	_, total, err := service.ListPublicForTenant(
		resellercontract.TenantContext{ResellerID: &resellerID},
		hiddenProductRepositoryStub{ids: []uint{7, 8}},
		"10",
		"keyword",
		2,
		20,
	)
	if err != nil {
		t.Fatalf("ListPublicForTenant returned error: %v", err)
	}
	if products.storefront {
		t.Fatal("reseller list must retain the existing query")
	}
	if total != 2 {
		t.Fatalf("total want 2 got %d", total)
	}
	if !reflect.DeepEqual(products.filter.CategoryIDs, []uint{10, 11}) {
		t.Fatalf("category ids want [10 11] got %v", products.filter.CategoryIDs)
	}
	if !reflect.DeepEqual(products.filter.ExcludeProductIDs, []uint{7, 8}) {
		t.Fatalf("excluded product ids want [7 8] got %v", products.filter.ExcludeProductIDs)
	}
	if products.filter.Page != 2 || products.filter.PageSize != 20 || !products.filter.OnlyActive || !products.filter.WithCategory {
		t.Fatalf("unexpected public filter: %#v", products.filter)
	}
}

func TestQueryServiceUsesProductContractErrors(t *testing.T) {
	service := NewService(Options{
		Products: &productRepositoryStub{},
	})

	if _, err := service.GetPublicBySlug("missing"); !errors.Is(err, productcontract.ErrNotFound) {
		t.Fatalf("GetPublicBySlug want contract not-found error, got %v", err)
	}
	resellerID := uint(9)
	if _, _, err := service.ListPublicForTenant(resellercontract.TenantContext{ResellerID: &resellerID}, nil, "", "", 1, 20); !errors.Is(err, productcontract.ErrResellerProductNotListed) {
		t.Fatalf("ListPublicForTenant want contract not-listed error, got %v", err)
	}
}

func TestApplyAutoStockCountsAssignsLegacyStockToOneSKU(t *testing.T) {
	service := NewService(Options{Stock: stockCounterStub{counts: []cardsecretcontract.SKUStockCount{
		{ProductID: 30, SKUID: 0, Status: cardsecretdomain.StatusAvailable, Total: 2},
		{ProductID: 30, SKUID: 101, Status: cardsecretdomain.StatusAvailable, Total: 3},
		{ProductID: 30, SKUID: 102, Status: cardsecretdomain.StatusAvailable, Total: 4},
	}}})
	products := []productdomain.Product{{
		ID:              30,
		FulfillmentType: constants.FulfillmentTypeAuto,
		SKUs: []productdomain.ProductSKU{
			{ID: 102, SKUCode: "SECOND", IsActive: true},
			{ID: 101, SKUCode: productdomain.DefaultSKUCode, IsActive: true},
		},
	}}

	if err := service.ApplyAutoStockCounts(products); err != nil {
		t.Fatalf("ApplyAutoStockCounts returned error: %v", err)
	}
	if products[0].AutoStockAvailable != 9 {
		t.Fatalf("product available want 9 got %d", products[0].AutoStockAvailable)
	}
	if products[0].SKUs[0].AutoStockAvailable != 4 {
		t.Fatalf("secondary SKU available want 4 got %d", products[0].SKUs[0].AutoStockAvailable)
	}
	if products[0].SKUs[1].AutoStockAvailable != 5 {
		t.Fatalf("DEFAULT SKU available want 5 got %d", products[0].SKUs[1].AutoStockAvailable)
	}
}

func TestListPublicForMainTenantUsesStorefrontQuery(t *testing.T) {
	products := &productRepositoryStub{rows: []productdomain.Product{{ID: 1}}, total: 1}
	categories := categoryRepositoryStub{
		byID: map[string]*categorydomain.Category{"10": {ID: 10, IsActive: true}},
		rows: []categorydomain.Category{{ID: 10, IsActive: true}, {ID: 11, ParentID: 10, IsActive: true}},
	}
	service := NewService(Options{Products: products, Categories: categories})
	rows, total, err := service.ListPublicForTenant(resellercontract.TenantContext{IsMain: true}, nil, "10", "keyword", 2, 20)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != 1 {
		t.Fatalf("storefront query result: rows=%v total=%d err=%v", rows, total, err)
	}
	if !products.storefront {
		t.Fatal("main storefront must use the storefront query")
	}
	filter := products.filter
	if filter.CategoryID != "10" || !reflect.DeepEqual(filter.CategoryIDs, []uint{10, 11}) || filter.Search != "keyword" ||
		filter.Page != 2 || filter.PageSize != 20 || !filter.OnlyActive || !filter.WithCategory || len(filter.ExcludeProductIDs) != 0 {
		t.Fatalf("unexpected storefront filter: %#v", filter)
	}
}

func TestOtherProductQueriesRetainExistingList(t *testing.T) {
	for _, name := range []string{"public", "exact", "upstream", "admin"} {
		t.Run(name, func(t *testing.T) {
			products := &productRepositoryStub{}
			service := NewService(Options{Products: products})
			var err error
			switch name {
			case "public":
				_, _, err = service.ListPublic("", "", 1, 20)
			case "exact":
				_, _, err = service.ListPublicExact("", 1, 20)
			case "upstream":
				_, _, err = service.ListForUpstreamSync(nil, true, 1, 20)
			case "admin":
				_, _, err = service.ListAdmin("", "", "", "", nil, nil, 0, 1, 20)
			}
			if err != nil {
				t.Fatal(err)
			}
			if products.storefront || products.filter.PageSize != 20 {
				t.Fatalf("%s must use the original list query, got %#v", name, products)
			}
		})
	}
}
