import { setImmediate } from 'node:timers'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { adminAPI } from '@/api/admin'
import type { AdminBackupConfig, AdminBackupRecord, AdminBackupStatus } from '@/api/types'
import i18n from '@/i18n'
import SettingsBackupTab from '@/views/admin/components/SettingsBackupTab.vue'
import { useBackupSettings } from './useBackupSettings'

const { notifyError, notifySuccess, confirmAction } = vi.hoisted(() => ({
  notifyError: vi.fn(), notifySuccess: vi.fn(), confirmAction: vi.fn(),
}))
vi.mock('@/utils/notify', () => ({ notifyError, notifySuccess }))
vi.mock('@/utils/confirm', () => ({ confirmAction }))

const reply = <T>(data: T) => ({ data: { status_code: 0, msg: 'success', data } })
const config = (changes: Partial<AdminBackupConfig> = {}): AdminBackupConfig => ({
  endpoint: 'https://storage.example.com', bucket: 'backups', access_key_id: 'access-id',
  secret_configured: true, prefix: 'dujiao-next', enabled: false,
  daily_time: '03:00', timezone: 'Asia/Shanghai', retain_count: 14, ...changes,
})
const record = (changes: Partial<AdminBackupRecord> = {}): AdminBackupRecord => ({
  id: 'backup-uuid', source: 'manual', admin_id: 1, status: 'succeeded', stage: 'uploading',
  filename: 'database.dump', size: 1024, sha256: 'a'.repeat(64), endpoint: 'https://storage.example.com',
  bucket: 'backups', object_key: 'dujiao-next/database.dump', error: '',
  created_at: '2026-10-19T03:00:00Z', started_at: '2026-10-19T03:00:00Z',
  finished_at: '2026-10-19T03:01:00Z', ...changes,
})
const status = (changes: Partial<AdminBackupStatus> = {}): AdminBackupStatus => ({
  supported: true, ready: true, pg_dump_version: 'pg_dump 18.0', server_version: '18.0',
  error: '', current: null, next_run: null, last_success: null, cleanup_error: '', ...changes,
})
const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
const flush = async () => {
  await new Promise<void>(done => setImmediate(done))
  await nextTick()
}
const apps: App[] = []
const mount = (active = ref(true)) => {
  let state!: ReturnType<typeof useBackupSettings>
  const app = createApp(defineComponent({
    setup() {
      state = useBackupSettings(active)
      return () => h('div')
    },
  }))
  const container = document.createElement('div')
  document.body.append(container)
  app.use(i18n).mount(container)
  apps.push(app)
  return { state, active, app }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  i18n.global.locale.value = 'en-US'
  vi.spyOn(adminAPI, 'getBackupConfig').mockResolvedValue(reply(config()))
  vi.spyOn(adminAPI, 'updateBackupConfig').mockImplementation(async payload => reply(config({ ...payload, secret_access_key: undefined })))
  vi.spyOn(adminAPI, 'getBackupStatus').mockResolvedValue(reply(status()))
  vi.spyOn(adminAPI, 'getBackupRecords').mockResolvedValue(reply({ items: [], total: 0, page: 1, page_size: 20 }))
  vi.spyOn(adminAPI, 'testBackupConnection').mockResolvedValue(reply(null))
  vi.spyOn(adminAPI, 'createBackup').mockResolvedValue(reply(record({ status: 'running', stage: 'exporting', finished_at: null })))
  vi.spyOn(adminAPI, 'getBackupDownload').mockResolvedValue(reply({ url: 'https://downloads.example.com/file?signature=signed' }))
  vi.spyOn(adminAPI, 'deleteBackup').mockResolvedValue(reply(null))
  confirmAction.mockResolvedValue(true)
})
afterEach(() => {
  for (const app of apps.splice(0)) app.unmount()
  document.body.replaceChildren()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('backup settings and actions', () => {
  it('loads only when activated and keeps defaults and unsaved edits across tab switches', async () => {
    const { state, active } = mount(ref(false))
    expect(adminAPI.getBackupConfig).not.toHaveBeenCalled()
    expect(state.form).toMatchObject({ enabled: false, daily_time: '03:00', timezone: 'Asia/Shanghai', retain_count: 14 })
    active.value = true
    await flush()
    expect(adminAPI.getBackupRecords).toHaveBeenCalledWith({ page: 1, page_size: 20 })
    state.form.prefix = 'unsaved'
    active.value = false
    active.value = true
    await flush()
    expect(state.form.prefix).toBe('unsaved')
    expect(adminAPI.getBackupConfig).toHaveBeenCalledTimes(1)
    expect(state.canTest.value).toBe(false)
    expect(state.canStart.value).toBe(false)
  })

  it('never renders a returned secret or backend status message keys', async () => {
    vi.mocked(adminAPI.getBackupConfig).mockResolvedValue(reply(config({ secret_access_key: 'must-never-appear' })))
    vi.mocked(adminAPI.getBackupStatus).mockResolvedValue(reply(status({ ready: false, error: 'error.backup_not_ready', cleanup_error: 'error.backup_failed' })))
    const container = document.createElement('div')
    document.body.append(container)
    const app = createApp(SettingsBackupTab, { active: true })
    apps.push(app)
    app.use(i18n).mount(container)
    await flush()
    const input = container.querySelector<HTMLInputElement>('#backup-secret-key')!
    expect(input.type).toBe('password')
    expect(input.value).toBe('')
    expect(input.placeholder).toBe('Leave blank to keep the saved secret')
    expect(container.innerHTML).not.toContain('must-never-appear')
    expect(container.textContent).not.toContain('error.backup_')
    expect(container.textContent).toContain('Check storage settings, the pg_dump installation')
    expect(container.textContent).toContain('Backup cleanup failed')
    const start = [...container.querySelectorAll('button')].find(button => button.textContent?.includes('Back up now'))!
    expect(start.disabled).toBe(true)
  })

  it('preserves the saved secret with a flat empty-secret PUT and prevents duplicate saves', async () => {
    const update = deferred<ReturnType<typeof reply<AdminBackupConfig>>>()
    vi.mocked(adminAPI.updateBackupConfig).mockReturnValue(update.promise)
    const { state } = mount()
    await flush()
    state.form.retain_count = 18
    const saving = state.save()
    await state.save()
    expect(adminAPI.updateBackupConfig).toHaveBeenCalledTimes(1)
    expect(adminAPI.updateBackupConfig).toHaveBeenCalledWith({
      endpoint: 'https://storage.example.com', bucket: 'backups', access_key_id: 'access-id', secret_access_key: '',
      prefix: 'dujiao-next', enabled: false, daily_time: '03:00', timezone: 'Asia/Shanghai', retain_count: 18,
    })
    update.resolve(reply(config({ retain_count: 18 })))
    await saving
    await flush()
    expect(state.form.secret_access_key).toBe('')
    expect(state.form.secret_configured).toBe(true)
    expect(state.dirty.value).toBe(false)
  })

  it('clears a replacement secret after saving and adopts the server-normalized configuration', async () => {
    vi.mocked(adminAPI.updateBackupConfig).mockResolvedValue(reply(config({ prefix: 'normalized', secret_access_key: 'never-render-this-either' })))
    const { state } = mount()
    await flush()
    state.form.secret_access_key = 'replacement-secret'
    state.form.prefix = '/normalized/'
    expect(state.canTest.value).toBe(false)
    await state.save()
    await flush()
    expect(adminAPI.updateBackupConfig).toHaveBeenCalledWith(expect.objectContaining({ secret_access_key: 'replacement-secret' }))
    expect(state.form.secret_access_key).toBe('')
    expect(state.form.prefix).toBe('normalized')
    expect(state.form.secret_configured).toBe(true)
    expect(state.canTest.value).toBe(true)
  })

  it.each([0, -1, 1.5, 1001, Number.NaN])('rejects invalid retention %s', async count => {
    const { state } = mount()
    await flush()
    state.form.retain_count = count
    await state.save()
    expect(adminAPI.updateBackupConfig).not.toHaveBeenCalled()
    expect(notifyError).toHaveBeenCalledWith('Retention count must be an integer between 1 and 1000')
  })

  it('requires a replacement secret when the access key ID changes', async () => {
    const { state } = mount()
    await flush()
    state.form.access_key_id = 'new-id'
    await state.save()
    expect(adminAPI.updateBackupConfig).not.toHaveBeenCalled()
    expect(notifyError).toHaveBeenCalledWith('Enter a new secret when changing the access key ID.')
  })

  it('tests only saved configuration and blocks actions in an unsupported environment', async () => {
    const { state } = mount()
    await flush()
    await state.testConnection()
    expect(adminAPI.testBackupConnection).toHaveBeenCalledTimes(1)
    state.form.bucket = 'unsaved-bucket'
    await state.testConnection()
    await state.startBackup()
    expect(adminAPI.testBackupConnection).toHaveBeenCalledTimes(1)
    expect(adminAPI.createBackup).not.toHaveBeenCalled()
    vi.mocked(adminAPI.getBackupStatus).mockResolvedValue(reply(status({ supported: false, ready: false })))
    await state.refresh()
    expect(state.canSave.value).toBe(false)
    expect(state.canTest.value).toBe(false)
    expect(state.canStart.value).toBe(false)
  })

  it('starts a manual backup and immediately discovers a running task', async () => {
    const { state } = mount()
    await flush()
    const runningRecord = record({ status: 'running', stage: 'exporting', finished_at: null })
    vi.mocked(adminAPI.getBackupStatus).mockResolvedValue(reply(status({ current: runningRecord })))
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(reply({ items: [runningRecord], total: 1, page: 1, page_size: 20 }))
    await state.startBackup()
    await flush()
    expect(adminAPI.createBackup).toHaveBeenCalledTimes(1)
    expect(state.runtime.value?.current).toMatchObject({ id: 'backup-uuid', status: 'running', stage: 'exporting' })
    expect(state.canStart.value).toBe(false)
    expect(state.canSave.value).toBe(false)
    expect(vi.getTimerCount()).toBe(1)
  })

  it('keeps records visible after a status error and does not repeat already-notified errors', async () => {
    vi.mocked(adminAPI.getBackupStatus).mockRejectedValue(Object.assign(new Error('Status unavailable'), { __notified: true }))
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(reply({ items: [record({ status: 'failed', error: 'database export failed' })], total: 1, page: 1, page_size: 20 }))
    const { state } = mount()
    await flush()
    expect(state.statusError.value).toBe('Status unavailable')
    expect(state.records.value[0]?.error).toBe('database export failed')
    expect(state.canStart.value).toBe(false)
    expect(notifyError).not.toHaveBeenCalled()
  })

  it('downloads the signed external URL without adding the admin base', async () => {
    let href = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { href = this.href })
    const { state } = mount()
    await flush()
    await state.download(record())
    expect(adminAPI.getBackupDownload).toHaveBeenCalledWith('backup-uuid')
    expect(href).toBe('https://downloads.example.com/file?signature=signed')
    await state.download(record({ status: 'failed' }))
    expect(adminAPI.getBackupDownload).toHaveBeenCalledTimes(1)
  })

  it('requires destructive confirmation and keeps a failed deletion available for retry', async () => {
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(reply({ items: [record()], total: 1, page: 1, page_size: 20 }))
    const { state } = mount()
    await flush()
    confirmAction.mockResolvedValueOnce(false)
    await state.remove(record())
    expect(adminAPI.deleteBackup).not.toHaveBeenCalled()
    vi.mocked(adminAPI.deleteBackup).mockRejectedValueOnce(Object.assign(new Error('Remote delete failed'), { __notified: true }))
    await state.remove(record())
    expect(confirmAction).toHaveBeenCalledWith(expect.objectContaining({ variant: 'destructive', description: expect.stringContaining('database.dump') }))
    expect(state.records.value).toHaveLength(1)
    expect(state.deletingId.value).toBe(null)
    expect(notifyError).not.toHaveBeenCalled()
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(reply({ items: [], total: 0, page: 1, page_size: 20 }))
    await state.remove(record())
    await flush()
    expect(state.records.value).toHaveLength(0)
  })
})

describe('backup polling lifecycle and request races', () => {
  it('polls every 3 seconds while running and stops after completion', async () => {
    const task = record({ status: 'running', stage: 'uploading', finished_at: null })
    vi.mocked(adminAPI.getBackupStatus).mockResolvedValue(reply(status({ current: task })))
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(reply({ items: [task], total: 1, page: 1, page_size: 20 }))
    const { state } = mount()
    await flush()
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(2999)
    expect(adminAPI.getBackupStatus).toHaveBeenCalledTimes(1)
    vi.mocked(adminAPI.getBackupStatus).mockResolvedValue(reply(status({ last_success: record() })))
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(reply({ items: [record()], total: 1, page: 1, page_size: 20 }))
    await vi.advanceTimersByTimeAsync(1)
    await flush()
    expect(adminAPI.getBackupStatus).toHaveBeenCalledTimes(2)
    expect(state.running.value).toBe(false)
    expect(state.runtime.value?.last_success?.id).toBe('backup-uuid')
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(9000)
    expect(adminAPI.getBackupStatus).toHaveBeenCalledTimes(2)
  })

  it('stops on tab switches, resumes on activation, and clears the timer on unmount', async () => {
    vi.mocked(adminAPI.getBackupStatus).mockResolvedValue(reply(status({ current: record({ status: 'running' }) })))
    const { active, app } = mount()
    await flush()
    active.value = false
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(9000)
    expect(adminAPI.getBackupStatus).toHaveBeenCalledTimes(1)
    active.value = true
    await flush()
    expect(adminAPI.getBackupStatus).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(1)
    app.unmount()
    apps.splice(apps.indexOf(app), 1)
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(9000)
    expect(adminAPI.getBackupStatus).toHaveBeenCalledTimes(2)
  })

  it('ignores delayed config, status, and records after deactivation and reactivation', async () => {
    const oldConfig = deferred<ReturnType<typeof reply<AdminBackupConfig>>>()
    const oldStatus = deferred<ReturnType<typeof reply<AdminBackupStatus>>>()
    const oldRecords = deferred<ReturnType<typeof reply<unknown>>>()
    vi.mocked(adminAPI.getBackupConfig).mockReturnValueOnce(oldConfig.promise)
    vi.mocked(adminAPI.getBackupStatus).mockReturnValueOnce(oldStatus.promise)
    vi.mocked(adminAPI.getBackupRecords).mockReturnValueOnce(oldRecords.promise)
    const { state, active } = mount()
    active.value = false
    active.value = true
    await flush()
    state.form.prefix = 'fresh-edit'
    oldConfig.resolve(reply(config({ prefix: 'stale-config' })))
    oldStatus.resolve(reply(status({ current: record({ status: 'running' }) })))
    oldRecords.resolve(reply({ items: [record({ status: 'running' })], total: 1, page: 1, page_size: 20 }))
    await flush()
    expect(state.form.prefix).toBe('fresh-edit')
    expect(state.runtime.value?.current).toBe(null)
    expect(state.records.value).toEqual([])
    expect(state.refreshing.value).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not resurrect polling when an in-flight response finishes after unmount', async () => {
    const pending = deferred<ReturnType<typeof reply<AdminBackupStatus>>>()
    vi.mocked(adminAPI.getBackupStatus).mockReturnValue(pending.promise)
    const { state, app } = mount()
    app.unmount()
    apps.splice(apps.indexOf(app), 1)
    pending.resolve(reply(status({ current: record({ status: 'running' }) })))
    await flush()
    expect(state.runtime.value).toBe(null)
    expect(state.records.value).toEqual([])
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['page-object', 'shared-pagination'])('reads %s records and ignores an older page response', async shape => {
    const response = (items: AdminBackupRecord[], page: number) => shape === 'page-object'
      ? reply({ items, total: 60, page, page_size: 20 })
      : { ...reply(items), data: { ...reply(items).data, pagination: { total: 60, page, page_size: 20 } } }
    vi.mocked(adminAPI.getBackupRecords).mockResolvedValue(response([record()], 1))
    const { state } = mount()
    await flush()
    expect(state.total.value).toBe(60)
    expect(state.totalPage.value).toBe(3)
    const stale = deferred<ReturnType<typeof response>>()
    vi.mocked(adminAPI.getBackupRecords).mockReturnValueOnce(stale.promise).mockResolvedValueOnce(response([record({ id: 'page-3' })], 3))
    state.changePage(2)
    state.changePage(3)
    await flush()
    stale.resolve(response([record({ id: 'page-2' })], 2))
    await flush()
    expect(state.page.value).toBe(3)
    expect(state.records.value[0]?.id).toBe('page-3')
    expect(adminAPI.getBackupRecords).toHaveBeenLastCalledWith({ page: 3, page_size: 20 })
  })
})
