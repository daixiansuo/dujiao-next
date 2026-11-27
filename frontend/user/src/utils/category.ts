export interface PublicCategory {
  id: number
  slug?: string
  name?: unknown
  icon?: string | null
  parent_id?: number | null
}

export interface CategoryGroup extends PublicCategory {
  children: CategoryGroup[]
}

const normalizeCategoryId = (value: unknown) => {
  const numberValue = Number(value)
  if (!Number.isFinite(numberValue) || numberValue <= 0) return 0
  return Math.floor(numberValue)
}

export const normalizeCategoryParentId = (value: unknown) => {
  return normalizeCategoryId(value)
}

export const createCategoryMap = (categories: PublicCategory[]) => {
  const categoryMap = new Map<number, PublicCategory>()

  categories.forEach((category) => {
    const categoryId = normalizeCategoryId(category.id)
    if (categoryId > 0) {
      categoryMap.set(categoryId, {
        ...category,
        id: categoryId,
        parent_id: normalizeCategoryParentId(category.parent_id),
      })
    }
  })

  return categoryMap
}

export const buildCategoryGroups = (categories: PublicCategory[]): CategoryGroup[] => {
  const categoryMap = createCategoryMap(categories)
  const nodes = new Map<number, CategoryGroup>()
  for (const category of categoryMap.values()) {
    nodes.set(category.id, { ...category, children: [] })
  }

  const roots: CategoryGroup[] = []
  for (const category of nodes.values()) {
    const parent = nodes.get(normalizeCategoryParentId(category.parent_id))
    if (!parent || parent.id === category.id) {
      roots.push(category)
    } else {
      parent.children.push(category)
    }
  }
  return roots
}

export const flattenCategoryGroups = (groups: CategoryGroup[]): CategoryGroup[] => {
  return groups.flatMap((group) => [group, ...flattenCategoryGroups(group.children)])
}

/** Ancestors ordered from the root to the immediate parent. */
export const getCategoryAncestors = (
  category: PublicCategory | undefined,
  categoryMap: Map<number, PublicCategory>,
): PublicCategory[] => {
  const ancestors: PublicCategory[] = []
  const visited = new Set<number>(category ? [category.id] : [])
  let parentId = normalizeCategoryParentId(category?.parent_id)
  while (parentId > 0 && !visited.has(parentId)) {
    const parent = categoryMap.get(parentId)
    if (!parent) break
    visited.add(parentId)
    ancestors.unshift(parent)
    parentId = normalizeCategoryParentId(parent.parent_id)
  }
  return ancestors
}

export const getRootCategory = (
  category: PublicCategory,
  categoryMap: Map<number, PublicCategory>,
): PublicCategory => getCategoryAncestors(category, categoryMap)[0] || category

/** 根据唯一选中路径生成逐级筛选行；返回父类后，旧分支的选择和下级行自然清除。 */
export const buildCategoryFilterRows = (groups: CategoryGroup[], selectedId: number | null) => {
  const nodes = new Map(flattenCategoryGroups(groups).map(category => [category.id, category]))
  const selected = selectedId === null ? undefined : nodes.get(selectedId)
  if (!selected) return []

  const path = [...getCategoryAncestors(selected, nodes), selected]
  return path.flatMap((category, index) => {
    const parent = nodes.get(category.id)!
    return parent.children.length ? [{ parent, selectedId: path[index + 1]?.id ?? null }] : []
  })
}

export const buildCategoryPath = (
  category: PublicCategory | undefined,
  categoryMap: Map<number, PublicCategory>,
  getLabel: (value: unknown) => string,
) => {
  if (!category) return ''
  return [...getCategoryAncestors(category, categoryMap), category]
    .map((item) => getLabel(item.name))
    .filter(Boolean)
    .join(' / ')
}
