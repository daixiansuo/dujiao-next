package categoryintegrationtest

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	categoryapp "github.com/dujiao-next/internal/modules/catalog/category/application"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	categorygormstore "github.com/dujiao-next/internal/modules/catalog/category/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func newCategoryServiceForTest(t *testing.T) (*categoryapp.Service, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:category_service_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}); err != nil {
		t.Fatalf("auto migrate category/product failed: %v", err)
	}

	return categoryapp.NewService(categorygormstore.NewCategoryStore(db)), db
}

func createCategoryFixture(t *testing.T, db *gorm.DB, slug string, parentID uint) categorydomain.Category {
	t.Helper()

	category := categorydomain.Category{
		ParentID: parentID,
		Slug:     slug,
		NameJSON: jsonmap.JSON{
			"zh-CN": slug,
		},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category fixture failed: %v", err)
	}
	return category
}

func createProductFixture(t *testing.T, db *gorm.DB, categoryID uint, slug string) {
	t.Helper()

	product := productdomain.Product{
		CategoryID:  categoryID,
		Slug:        slug,
		TitleJSON:   jsonmap.JSON{"zh-CN": slug},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product fixture failed: %v", err)
	}
}

func TestCategoryServiceCreateSupportsSecondLevelCategory(t *testing.T) {
	svc, db := newCategoryServiceForTest(t)
	parent := createCategoryFixture(t, db, "games", 0)

	category, err := svc.Create(categoryapp.UpsertInput{
		ParentID: parent.ID,
		Slug:     "steam",
		NameJSON: map[string]interface{}{
			"zh-CN": "Steam",
		},
	})
	if err != nil {
		t.Fatalf("create second-level category failed: %v", err)
	}
	if category.ParentID != parent.ID {
		t.Fatalf("expected parent_id=%d, got %d", parent.ID, category.ParentID)
	}
}

func TestCategoryServiceCreateSupportsThirdLevelAndRejectsFourthLevel(t *testing.T) {
	svc, db := newCategoryServiceForTest(t)
	parent := createCategoryFixture(t, db, "games", 0)
	child := createCategoryFixture(t, db, "steam", parent.ID)
	createProductFixture(t, db, child.ID, "legacy-second-level-product")

	_, err := svc.Create(categoryapp.UpsertInput{
		ParentID: 9999,
		Slug:     "missing-parent",
		NameJSON: map[string]interface{}{"zh-CN": "missing-parent"},
	})
	if err != categoryapp.ErrParentInvalid {
		t.Fatalf("expected ErrCategoryParentInvalid for missing parent, got %v", err)
	}

	third, err := svc.Create(categoryapp.UpsertInput{
		ParentID: child.ID,
		Slug:     "steam-gift-card",
		NameJSON: map[string]interface{}{"zh-CN": "steam-gift-card"},
	})
	if err != nil || third.ParentID != child.ID {
		t.Fatalf("create third-level category: category=%+v err=%v", third, err)
	}
	var legacyProduct productdomain.Product
	if err := db.First(&legacyProduct).Error; err != nil || legacyProduct.CategoryID != child.ID {
		t.Fatalf("adding a child must retain legacy product assignment: product=%+v err=%v", legacyProduct, err)
	}

	_, err = svc.Create(categoryapp.UpsertInput{
		ParentID: third.ID,
		Slug:     "fourth-level",
		NameJSON: map[string]interface{}{"zh-CN": "fourth-level"},
	})
	if err != categoryapp.ErrParentInvalid {
		t.Fatalf("expected ErrCategoryParentInvalid for fourth-level category, got %v", err)
	}
}

func TestCategoryServiceUpdateRejectsInvalidParentAssignment(t *testing.T) {
	svc, db := newCategoryServiceForTest(t)
	rootA := createCategoryFixture(t, db, "games", 0)
	rootB := createCategoryFixture(t, db, "cards", 0)
	child := createCategoryFixture(t, db, "steam", rootA.ID)
	_ = createCategoryFixture(t, db, "steam-gift-card", child.ID)

	_, err := svc.Update(fmt.Sprintf("%d", rootA.ID), categoryapp.UpsertInput{
		ParentID: rootA.ID,
		Slug:     rootA.Slug,
		NameJSON: map[string]interface{}{"zh-CN": rootA.Slug},
	})
	if err != categoryapp.ErrParentInvalid {
		t.Fatalf("expected ErrCategoryParentInvalid for self parent, got %v", err)
	}

	_, err = svc.Update(fmt.Sprintf("%d", rootA.ID), categoryapp.UpsertInput{
		ParentID: rootB.ID,
		Slug:     rootA.Slug,
		NameJSON: map[string]interface{}{"zh-CN": rootA.Slug},
	})
	if err != categoryapp.ErrParentInvalid {
		t.Fatalf("expected ErrCategoryParentInvalid when moving a branch beyond three levels, got %v", err)
	}
	_, err = svc.Update(fmt.Sprintf("%d", rootA.ID), categoryapp.UpsertInput{
		ParentID: child.ID,
		Slug:     rootA.Slug,
		NameJSON: map[string]interface{}{"zh-CN": rootA.Slug},
	})
	if err != categoryapp.ErrParentInvalid {
		t.Fatalf("expected ErrCategoryParentInvalid for descendant parent, got %v", err)
	}
}

