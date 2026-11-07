import { setImmediate } from 'node:timers'
import { afterEach, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { adminAPI } from '@/api/admin'
import i18n from '@/i18n'
import ProductMappings from './ProductMappings.vue'

let app: App | undefined
const flush = async () => {
  await new Promise<void>(done => setImmediate(done))
  await nextTick()
}
const selectOption = async (label: string, trigger = document.querySelectorAll<HTMLElement>('[role="combobox"]')[1]!) => {
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flush()
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')]
    .find(el => el.textContent?.trim() === label)
  expect(option, label).toBeTruthy()
  option!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flush()
}

afterEach(() => {
  app?.unmount()
  app = undefined
  document.body.replaceChildren()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('filters mappings by local category and resets pagination when selecting or clearing it', async () => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  i18n.global.locale.value = 'zh-CN'
  vi.spyOn(adminAPI, 'getSiteConnections').mockResolvedValue({ data: { data: [] } })
  vi.spyOn(adminAPI, 'getCategories').mockResolvedValue({ data: { data: [
    { id: 10, parent_id: 0, name: { 'zh-CN': '账号' } },
    { id: 11, parent_id: 10, name: { 'zh-CN': '社交账号' } },
    { id: 12, parent_id: 11, name: { 'zh-CN': 'Facebook' } },
  ] } })
  const list = vi.spyOn(adminAPI, 'getProductMappings').mockImplementation(async (params) => ({
    data: { data: [], pagination: { page: params?.page, page_size: 20, total: 40, total_page: 2 } },
  }))
  const container = document.createElement('div')
  document.body.append(container)
  app = createApp(ProductMappings)
  app.use(i18n).mount(container)
  await flush()
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, category_id: undefined }))

  const nextPage = [...document.querySelectorAll<HTMLButtonElement>('button')]
    .find(el => el.textContent?.trim() === '下一页')!
  nextPage.click()
  await flush()
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))

  await selectOption('账号 / 社交账号')
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, category_id: '11' }))
  nextPage.click()
  await flush()
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, category_id: '11' }))

  await selectOption('账号 / 社交账号 / Facebook')
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, category_id: '12' }))
  await selectOption('账号')
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, category_id: '10' }))

  await selectOption('全部分类')
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, category_id: undefined }))
})

it.each([false, true])('imports checked products with automatic categories and ignores a hidden manual category (fallback=%s)', async fallback => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  i18n.global.locale.value = 'zh-CN'
  vi.spyOn(adminAPI, 'getSiteConnections').mockResolvedValue({ data: { data: [{ id: 1, name: 'CEOACC' }] } })
  vi.spyOn(adminAPI, 'getCategories').mockResolvedValue({ data: { data: [
    { id: 10, parent_id: 0, name: { 'zh-CN': '账号' } },
    { id: 11, parent_id: 10, name: { 'zh-CN': 'Facebook' } },
    { id: 12, parent_id: 11, name: { 'zh-CN': '老号' } },
  ] } })
  vi.spyOn(adminAPI, 'getProductMappings').mockResolvedValue({ data: { data: [] } })
  vi.spyOn(adminAPI, 'getUpstreamProducts').mockResolvedValue({ data: { data: {
    items: [{ id: 101, category_id: 52, title: { 'zh-CN': '测试商品' }, price_amount: '1', is_active: true }],
    total: 1, mapped_ids: [],
  } } })
  vi.spyOn(adminAPI, 'getUpstreamCategories').mockResolvedValue({ data: { data: {
    supported: true, categories: [
      { id: 50, parent_id: 0, name: { 'zh-CN': '社交账号' } },
      { id: 51, parent_id: 50, name: { 'zh-CN': 'Facebook' } },
      { id: 52, parent_id: 51, name: { 'zh-CN': '老号' } },
    ],
  } } })
  vi.spyOn(adminAPI, 'getUpstreamCategoryCounts').mockResolvedValue({ data: { data: { counts: { 52: 1 } } } })
  const batch = vi.spyOn(adminAPI, 'batchImportUpstreamProducts')
  if (fallback) batch.mockRejectedValue({ response: { status: 404 } })
  else batch.mockResolvedValue({ data: { data: { success_count: 1, results: [{ upstream_product_id: 101, success: true }] } } })
  const single = vi.spyOn(adminAPI, 'importUpstreamProduct').mockResolvedValue({ data: { data: {} } })
  const container = document.createElement('div')
  document.body.append(container)
  app = createApp(ProductMappings)
  app.use(i18n).mount(container)
  await flush()
  ;[...document.querySelectorAll<HTMLButtonElement>('button')].find(el => el.textContent?.trim() === '从上游导入')!.click()
  await flush()
  const modal = document.querySelector<HTMLElement>('[role="dialog"]')!
  await selectOption('CEOACC', modal.querySelector<HTMLElement>('[role="combobox"]')!)
  await selectOption('账号 / Facebook / 老号', modal.querySelectorAll<HTMLElement>('[role="combobox"]')[1]!)
  modal.querySelector<HTMLButtonElement>('#auto-create-cat')!.click()
  await flush()
  expect(modal.textContent).toContain('社交账号 / Facebook / 老号')
  modal.querySelector<HTMLButtonElement>('[role="checkbox"]')!.click()
  await flush()
  ;[...modal.querySelectorAll<HTMLButtonElement>('button')].find(el => el.textContent?.trim() === '导入选中 (1)')!.click()
  await flush()
  expect(batch).toHaveBeenCalledWith({ connection_id: 1, upstream_product_ids: [101], auto_create_category: true, category_id: undefined })
  if (fallback) expect(single).toHaveBeenCalledWith({ connection_id: 1, upstream_product_id: 101, auto_create_category: true, category_id: undefined })
})
