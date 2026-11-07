import { describe, expect, it } from 'vitest'
import type { AdminCategory } from '@/api/types'
import {
  buildAdminCategoryPath, createAdminCategoryChildCountMap, createAdminCategoryMap,
  flattenAdminCategories, getAdminCategoryParentOptions, isAdminProductCategorySelectable,
} from './category'

const category = (id: number, parent_id: number, name: string): AdminCategory => ({
  id, parent_id, name: { 'zh-CN': name }, slug: `category-${id}`,
  icon: '', sort_order: 0, is_active: true, created_at: '',
})
const categories = [
  category(1, 0, '社交账号'), category(2, 1, 'Facebook'), category(3, 2, '老号'),
  category(4, 0, '邮箱'), category(5, 4, 'Outlook'),
]

describe('three-level product categories', () => {
  it('keeps third-level depth and full paths while preserving sibling order', () => {
    const items = flattenAdminCategories(categories)
    expect(items.map(item => [item.category.id, item.depth])).toEqual([[1, 0], [2, 1], [3, 2], [4, 0], [5, 1]])
    expect(buildAdminCategoryPath(categories[2], createAdminCategoryMap(categories), item => item.name['zh-CN']!))
      .toBe('社交账号 / Facebook / 老号')
  })

  it('offers parents based on the entire moving subtree and excludes descendants', () => {
    const ids = (id = 0) => getAdminCategoryParentOptions(categories, id).map(item => item.category.id)
    expect(ids()).toEqual([1, 2, 4, 5])
    expect(ids(1)).toEqual([]) // Moving the complete three-level tree would create a fourth level.
    expect(ids(2)).toEqual([1, 4]) // Its child must still fit after moving.
    expect(ids(3)).toEqual([1, 2, 4, 5])
  })

  it('allows products on leaves at any level without treating all second-level categories as parents', () => {
    const counts = createAdminCategoryChildCountMap(categories)
    expect(categories.map(item => isAdminProductCategorySelectable(item, counts))).toEqual([false, false, true, false, true])
    const standalone = category(6, 0, '独立分类')
    expect(isAdminProductCategorySelectable(standalone, counts)).toBe(true)
  })
})