func TestCategoryServiceUpdateMovesTwoLevelBranchAndPromotesLeaf(t *testing.T) {
	svc, db := newCategoryServiceForTest(t)
	root := createCategoryFixture(t, db, "games", 0)
	child := createCategoryFixture(t, db, "steam", root.ID)
	destination := createCategoryFixture(t, db, "digital", 0)

	moved, err := svc.Update(fmt.Sprint(root.ID), categoryapp.UpsertInput{
		ParentID: destination.ID,
		Slug:     root.Slug,
		NameJSON: map[string]interface{}{"zh-CN": root.Slug},
	})
	if err != nil || moved.ParentID != destination.ID {
		t.Fatalf("move two-level branch within three levels: category=%+v err=%v", moved, err)
	}
	promoted, err := svc.Update(fmt.Sprint(child.ID), categoryapp.UpsertInput{
		Slug:     child.Slug,
		NameJSON: map[string]interface{}{"zh-CN": child.Slug},
	})
	if err != nil || promoted.ParentID != 0 {
		t.Fatalf("promote third-level leaf to root: category=%+v err=%v", promoted, err)
	}
}

func TestCategoryServiceDeleteRejectsCategoriesWithChildrenOrProducts(t *testing.T) {
	svc, db := newCategoryServiceForTest(t)
	parent := createCategoryFixture(t, db, "games", 0)
	child := createCategoryFixture(t, db, "steam", parent.ID)

	if err := svc.Delete(fmt.Sprintf("%d", parent.ID)); err != categoryapp.ErrInUse {
		t.Fatalf("expected ErrCategoryInUse for category with children, got %v", err)
	}

	createProductFixture(t, db, child.ID, "steam-product")
	if err := svc.Delete(fmt.Sprintf("%d", child.ID)); err != categoryapp.ErrInUse {
		t.Fatalf("expected ErrCategoryInUse for category with products, got %v", err)
	}
}

func TestCategoryServiceListSortOrderDescending(t *testing.T) {
	svc, db := newCategoryServiceForTest(t)

	high := categorydomain.Category{
		Slug:      "high",
		NameJSON:  jsonmap.JSON{"zh-CN": "high"},
		SortOrder: 100,
	}
	low := categorydomain.Category{
		Slug:      "low",
		NameJSON:  jsonmap.JSON{"zh-CN": "low"},
		SortOrder: 1,
	}
	if err := db.Create(&high).Error; err != nil {
		t.Fatalf("create high sort category failed: %v", err)
	}
	if err := db.Create(&low).Error; err != nil {
		t.Fatalf("create low sort category failed: %v", err)
	}

	rows, err := svc.List()
	if err != nil {
		t.Fatalf("list categories failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(rows))
	}
	if rows[0].Slug != "high" || rows[1].Slug != "low" {
		t.Fatalf("expected high sort_order first, got %s then %s", rows[0].Slug, rows[1].Slug)
	}
}

func TestCategoryTreeHelpersHandleMissingIDsAndCycles(t *testing.T) {
	if got := categorydomain.DescendantIDs(nil, 42); !reflect.DeepEqual(got, []uint{42}) {
		t.Fatalf("missing category must retain its query ID, got %v", got)
	}
	if got := categorydomain.DescendantIDs(nil, 0); len(got) != 0 {
		t.Fatalf("empty category filter must remain empty, got %v", got)
	}
	cycle := []categorydomain.Category{{ID: 1, ParentID: 3}, {ID: 2, ParentID: 1}, {ID: 3, ParentID: 2}}
	if got := categorydomain.DescendantIDs(cycle, 1); !reflect.DeepEqual(got, []uint{1, 2, 3}) {
		t.Fatalf("query traversal must terminate and avoid duplicate IDs, got %v", got)
	}
	if categorydomain.CanSetParent(cycle, 0, 1) || categorydomain.CanSetParent(cycle, 1, 0) {
		t.Fatal("cyclic branch must not be accepted as a parent or moved intact")
	}
}
