import { setImmediate } from 'node:timers'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { adminAPI } from '@/api/admin'
import type { AdminMedia, AdminProduct } from '@/api/types'
import i18n from '@/i18n'
import Products from './Products.vue'

const { notifyError, notifySuccess } = vi.hoisted(() => ({
  notifyError: vi.fn(), notifySuccess: vi.fn(),
}))
vi.mock('@/utils/notify', () => ({ notifyError, notifySuccess }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    getProducts: vi.fn(), getCategories: vi.fn(), getSettings: vi.fn(),
    batchUpdateProductImages: vi.fn(), getMedia: vi.fn(), upload: vi.fn(),
  },
}))
vi.mock('./components/ProductEditModal.vue', () => ({ default: { render: () => null } }))

const reply = <T>(data: T) => ({ data: { status_code: 0, msg: 'success', data } })
const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
const product = (id: number): AdminProduct => ({
  id, category_id: 0, slug: `product-${id}`, seo_meta: {},
  title: { 'zh-CN': `商品 ${id}` }, description: {}, content: {},
  price_amount: 10, cost_price_amount: 5, images: [`/uploads/original-${id}.png`], tags: [],
  purchase_type: 'guest', min_purchase_quantity: 1, max_purchase_quantity: 0,
  stock_display_mode: 'exact', fulfillment_type: 'manual', manual_form_schema: null,
  manual_stock_total: 10, manual_stock_locked: 0, manual_stock_sold: 0,
  payment_channel_ids: '', is_affiliate_enabled: false,
  auto_stock_available: 0, auto_stock_total: 0, auto_stock_locked: 0, auto_stock_sold: 0,
  is_mapped: false, is_active: true, sort_order: 0, created_at: '', updated_at: '',
})
const mediaItems: AdminMedia[] = ['A', 'B', 'C'].map((name, index) => ({
  id: index + 1, name: `gallery ${name}`, filename: `${name}.png`, path: `/uploads/${name}.png`,
  mime_type: 'image/png', size: 100, scene: 'product', width: 100, height: 100, created_at: '',
}))
let savedProducts: AdminProduct[]
const replaceImages = (ids: number[], images: string[]) => {
  savedProducts = savedProducts.map(item => ids.includes(item.id) ? { ...item, images: [...images] } : item)
}
const apps: App[] = []
const flush = async () => {
  await new Promise<void>(done => setImmediate(done))
  await nextTick()
}
const findButton = (text: string, root: ParentNode = document) =>
  [...root.querySelectorAll<HTMLButtonElement>('button')].find(el => el.textContent?.trim() === text)
const button = (text: string, root: ParentNode = document) => {
  const result = findButton(text, root)
  expect(result, `button ${text}`).toBeTruthy()
  return result!
}
const clickButton = async (text: string, root: ParentNode = document) => {
  button(text, root).click()
  await flush()
}
const findDialog = (title: string) =>
  [...document.querySelectorAll<HTMLElement>('[role="dialog"][data-state="open"]')]
    .find(el => el.querySelector('h2')?.textContent?.trim() === title)
