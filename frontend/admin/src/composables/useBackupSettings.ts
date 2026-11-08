import { computed, onScopeDispose, reactive, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminBackupConfig, AdminBackupConfigUpdate, AdminBackupRecord, AdminBackupRecordsPage, AdminBackupStatus, ApiResponse } from '@/api/types'
import { confirmAction } from '@/utils/confirm'
import { notifyError, notifySuccess } from '@/utils/notify'

const defaultConfig = (): AdminBackupConfig & { secret_access_key: string } => ({
  endpoint: '', bucket: '', access_key_id: '', secret_access_key: '', secret_configured: false,
  prefix: 'dujiao-next', enabled: false, daily_time: '03:00', timezone: 'Asia/Shanghai', retain_count: 14,
})

const configPayload = (config: AdminBackupConfig): AdminBackupConfigUpdate => ({
  endpoint: config.endpoint.trim(),
  bucket: config.bucket.trim(),
  access_key_id: config.access_key_id.trim(),
  secret_access_key: config.secret_access_key || '',
  prefix: config.prefix.trim(),
  enabled: config.enabled,
  daily_time: config.daily_time,
  timezone: config.timezone.trim(),
  retain_count: Number(config.retain_count),
})

export const useBackupSettings = (active: MaybeRefOrGetter<boolean>) => {
  const { t } = useI18n()
  const form = reactive(defaultConfig())
  const savedConfig = ref<AdminBackupConfig | null>(null)
  const loading = ref(false)
  const refreshing = ref(false)
  const submitting = ref(false)
  const testing = ref(false)
  const starting = ref(false)
  const deletingId = ref<string | null>(null)
  const downloadingId = ref<string | null>(null)
  const runtime = ref<AdminBackupStatus | null>(null)
  const records = ref<AdminBackupRecord[]>([])
  const recordsLoaded = ref(false)
  const configError = ref('')
  const statusError = ref('')
  const recordsError = ref('')
  const page = ref(1)
  const pageSize = 20
  const total = ref(0)
  const totalPage = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
  const running = computed(() => runtime.value?.current?.status === 'running' || records.value.some(record => record.status === 'running'))
  const busy = computed(() => submitting.value || testing.value || starting.value || deletingId.value !== null || downloadingId.value !== null)
  const dirty = computed(() => !savedConfig.value || JSON.stringify(configPayload(form)) !== JSON.stringify(configPayload(savedConfig.value)))
  const storageConfigured = computed(() => {
    const config = savedConfig.value
    return !!(config?.endpoint && config.bucket && config.access_key_id && config.secret_configured)
  })
  const canSave = computed(() => toValue(active) && !!savedConfig.value && !loading.value && !busy.value && !running.value && runtime.value?.supported !== false)
  const canTest = computed(() => canSave.value && storageConfigured.value && !dirty.value && runtime.value?.supported === true)
  const canStart = computed(() => canTest.value && runtime.value?.ready === true && !refreshing.value && !statusError.value)

  let disposed = false
  let session = 0
  let refreshRequest = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  const isCurrent = (value: number) => !disposed && toValue(active) && value === session
  const stopTimer = () => {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
  }
  const errorMessage = (err: unknown, key: string) => (err as Error)?.message || t(`admin.settings.backup.${key}`)
  const reportError = (err: unknown, key: string) => {
    if (!(err as Error & { __notified?: boolean })?.__notified) notifyError(errorMessage(err, key))
  }

  const loadConfig = async (currentSession: number) => {
    loading.value = true
    configError.value = ''
    try {
      const response = await adminAPI.getBackupConfig()
      if (!isCurrent(currentSession)) return
      // Even an incorrectly unmasked response must never populate the secret input.
      const config = { ...defaultConfig(), ...response.data.data as AdminBackupConfig, secret_access_key: '' }
      Object.assign(form, config)
      savedConfig.value = { ...config }
    } catch (err) {
      if (!isCurrent(currentSession)) return
      configError.value = errorMessage(err, 'loadFailed')
      reportError(err, 'loadFailed')
    } finally {
      if (isCurrent(currentSession)) loading.value = false
    }
  }

  const refresh = async () => {
    if (disposed || !toValue(active)) return
    stopTimer()
    const currentSession = session
    const request = ++refreshRequest
    const requestedPage = page.value
    refreshing.value = true
    const [statusResult, recordsResult] = await Promise.allSettled([
      adminAPI.getBackupStatus(),
      adminAPI.getBackupRecords({ page: requestedPage, page_size: pageSize }),
    ])
    if (!isCurrent(currentSession) || request !== refreshRequest) return
    if (statusResult.status === 'fulfilled') {
      runtime.value = statusResult.value.data.data as AdminBackupStatus
      statusError.value = ''
    } else {
      statusError.value = errorMessage(statusResult.reason, 'statusFailed')
      reportError(statusResult.reason, 'statusFailed')
    }
    if (recordsResult.status === 'fulfilled') {
      const response = recordsResult.value.data as ApiResponse<AdminBackupRecordsPage | AdminBackupRecord[] | null>
      const data = response.data
      // Support the agreed page object and the server's shared pagination envelope.
      records.value = Array.isArray(data) ? data : data?.items || []
      total.value = Array.isArray(data) || data === null ? response.pagination?.total || 0 : data.total
      recordsLoaded.value = true
      recordsError.value = ''
    } else {
      recordsError.value = errorMessage(recordsResult.reason, 'recordsFailed')
      reportError(recordsResult.reason, 'recordsFailed')
    }
    refreshing.value = false
    if (page.value > totalPage.value && recordsResult.status === 'fulfilled') {
      page.value = totalPage.value
      void refresh()
      return
    }
    if (running.value) timer = setTimeout(() => void refresh(), 3000)
  }

  const reload = async () => {
    const currentSession = session
    await Promise.all([savedConfig.value ? Promise.resolve() : loadConfig(currentSession), refresh()])
  }

  const save = async () => {
    if (!canSave.value) return
    const payload = configPayload(form)
    if (!Number.isInteger(payload.retain_count) || payload.retain_count < 1 || payload.retain_count > 1000) {
      notifyError(t('admin.settings.backup.invalidRetainCount'))
      return
    }
    if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(payload.daily_time)) {
      notifyError(t('admin.settings.backup.invalidDailyTime'))
      return
    }
    try {
      if (!payload.timezone) throw new Error('timezone')
      new Intl.DateTimeFormat('en-US', { timeZone: payload.timezone })
    } catch {
      notifyError(t('admin.settings.backup.invalidTimezone'))
      return
    }
    if (savedConfig.value?.secret_configured && payload.access_key_id !== savedConfig.value.access_key_id && !payload.secret_access_key) {
      notifyError(t('admin.settings.backup.secretRequiredForAccessKey'))
      return
    }
    submitting.value = true
    try {
      const response = await adminAPI.updateBackupConfig(payload)
      if (disposed) return
      const config = { ...defaultConfig(), ...response.data.data as AdminBackupConfig, secret_access_key: '' }
      Object.assign(form, config)
      savedConfig.value = { ...config }
      notifySuccess(t('admin.settings.backup.saveSuccess'))
      void refresh()
    } catch (err) {
      if (!disposed) reportError(err, 'saveFailed')
    } finally {
      submitting.value = false
    }
  }

  const testConnection = async () => {
    if (!canTest.value) return
    testing.value = true
    try {
      await adminAPI.testBackupConnection()
      if (!disposed) notifySuccess(t('admin.settings.backup.testSuccess'))
    } catch (err) {
      if (!disposed) reportError(err, 'testFailed')
    } finally {
      testing.value = false
    }
  }

  const startBackup = async () => {
    if (!canStart.value) return
    starting.value = true
    const currentSession = session
    try {
      const response = await adminAPI.createBackup()
      if (!isCurrent(currentSession)) return
      const record = response.data.data as AdminBackupRecord
      if (runtime.value) runtime.value = { ...runtime.value, current: record }
      page.value = 1
      notifySuccess(t('admin.settings.backup.startSuccess'))
    } catch (err) {
      if (isCurrent(currentSession)) reportError(err, 'startFailed')
    } finally {
      starting.value = false
      // Also discover a backup that started before a response was lost.
      void refresh()
    }
  }

  const download = async (record: AdminBackupRecord) => {
    if (busy.value || record.status !== 'succeeded') return
    downloadingId.value = record.id
    const currentSession = session
    try {
      const response = await adminAPI.getBackupDownload(record.id)
      if (!isCurrent(currentSession)) return
      const link = document.createElement('a')
      link.href = (response.data.data as { url: string }).url
      link.target = '_blank'
      link.rel = 'noopener noreferrer'
      link.click()
    } catch (err) {
      if (isCurrent(currentSession)) reportError(err, 'downloadFailed')
    } finally {
      downloadingId.value = null
    }
  }

  const remove = async (record: AdminBackupRecord) => {
    if (busy.value || running.value || record.status === 'running') return
    deletingId.value = record.id
    const currentSession = session
    try {
      const confirmed = await confirmAction({
        title: t('admin.settings.backup.deleteTitle'),
        description: t('admin.settings.backup.deleteWarning', { name: record.filename || record.id }),
        confirmText: t('admin.settings.backup.delete'),
        variant: 'destructive',
      })
      if (!confirmed || !isCurrent(currentSession)) return
      await adminAPI.deleteBackup(record.id)
      if (isCurrent(currentSession)) {
        notifySuccess(t('admin.settings.backup.deleteSuccess'))
        void refresh()
      }
    } catch (err) {
      if (isCurrent(currentSession)) reportError(err, 'deleteFailed')
    } finally {
      deletingId.value = null
    }
  }

  const changePage = (value: number) => {
    if (value === page.value || value < 1 || value > totalPage.value) return
    page.value = value
    void refresh()
  }

  watch(() => toValue(active), (visible) => {
    session++
    stopTimer()
    loading.value = false
    refreshing.value = false
    if (visible) void reload()
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => {
    disposed = true
    session++
    stopTimer()
  })

  return {
    form, loading, refreshing, submitting, testing, starting, deletingId, downloadingId,
    runtime, records, recordsLoaded, configError, statusError, recordsError,
    page, pageSize, total, totalPage, running, busy, dirty, canSave, canTest, canStart,
    reload, refresh, save, testConnection, startBackup, download, remove, changePage,
  }
}
