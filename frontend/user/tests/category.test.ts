import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildCategoryGroups,
  buildCategoryPath,
  createCategoryMap,
  flattenCategoryGroups,
  getCategoryAncestors,
  getRootCategory,
  type PublicCategory,
} from '../src/utils/category.ts'

const categories: PublicCategory[] = [
  { id: 1, parent_id: 0, name: '社交账号', slug: 'social' },
  { id: 2, parent_id: 1, name: 'Facebook', slug: 'facebook' },
  { id: 3, parent_id: 2, name: '老号', slug: 'facebook-old' },
  { id: 4, parent_id: 2, name: '新号', slug: 'facebook-new' },
  { id: 5, parent_id: 1, name: 'Instagram', slug: 'instagram' },
  { id: 6, parent_id: 0, name: '邮箱', slug: 'email' },
]
const label = (value: unknown) => String(value || '')

test('category tree preserves three levels, sibling order, and existing leaf categories', () => {
  const groups = buildCategoryGroups(categories)
  assert.deepEqual(groups.map((group) => group.id), [1, 6])
  assert.deepEqual(groups[0]!.children.map((group) => group.id), [2, 5])
  assert.deepEqual(groups[0]!.children[0]!.children.map((group) => group.id), [3, 4])
  assert.deepEqual(groups[0]!.children[1]!.children, [])
  assert.deepEqual(flattenCategoryGroups(groups).map((group) => group.id), [1, 2, 3, 4, 5, 6])
  assert.equal('children' in categories[0]!, false, 'building a tree must not mutate the API records')
})

test('category tree works when the API returns children before parents', () => {
  const groups = buildCategoryGroups([categories[2]!, categories[1]!, categories[0]!])
  assert.equal(groups[0]!.children[0]!.children[0]!.id, 3)
})

test('deep category links expose every ancestor in root-to-parent order', () => {
  const map = createCategoryMap(categories)
  assert.deepEqual(getCategoryAncestors(map.get(3), map).map((item) => item.id), [1, 2])
  assert.equal(buildCategoryPath(map.get(3), map, label), '社交账号 / Facebook / 老号')
  assert.equal(buildCategoryPath(map.get(2), map, label), '社交账号 / Facebook')
  assert.equal(buildCategoryPath(map.get(1), map, label), '社交账号')
})

test('homepage grouping resolves both second- and third-level products to the same root', () => {
  const map = createCategoryMap(categories)
  assert.equal(getRootCategory(map.get(3)!, map).id, 1)
  assert.equal(getRootCategory(map.get(5)!, map).id, 1)
  assert.equal(getRootCategory(map.get(6)!, map).id, 6)
})

test('a missing parent preserves the existing standalone category behavior', () => {
  const orphan = { id: 9, parent_id: 99, name: 'Standalone' }
  const map = createCategoryMap([orphan])
  assert.equal(buildCategoryGroups([orphan])[0]!.id, 9)
  assert.equal(buildCategoryPath(orphan, map, label), 'Standalone')
  assert.deepEqual(getCategoryAncestors(orphan, map), [])
})

test('ancestor traversal stops safely for a circular record without repeating the selected category', () => {
  const first = { id: 7, parent_id: 8, name: 'First' }
  const second = { id: 8, parent_id: 7, name: 'Second' }
  const map = createCategoryMap([first, second])
  assert.deepEqual(getCategoryAncestors(first, map).map((item) => item.id), [8])
})
