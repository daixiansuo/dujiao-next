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
const selectOption = async (label: string) => {
  const trigger = document.querySelectorAll<HTMLElement>('[role="combobox"]')[1]!
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

  await selectOption('全部分类')
  expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, category_id: undefined }))
})
