import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from './client'
import { adminAPI } from './admin'

const { notifyError } = vi.hoisted(() => ({ notifyError: vi.fn() }))
vi.mock('@/utils/notify', () => ({ notifyError }))

beforeEach(() => { vi.clearAllMocks(); localStorage.clear() })
afterEach(() => vi.unstubAllGlobals())

const failedResponse = (status = 200, code = 500) => new Response(
  JSON.stringify({ status_code: code, msg: '分类重算失败' }), { status },
)

describe('API error notification ownership', () => {
  it('lets automatic markup recalculation own its notification while manual requests keep the default', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => failedResponse()))
    const error = await adminAPI.reapplyConnectionMarkup(1, { skipErrorNotification: true }).catch(err => err)
    expect(error.message).toBe('分类重算失败')
    expect(error.__notified).not.toBe(true)
    expect(notifyError).not.toHaveBeenCalled()
    const manualError = await adminAPI.reapplyConnectionMarkup(1).catch(err => err)
    expect(manualError.__notified).toBe(true)
    expect(notifyError).toHaveBeenCalledExactlyOnceWith('分类重算失败')
  })

  it.each(['network', 'json', 'http', 'blob'])('propagates silent %s failures without marking them notified', async kind => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => {
      if (kind === 'network') throw new TypeError('Failed to fetch')
      if (kind === 'json') return new Response('invalid JSON', { status: 200 })
      return failedResponse(500)
    }))
    const error = await api.post('/admin/site-connections/1/reapply-markup', undefined, {
      skipErrorNotification: true, blob: kind === 'blob',
    }).catch(err => err)
    expect(error).toBeInstanceOf(Error)
    expect(error.__notified).not.toBe(true)
    expect(notifyError).not.toHaveBeenCalled()
  })

  it.each([401, 200])('still clears authentication and redirects on silent unauthorized responses (HTTP %s)', async status => {
    const location = { href: '' }
    vi.stubGlobal('window', { location, performance: window.performance })
    localStorage.setItem('admin_token', 'expired-token')
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => failedResponse(status, 401)))
    const error = await api.post('/admin/site-connections/1/reapply-markup', undefined, { skipErrorNotification: true }).catch(err => err)
    expect(error.message).toBe('分类重算失败')
    expect(localStorage.getItem('admin_token')).toBeNull()
    expect(location.href).toMatch(/\/login$/)
    expect(notifyError).not.toHaveBeenCalled()
  })
})
