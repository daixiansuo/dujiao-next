import { afterEach, describe, expect, it, vi } from 'vitest'
import { adminAPI } from './admin'

const response = (data: unknown) => new Response(JSON.stringify({ status_code: 0, msg: 'success', data }), { status: 200 })

afterEach(() => vi.unstubAllGlobals())

describe('backup HTTP contract', () => {
  it('uses the dedicated config/status endpoints, flat config, and the prescribed POST bodies', async () => {
    const fetch = vi.fn().mockImplementation(async () => response(null))
    vi.stubGlobal('fetch', fetch)
    const payload = {
      endpoint: 'https://storage.example.com', bucket: 'backups', access_key_id: 'access-id', secret_access_key: '',
      prefix: 'dujiao-next', enabled: false, daily_time: '03:00', timezone: 'Asia/Shanghai', retain_count: 14,
    }
    await adminAPI.getBackupConfig()
    await adminAPI.updateBackupConfig(payload)
    await adminAPI.testBackupConnection()
    await adminAPI.getBackupStatus()
    await adminAPI.getBackupRecords({ page: 2, page_size: 20 })
    await adminAPI.createBackup()
    await adminAPI.getBackupDownload('string/id?')
    await adminAPI.deleteBackup('string/id?')
    const calls = fetch.mock.calls.map(([url, options]) => ({ url, method: options.method, body: options.body }))
    expect(calls).toEqual([
      { url: '/api/v1/admin/backups/config', method: 'GET', body: undefined },
      { url: '/api/v1/admin/backups/config', method: 'PUT', body: JSON.stringify(payload) },
      { url: '/api/v1/admin/backups/test', method: 'POST', body: undefined },
      { url: '/api/v1/admin/backups/status', method: 'GET', body: undefined },
      { url: '/api/v1/admin/backups/records?page=2&page_size=20', method: 'GET', body: undefined },
      { url: '/api/v1/admin/backups/records', method: 'POST', body: '{}' },
      { url: '/api/v1/admin/backups/records/string%2Fid%3F/download', method: 'POST', body: undefined },
      { url: '/api/v1/admin/backups/records/string%2Fid%3F', method: 'DELETE', body: undefined },
    ])
  })
})
