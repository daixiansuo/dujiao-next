package categorydomain

import (
	"time"

	"github.com/dujiao-next/internal/shared/jsonmap"
)

// Category 分类表
type Category struct {
	ID        uint         `gorm:"primarykey" json:"id"`                         // 主键
	ParentID  uint         `gorm:"not null;default:0;index" json:"parent_id"`    // 父分类ID，0 表示一级分类
	Slug      string       `gorm:"uniqueIndex;not null" json:"slug"`             // 唯一标识
	NameJSON  jsonmap.JSON `gorm:"type:json;not null" json:"name"`               // 多语言名称
	Icon      string       `gorm:"type:varchar(500)" json:"icon"`                // 分类图标（图片路径）
	SortOrder int          `gorm:"default:0;index" json:"sort_order"`            // 排序权重
	IsActive  bool         `gorm:"not null;default:true;index" json:"is_active"` // 是否启用，停用后前台不展示
	CreatedAt time.Time    `gorm:"index" json:"created_at"`                      // 创建时间
	DeletedAt *time.Time   `gorm:"index" json:"-"`                               // 软删除时间
}

// TableName 指定表名
func (Category) TableName() string {
	return "categories"
}

const MaxDepth = 3

// DescendantIDs returns the category and its descendants, preserving sibling order.
// Missing categories retain their ID so a query still matches no unrelated rows.
func DescendantIDs(categories []Category, categoryID uint) []uint {
	if categoryID == 0 {
		return nil
	}
	children := make(map[uint][]uint, len(categories))
	for _, category := range categories {
		children[category.ParentID] = append(children[category.ParentID], category.ID)
	}
	ids := []uint{categoryID}
	visited := map[uint]bool{categoryID: true}
	for index := 0; index < len(ids); index++ {
		for _, childID := range children[ids[index]] {
			if !visited[childID] {
				visited[childID] = true
				ids = append(ids, childID)
			}
		}
	}
	return ids
}

// CanSetParent checks the depth of the whole moved branch. categoryID=0 creates a leaf.
func CanSetParent(categories []Category, categoryID, parentID uint) bool {
	parents := make(map[uint]uint, len(categories))
	children := make(map[uint][]uint, len(categories))
	for _, category := range categories {
		parents[category.ID] = category.ParentID
		children[category.ParentID] = append(children[category.ParentID], category.ID)
	}

	parentDepth := 0
	visited := make(map[uint]bool)
	for id := parentID; id != 0; {
		if id == categoryID || visited[id] {
			return false
		}
		next, exists := parents[id]
		if !exists {
			return false
		}
		visited[id] = true
		parentDepth++
		id = next
	}
	if parentDepth >= MaxDepth {
		return false
	}
	if categoryID == 0 {
		return true
	}

	branch := []uint{categoryID}
	visited = map[uint]bool{categoryID: true}
	for depth := 1; len(branch) > 0; depth++ {
		if parentDepth+depth > MaxDepth {
			return false
		}
		var next []uint
		for _, id := range branch {
			for _, childID := range children[id] {
				if visited[childID] {
					return false
				}
				visited[childID] = true
				next = append(next, childID)
			}
		}
		branch = next
	}
	return true
}
