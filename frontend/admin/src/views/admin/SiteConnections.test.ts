import { setImmediate } from 'node:timers'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import { adminAPI } from '@/api/admin'
import type { AdminSiteConnection } from '@/api/types'
import i18n from '@/i18n'
import SiteConnections from './SiteConnections.vue'

const { notifyError, notifySuccess, confirmAction } = vi.hoisted(() => ({
  notifyError: vi.fn(), notifySuccess: vi.fn(), confirmAction: vi.fn(),
}))
vi.mock('@/utils/notify', () => ({ notifyError, notifySuccess }))
vi.mock('@/utils/confirm', () => ({ confirmAction }))

const reply = <T>(data: T) => ({ data: { status_code: 0, msg: 'success', data } })
let saved: AdminSiteConnection
const apps: App[] = []
const flush = async () => {
  await new Promise<void>(done => setImmediate(done))
  await nextTick()
}
const button = (text: string) => {
  const result = [...document.querySelectorAll<HTMLButtonElement>('button')].find(el => el.textContent?.trim() === text)
  expect(result, `button ${text}`).toBeTruthy()
  return result!
}
const edit = async () => { button('编辑').click(); await flush() }
const submit = async () => {
  document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flush()
}
const setInput = async (id: string, value: string) => {
  const input = document.getElementById(id) as HTMLInputElement
  expect(input).toBeTruthy()
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flush()
}
const selectMode = async (label: string) => {
  document.getElementById('price-markup-mode')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flush()
  const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find(el => el.textContent?.trim() === label)
  expect(option).toBeTruthy()
  option!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  await flush()
}
const mount = async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const app = createApp(SiteConnections)
  apps.push(app)
  app.use(i18n).mount(container)
  await flush()
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  i18n.global.locale.value = 'zh-CN'
  saved = {
    id: 1, name: 'upstream', type: 'upstream', base_url: 'https://upstream.example.com', api_key: 'key',
    is_active: true, status: 'active', exchange_rate: 1, price_markup_percent: 40,
    price_markup_amount: '3.25', created_at: '', updated_at: '',
  }
  vi.spyOn(adminAPI, 'getSiteConnections').mockImplementation(async () => reply([{ ...saved }]))
  vi.spyOn(adminAPI, 'updateSiteConnection').mockImplementation(async (_id, payload) => {
    saved = { ...saved, ...payload }
    return reply({ ...saved })
  })
  vi.spyOn(adminAPI, 'reapplyConnectionMarkup').mockResolvedValue(reply({ updated_products: 1 }))
  confirmAction.mockResolvedValue(true)
})
afterEach(() => {
  for (const app of apps.splice(0)) app.unmount()
  document.body.replaceChildren()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('connection pricing form', () => {
  it('defaults legacy connections to percentage and saves only the selected field', async () => {
    await mount()
    await edit()
    expect((document.getElementById('price-markup-percent') as HTMLInputElement).value).toBe('40')
    await selectMode('固定金额')
    expect((document.getElementById('price-markup-amount') as HTMLInputElement).value).toBe('3.25')
    await setInput('price-markup-amount', '5.20')
    await selectMode('百分比')
    expect((document.getElementById('price-markup-percent') as HTMLInputElement).value).toBe('40')
    await selectMode('固定金额')
    expect((document.getElementById('price-markup-amount') as HTMLInputElement).value).toBe('5.2')
    await submit()
    expect(adminAPI.updateSiteConnection).toHaveBeenCalledWith(1, expect.objectContaining({ price_markup_mode: 'fixed_amount', price_markup_amount: '5.2' }))
    expect(vi.mocked(adminAPI.updateSiteConnection).mock.calls[0]![1]).not.toHaveProperty('price_markup_percent')
    expect(saved.price_markup_percent).toBe(40)
    await edit()
    expect((document.getElementById('price-markup-amount') as HTMLInputElement).value).toBe('5.2')
    expect(notifyError).not.toHaveBeenCalled()
  })

  it('saves explicit zero, reloads it and keeps reapply available for zero markup', async () => {
    saved.price_markup_mode = 'fixed_amount'
    saved.price_markup_percent = 0
    await mount()
    await edit()
    await setInput('price-markup-amount', '0')
    await submit()
    expect(adminAPI.updateSiteConnection).toHaveBeenCalledWith(1, expect.objectContaining({ price_markup_amount: '0', price_markup_mode: 'fixed_amount' }))
    button('重新应用加价').click()
    await flush()
    expect(adminAPI.reapplyConnectionMarkup).toHaveBeenCalledWith(1)
    await edit()
    expect((document.getElementById('price-markup-amount') as HTMLInputElement).value).toBe('0')
    await selectMode('百分比')
    await submit()
    expect(vi.mocked(adminAPI.updateSiteConnection).mock.calls[1]![1]).toMatchObject({ price_markup_mode: 'percent', price_markup_percent: 0 })
    expect(vi.mocked(adminAPI.updateSiteConnection).mock.calls[1]![1]).not.toHaveProperty('price_markup_amount')
    expect(button('重新应用加价')).toBeTruthy()
  })
})