const dialog = (title: string) => {
  const result = findDialog(title)
  expect(result, `dialog ${title}`).toBeTruthy()
  return result!
}
const batchDialog = () => dialog('批量修改图片')
const libraryDialog = () => dialog('选择素材')
const previewImages = () => [...batchDialog().querySelectorAll('img')].map(el => el.getAttribute('src'))
const row = (id: number) => {
  const result = [...document.querySelectorAll<HTMLTableRowElement>('tbody tr')]
    .find(el => el.querySelector('td:nth-child(2)')?.textContent?.trim() === `#${id}`)
  expect(result, `product row ${id}`).toBeTruthy()
  return result!
}
const cover = (id: number) => row(id).querySelector('img')?.getAttribute('src')
const selectProduct = async (id: number) => {
  row(id).querySelector<HTMLButtonElement>('[role="checkbox"]')!.click()
  await flush()
}
const selectAll = async () => {
  document.querySelector<HTMLButtonElement>('thead [role="checkbox"]')!.click()
  await flush()
}
const mount = async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/products', component: Products }],
  })
  await router.push('/products')
  await router.isReady()
  const container = document.createElement('div')
  document.body.append(container)
  const app = createApp(Products)
  apps.push(app)
  app.use(i18n).use(router).mount(container)
  await flush()
}
const openBatch = async (ids = [101, 102]) => {
  for (const id of ids) await selectProduct(id)
  await clickButton('批量修改图片')
  expect(batchDialog().textContent).toContain(`已选 ${ids.length} 个商品`)
}
const openLibrary = () => clickButton('从素材库选择', batchDialog())
const selectMedia = async (index: number) => {
  const item = mediaItems[index]!
  const image = [...libraryDialog().querySelectorAll<HTMLImageElement>('img')].find(el => el.alt === item.name)
  expect(image, `media ${item.name}`).toBeTruthy()
  image!.click()
  await flush()
}
const chooseImages = async (indices: number[]) => {
  await openLibrary()
  for (const index of indices) await selectMedia(index)
  await clickButton('确认选择', libraryDialog())
}
const uploadFiles = async (input: HTMLInputElement, files: File[]) => {
  Object.defineProperty(input, 'files', { configurable: true, value: files })
  input.dispatchEvent(new Event('change', { bubbles: true }))
  await flush()
}
const escape = async (root: HTMLElement) => {
  root.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
  await flush()
}
// Dispatch also exercises the handler guard before Vue patches disabled buttons.
const dispatchClick = (target: HTMLButtonElement) =>
  target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  i18n.global.locale.value = 'zh-CN'
  savedProducts = [101, 102, 103].map(product)
  vi.mocked(adminAPI.getProducts).mockImplementation(async (params = {}) => {
    const page = Number(params.page || 1)
    const pageSize = Number(params.page_size || 10)
    const items = savedProducts.slice((page - 1) * pageSize, page * pageSize)
      .map(item => ({ ...item, images: [...item.images] }))
    return { data: { ...reply(items).data, pagination: {
      page, page_size: pageSize, total: savedProducts.length, total_page: Math.ceil(savedProducts.length / pageSize),
    } } }
  })
  vi.mocked(adminAPI.getCategories).mockResolvedValue(reply([]))
  vi.mocked(adminAPI.getSettings).mockResolvedValue(reply({ currency: 'CNY' }))
  vi.mocked(adminAPI.getMedia).mockImplementation(async (params = {}) => {
    const items = mediaItems.filter(item => item.name.includes(String(params.search || '')))
    return reply({ items, total: items.length })
  })
  vi.mocked(adminAPI.batchUpdateProductImages).mockImplementation(async (ids, images) => {
    replaceImages(ids, images)
    return reply({ total: ids.length, success_count: ids.length, failed_items: [] })
  })
  vi.mocked(adminAPI.upload).mockRejectedValue(new Error('Unexpected upload'))
})
afterEach(async () => {
  for (const app of apps.splice(0)) app.unmount()
  await flush()
  document.body.replaceChildren()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('product batch images', () => {
  it('selects only the current page, submits one ordered image array and refreshes the covers', async () => {
    savedProducts = Array.from({ length: 12 }, (_, index) => product(101 + index))
    await mount()
    expect(findButton('批量修改图片')).toBeUndefined()
    await selectAll()
    await clickButton('下一页')
    expect(findButton('批量修改图片')).toBeUndefined()
    expect(document.querySelectorAll('tbody [role="checkbox"]')).toHaveLength(2)
    await selectAll()
    await selectProduct(112)
    expect(document.querySelector('thead [role="checkbox"]')?.getAttribute('aria-checked')).toBe('mixed')
    await selectAll()
    await clickButton('批量修改图片')
    expect(batchDialog().textContent).toContain('已选 2 个商品')
    expect(previewImages()).toEqual([])
    await chooseImages([2, 0, 1])
    const images = ['/uploads/C.png', '/uploads/A.png', '/uploads/B.png']
    expect(previewImages()).toEqual(images)
    await clickButton('保存', batchDialog())
    expect(adminAPI.batchUpdateProductImages).toHaveBeenCalledExactlyOnceWith([111, 112], images)
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(3)
    expect(adminAPI.getProducts).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 10 }))
    expect(cover(111)).toBe(images[0])
    expect(cover(112)).toBe(images[0])
    expect(savedProducts.slice(0, 10).every(item => item.images[0] === `/uploads/original-${item.id}.png`)).toBe(true)
    expect(findDialog('批量修改图片')).toBeUndefined()
    expect(notifySuccess).toHaveBeenCalledExactlyOnceWith('商品图片更新完成 2/2')
    expect(notifyError).not.toHaveBeenCalled()
  })

  it('blocks empty submissions initially and after removing the last image', async () => {
    await mount()
    await openBatch([101])
    expect(button('保存', batchDialog()).disabled).toBe(true)
    dispatchClick(button('保存', batchDialog()))
    await flush()
    expect(adminAPI.batchUpdateProductImages).not.toHaveBeenCalled()
    await chooseImages([0])
    expect(button('保存', batchDialog()).disabled).toBe(false)
    await clickButton('×', batchDialog())
    expect(previewImages()).toEqual([])
    expect(button('保存', batchDialog()).disabled).toBe(true)
    dispatchClick(button('保存', batchDialog()))
    await flush()
    expect(adminAPI.batchUpdateProductImages).not.toHaveBeenCalled()
  })

  it('preserves the images and target IDs after a network error for retry', async () => {
    vi.mocked(adminAPI.batchUpdateProductImages).mockRejectedValueOnce(new Error('connection lost'))
    await mount()
    await openBatch()
    await chooseImages([1, 0])
    await clickButton('保存', batchDialog())
    const images = ['/uploads/B.png', '/uploads/A.png']
    expect(previewImages()).toEqual(images)
    expect(batchDialog().textContent).toContain('已选 2 个商品')
    expect(button('保存', batchDialog()).disabled).toBe(false)
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(1)
    expect(notifyError).toHaveBeenCalledExactlyOnceWith(expect.stringContaining('connection lost'))
    expect(notifySuccess).not.toHaveBeenCalled()
    await clickButton('保存', batchDialog())
    expect(adminAPI.batchUpdateProductImages).toHaveBeenNthCalledWith(1, [101, 102], images)
    expect(adminAPI.batchUpdateProductImages).toHaveBeenNthCalledWith(2, [101, 102], images)
    expect(findDialog('批量修改图片')).toBeUndefined()
    expect(cover(101)).toBe(images[0])
  })

  it('shows partial failure details, refreshes successes and retries only failed IDs', async () => {
    vi.mocked(adminAPI.batchUpdateProductImages).mockImplementationOnce(async (_ids, images) => {
      replaceImages([101], images)
      return reply({ total: 3, success_count: 1, failed_items: [
        { id: 102, message: '商品不存在' }, { id: 103, message: '更新失败' },
      ] })
    })
    await mount()
    await openBatch([101, 102, 103])
    await chooseImages([1, 0])
    await clickButton('保存', batchDialog())
    const images = ['/uploads/B.png', '/uploads/A.png']
    const status = batchDialog().querySelector('[role="status"]')!
    expect(status.textContent).toContain('商品图片更新成功 1/3')
    expect([...status.querySelectorAll('li')].map(el => el.textContent)).toEqual([
      '#102 · 商品不存在', '#103 · 更新失败',
    ])
    expect(batchDialog().textContent).toContain('已选 2 个商品')
    expect(previewImages()).toEqual(images)
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(2)
    expect(cover(101)).toBe(images[0])
    expect(cover(102)).toBe('/uploads/original-102.png')
    expect(notifySuccess).not.toHaveBeenCalled()
    await clickButton('重试失败项', batchDialog())
    expect(adminAPI.batchUpdateProductImages).toHaveBeenNthCalledWith(1, [101, 102, 103], images)
    expect(adminAPI.batchUpdateProductImages).toHaveBeenNthCalledWith(2, [102, 103], images)
    expect(adminAPI.batchUpdateProductImages).toHaveBeenCalledTimes(2)
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(3)
    expect(cover(102)).toBe(images[0])
    expect(cover(103)).toBe(images[0])
    expect(findDialog('批量修改图片')).toBeUndefined()
    expect(notifySuccess).toHaveBeenCalledExactlyOnceWith('商品图片更新完成 2/2')
  })

  it('keeps all failures visible without a success notice or refresh and clears them on reopening', async () => {
    vi.mocked(adminAPI.batchUpdateProductImages).mockResolvedValueOnce(reply({ total: 2, success_count: 0, failed_items: [
      { id: 101, message: '商品不存在' }, { id: 102, message: '更新失败' },
    ] }))
    await mount()
    await openBatch()
    await chooseImages([0])
    await clickButton('保存', batchDialog())
    const status = batchDialog().querySelector('[role="status"]')!
    expect(status.textContent).toContain('商品图片更新成功 0/2')
    expect([...status.querySelectorAll('li')].map(el => el.textContent)).toEqual([
      '#101 · 商品不存在', '#102 · 更新失败',
    ])
    expect(previewImages()).toEqual(['/uploads/A.png'])
    expect(button('重试失败项', batchDialog()).disabled).toBe(false)
    expect(notifySuccess).not.toHaveBeenCalled()
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(1)
    expect(cover(101)).toBe('/uploads/original-101.png')
    await clickButton('取消', batchDialog())
    await clickButton('批量修改图片')
    expect(batchDialog().querySelector('[role="status"]')).toBeNull()
    expect(previewImages()).toEqual([])
    expect(button('保存', batchDialog()).disabled).toBe(true)
    expect(findButton('重试失败项', batchDialog())).toBeUndefined()
  })

  it('deduplicates save clicks before rendering and blocks closing while the save is pending', async () => {
    const request = deferred<ReturnType<typeof reply>>()
    vi.mocked(adminAPI.batchUpdateProductImages).mockReturnValueOnce(request.promise)
    await mount()
    await openBatch()
    await chooseImages([0])
    const save = button('保存', batchDialog())
    dispatchClick(save)
    dispatchClick(save)
    expect(adminAPI.batchUpdateProductImages).toHaveBeenCalledTimes(1)
    await flush()
    expect(save.disabled).toBe(true)
    expect(button('取消', batchDialog()).disabled).toBe(true)
    await clickButton('取消', batchDialog())
    await clickButton('Close', batchDialog())
    await escape(batchDialog())
    expect(previewImages()).toEqual(['/uploads/A.png'])
    dispatchClick(save)
    await flush()
    expect(adminAPI.batchUpdateProductImages).toHaveBeenCalledTimes(1)
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(1)
    replaceImages([101, 102], ['/uploads/A.png'])
    request.resolve(reply({ total: 2, success_count: 2, failed_items: [] }))
    await flush()
    expect(findDialog('批量修改图片')).toBeUndefined()
    expect(adminAPI.getProducts).toHaveBeenCalledTimes(2)
    expect(cover(101)).toBe('/uploads/A.png')
  })

  it('uses the real picker uploading state to block save and close even with an existing image', async () => {
    const request = deferred<ReturnType<typeof reply>>()
    vi.mocked(adminAPI.upload).mockReturnValueOnce(request.promise)
    await mount()
    await openBatch()
    await chooseImages([0])
    const input = batchDialog().querySelector<HTMLInputElement>('input[type="file"]')!
    const file = new File(['image'], 'direct.png', { type: 'image/png' })
    await uploadFiles(input, [file])
    expect(adminAPI.upload).toHaveBeenCalledExactlyOnceWith(expect.any(FormData), 'product')
    expect(vi.mocked(adminAPI.upload).mock.calls[0]![0].get('file')).toBe(file)
    expect(previewImages()).toEqual(['/uploads/A.png'])
    expect(button('保存', batchDialog()).disabled).toBe(true)
    expect(button('取消', batchDialog()).disabled).toBe(true)
    dispatchClick(button('保存', batchDialog()))
    await clickButton('取消', batchDialog())
    await clickButton('Close', batchDialog())
    await escape(batchDialog())
    expect(previewImages()).toEqual(['/uploads/A.png'])
    await uploadFiles(input, [file])
    expect(adminAPI.upload).toHaveBeenCalledTimes(1)
    expect(adminAPI.batchUpdateProductImages).not.toHaveBeenCalled()
    request.resolve(reply({ url: '/uploads/direct.png' }))
    await flush()
    expect(previewImages()).toEqual(['/uploads/A.png', '/uploads/direct.png'])
    expect(button('保存', batchDialog()).disabled).toBe(false)
    expect(button('取消', batchDialog()).disabled).toBe(false)
    await clickButton('保存', batchDialog())
    expect(adminAPI.batchUpdateProductImages).toHaveBeenCalledExactlyOnceWith(
      [101, 102], ['/uploads/A.png', '/uploads/direct.png'],
    )
  })

  it('waits for every library upload before allowing confirmation or dismissal', async () => {
    const first = deferred<ReturnType<typeof reply>>()
    const second = deferred<ReturnType<typeof reply>>()
    vi.mocked(adminAPI.upload).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    await mount()
    await openBatch()
    await chooseImages([0])
    await openLibrary()
    await clickButton('上传新文件', libraryDialog())
    const input = libraryDialog().querySelector<HTMLInputElement>('input[type="file"]')!
    const files = ['first', 'second'].map(name => new File(['image'], `${name}.png`, { type: 'image/png' }))
    await uploadFiles(input, files)
    expect(adminAPI.upload).toHaveBeenCalledTimes(2)
    first.resolve(reply({ url: '/uploads/first.png' }))
    await flush()
    expect(libraryDialog().textContent).toContain('已选择 2 项')
    expect(button('确认选择', libraryDialog()).disabled).toBe(true)
    expect(button('取消', libraryDialog()).disabled).toBe(true)
    expect(button('保存', batchDialog()).disabled).toBe(true)
    dispatchClick(button('确认选择', libraryDialog()))
    await clickButton('取消', libraryDialog())
    await clickButton('Close', libraryDialog())
    await escape(libraryDialog())
    expect(libraryDialog()).toBeTruthy()
    expect(previewImages()).toEqual(['/uploads/A.png'])
    await uploadFiles(input, files)
    expect(adminAPI.upload).toHaveBeenCalledTimes(2)
    expect(adminAPI.batchUpdateProductImages).not.toHaveBeenCalled()
    second.resolve(reply({ url: '/uploads/second.png' }))
    await flush()
    expect(button('确认选择', libraryDialog()).disabled).toBe(false)
    expect(button('取消', libraryDialog()).disabled).toBe(false)
    expect(libraryDialog().textContent).toContain('已选择 3 项')
    expect(previewImages()).toEqual(['/uploads/A.png'])
    await clickButton('确认选择', libraryDialog())
    const images = ['/uploads/A.png', '/uploads/first.png', '/uploads/second.png']
    expect(previewImages()).toEqual(images)
    await clickButton('保存', batchDialog())
    expect(adminAPI.batchUpdateProductImages).toHaveBeenCalledExactlyOnceWith([101, 102], images)
  })

  it('discards cancelled library selections and resets the library and batch dialog on reopening', async () => {
    await mount()
    await openBatch()
    await chooseImages([0])
    await openLibrary()
    await selectMedia(1)
    expect(libraryDialog().textContent).toContain('已选择 2 项')
    const search = libraryDialog().querySelector<HTMLInputElement>('input[placeholder="搜索素材..."]')!
    search.value = 'gallery'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await vi.waitFor(() => expect(adminAPI.getMedia).toHaveBeenLastCalledWith(expect.objectContaining({ search: 'gallery' })))
    await clickButton('上传新文件', libraryDialog())
    await clickButton('取消', libraryDialog())
    expect(findDialog('选择素材')).toBeUndefined()
    expect(previewImages()).toEqual(['/uploads/A.png'])
    await openLibrary()
    const reopenedSearch = libraryDialog().querySelector<HTMLInputElement>('input[placeholder="搜索素材..."]')!
    expect(reopenedSearch.value).toBe('')
    expect(libraryDialog().textContent).toContain('已选择 1 项')
    expect(adminAPI.getMedia).toHaveBeenLastCalledWith({ page: 1, page_size: 18 })
    await clickButton('确认选择', libraryDialog())
    expect(previewImages()).toEqual(['/uploads/A.png'])
    await clickButton('取消', batchDialog())
    await clickButton('批量修改图片')
    expect(batchDialog().textContent).toContain('已选 2 个商品')
    expect(previewImages()).toEqual([])
    expect(button('保存', batchDialog()).disabled).toBe(true)
    await openLibrary()
    expect(button('确认选择', libraryDialog()).disabled).toBe(true)
    expect(libraryDialog().textContent).not.toContain('已选择')
    expect(adminAPI.batchUpdateProductImages).not.toHaveBeenCalled()
  })
})
