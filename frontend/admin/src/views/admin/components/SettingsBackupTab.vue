<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download, LoaderCircle, Play, RefreshCw, Trash2 } from 'lucide-vue-next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import ListPagination from '@/components/ListPagination.vue'
import { useBackupSettings } from '@/composables/useBackupSettings'
import { formatDate } from '@/utils/format'
import { backupDiagnostic } from '@/utils/backupDiagnostic'
import type { AdminBackupRecord } from '@/api/types'

const props = defineProps<{ active: boolean }>()
const { t, locale } = useI18n()
const {
  form, loading, refreshing, submitting, testing, starting, deletingId, downloadingId,
  runtime, records, recordsLoaded, configError, statusError, recordsError,
  page, total, totalPage, running, busy, dirty, canSave, canTest, canStart,
  reload, refresh, save, testConnection, startBackup, download, remove, changePage,
} = useBackupSettings(() => props.active)

const fieldsDisabled = computed(() => loading.value || busy.value || running.value || !!configError.value || runtime.value?.supported === false)
const runtimeLabel = computed(() => {
  if (statusError.value) return t('admin.settings.backup.statusUnavailable')
  if (!runtime.value) return t('admin.settings.backup.statusUnknown')
  if (!runtime.value.supported) return t('admin.settings.backup.unsupported')
  if (running.value) return t('admin.settings.backup.statuses.running')
  return t(`admin.settings.backup.${runtime.value.ready ? 'ready' : 'notReady'}`)
})
const dateLabel = (value?: string | null) => formatDate(value || undefined) || t('admin.settings.backup.none')
const sizeLabel = (value: number) => t('admin.settings.backup.sizeFormat', { size: new Intl.NumberFormat(locale.value).format(value) })
const statusLabel = (record: AdminBackupRecord) => {
  if (record.status === 'running' || record.status === 'succeeded' || record.status === 'failed') {
    return t(`admin.settings.backup.statuses.${record.status}`)
  }
  return t('admin.settings.backup.statusUnknown')
}
const sourceLabel = (record: AdminBackupRecord) => {
  if (record.source === 'manual' || record.source === 'scheduled') return t(`admin.settings.backup.sources.${record.source}`)
  return t('admin.settings.backup.statusUnknown')
}
const stageLabel = (record: AdminBackupRecord) => {
  if (record.stage === 'exporting' || record.stage === 'uploading') return t(`admin.settings.backup.stages.${record.stage}`)
  return t('admin.settings.backup.processing')
}

defineExpose({ save, submitting, canSave })
</script>

<template>
  <div class="min-w-0 space-y-6">
    <Card>
      <CardHeader>
        <CardTitle>{{ t('admin.settings.backup.storageTitle') }}</CardTitle>
        <CardDescription>{{ t('admin.settings.backup.storageDescription') }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-5">
        <Alert v-if="configError" variant="destructive">
          <AlertTitle>{{ t('admin.settings.backup.loadFailed') }}</AlertTitle>
          <AlertDescription class="break-all">{{ configError }}</AlertDescription>
        </Alert>
        <Button v-if="configError" variant="outline" :disabled="loading" @click="reload">
          {{ t('admin.settings.backup.retry') }}
        </Button>
        <p v-if="loading" role="status" class="text-sm text-muted-foreground">{{ t('admin.settings.backup.loading') }}</p>
        <fieldset :disabled="fieldsDisabled" class="grid min-w-0 gap-5 sm:grid-cols-2">
          <div class="space-y-2 sm:col-span-2">
            <Label for="backup-endpoint">{{ t('admin.settings.backup.endpoint') }}</Label>
            <Input id="backup-endpoint" v-model="form.endpoint" type="url" :placeholder="t('admin.settings.backup.endpointPlaceholder')" autocomplete="off" />
          </div>
          <div class="space-y-2">
            <Label for="backup-bucket">{{ t('admin.settings.backup.bucket') }}</Label>
            <Input id="backup-bucket" v-model="form.bucket" autocomplete="off" />
          </div>
          <div class="space-y-2">
            <Label for="backup-prefix">{{ t('admin.settings.backup.prefix') }}</Label>
            <Input id="backup-prefix" v-model="form.prefix" autocomplete="off" />
          </div>
          <div class="space-y-2">
            <Label for="backup-access-key">{{ t('admin.settings.backup.accessKeyId') }}</Label>
            <Input id="backup-access-key" v-model="form.access_key_id" autocomplete="off" spellcheck="false" />
          </div>
          <div class="space-y-2">
            <Label for="backup-secret-key">{{ t('admin.settings.backup.secretAccessKey') }}</Label>
            <Input
              id="backup-secret-key" v-model="form.secret_access_key" type="password" autocomplete="new-password"
              spellcheck="false" :placeholder="t(`admin.settings.backup.${form.secret_configured ? 'secretKeepPlaceholder' : 'secretPlaceholder'}`)"
              aria-describedby="backup-secret-hint"
            />
            <p id="backup-secret-hint" class="text-xs text-muted-foreground">
              {{ t(`admin.settings.backup.${form.secret_configured ? 'secretConfigured' : 'secretMissing'}`) }}
              {{ t('admin.settings.backup.secretHint') }}
            </p>
          </div>
        </fieldset>
        <Alert>
          <AlertTitle>{{ t('admin.settings.backup.savedConfigTitle') }}</AlertTitle>
          <AlertDescription>{{ t('admin.settings.backup.savedConfigHint') }}</AlertDescription>
        </Alert>
        <p v-if="dirty && !loading && !configError" role="status" class="text-sm text-amber-700 dark:text-amber-400">
          {{ t('admin.settings.backup.unsavedChanges') }}
        </p>
        <div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
          <Button :disabled="!canSave" @click="save">
            <LoaderCircle v-if="submitting" class="h-4 w-4 animate-spin" aria-hidden="true" />
            {{ t(`admin.settings.backup.${submitting ? 'saving' : 'save'}`) }}
          </Button>
          <Button variant="outline" :disabled="!canTest" @click="testConnection">
            <LoaderCircle v-if="testing" class="h-4 w-4 animate-spin" aria-hidden="true" />
            {{ t(`admin.settings.backup.${testing ? 'testing' : 'test'}`) }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle>{{ t('admin.settings.backup.scheduleTitle') }}</CardTitle>
        <CardDescription>{{ t('admin.settings.backup.scheduleDescription') }}</CardDescription>
      </CardHeader>
      <CardContent>
        <fieldset :disabled="fieldsDisabled" class="space-y-5">
          <div class="flex items-start justify-between gap-4 rounded-lg border border-border p-4">
            <div class="space-y-1">
              <Label for="backup-enabled">{{ t('admin.settings.backup.enabled') }}</Label>
              <p id="backup-enabled-hint" class="text-xs text-muted-foreground">{{ t('admin.settings.backup.enabledHint') }}</p>
            </div>
            <Switch id="backup-enabled" v-model="form.enabled" :disabled="fieldsDisabled" aria-describedby="backup-enabled-hint" />
          </div>
          <div class="grid min-w-0 gap-5 sm:grid-cols-3">
            <div class="space-y-2">
              <Label for="backup-daily-time">{{ t('admin.settings.backup.dailyTime') }}</Label>
              <Input id="backup-daily-time" v-model="form.daily_time" type="time" step="60" />
            </div>
            <div class="space-y-2">
              <Label for="backup-timezone">{{ t('admin.settings.backup.timezone') }}</Label>
              <Input id="backup-timezone" v-model="form.timezone" :placeholder="t('admin.settings.backup.timezonePlaceholder')" />
            </div>
            <div class="space-y-2">
              <Label for="backup-retain-count">{{ t('admin.settings.backup.retainCount') }}</Label>
              <Input id="backup-retain-count" v-model="form.retain_count" type="number" min="1" max="1000" step="1" inputmode="numeric" aria-describedby="backup-retain-hint" />
            </div>
          </div>
          <p id="backup-retain-hint" class="text-xs text-muted-foreground">{{ t('admin.settings.backup.retainHint') }}</p>
        </fieldset>
      </CardContent>
    </Card>

    <Card class="min-w-0" :aria-busy="refreshing">
      <CardHeader class="gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div class="min-w-0 space-y-2">
          <CardTitle>{{ t('admin.settings.backup.runtimeTitle') }}</CardTitle>
          <CardDescription>{{ t('admin.settings.backup.runtimeDescription') }}</CardDescription>
          <Badge :variant="statusError || runtime?.supported === false ? 'destructive' : 'secondary'">{{ runtimeLabel }}</Badge>
        </div>
        <div class="flex shrink-0 flex-col gap-2 sm:flex-row">
          <Button variant="outline" :disabled="refreshing || busy" @click="refresh">
            <RefreshCw class="h-4 w-4" :class="{ 'animate-spin': refreshing }" aria-hidden="true" />
            {{ t('admin.settings.backup.refresh') }}
          </Button>
          <Button :disabled="!canStart" @click="startBackup">
            <LoaderCircle v-if="starting" class="h-4 w-4 animate-spin" aria-hidden="true" />
            <Play v-else class="h-4 w-4" aria-hidden="true" />
            {{ t(`admin.settings.backup.${starting ? 'starting' : 'start'}`) }}
          </Button>
        </div>
      </CardHeader>
      <CardContent class="min-w-0 space-y-5">
        <Alert v-if="statusError" variant="destructive">
          <AlertTitle>{{ t('admin.settings.backup.statusFailed') }}</AlertTitle>
          <AlertDescription class="break-all">{{ statusError }}</AlertDescription>
        </Alert>
        <Alert v-if="runtime && !runtime.supported">
          <AlertTitle>{{ t('admin.settings.backup.unsupported') }}</AlertTitle>
          <AlertDescription>{{ t('admin.settings.backup.unsupportedHint') }}</AlertDescription>
        </Alert>
        <Alert v-if="runtime?.error" variant="destructive">
          <AlertTitle>{{ t('admin.settings.backup.runtimeError') }}</AlertTitle>
          <AlertDescription class="whitespace-pre-wrap break-all">{{ backupDiagnostic(runtime.error, t) }}</AlertDescription>
        </Alert>
        <Alert v-if="runtime?.cleanup_error" variant="destructive">
          <AlertTitle>{{ t('admin.settings.backup.cleanupError') }}</AlertTitle>
          <AlertDescription class="whitespace-pre-wrap break-all">{{ backupDiagnostic(runtime.cleanup_error, t) }}</AlertDescription>
        </Alert>
        <dl class="grid min-w-0 gap-4 text-sm sm:grid-cols-2">
          <div class="min-w-0 space-y-1">
            <dt class="text-muted-foreground">{{ t('admin.settings.backup.pgDumpVersion') }}</dt>
            <dd class="break-all font-mono text-xs">{{ runtime?.pg_dump_version || t('admin.settings.backup.none') }}</dd>
          </div>
          <div class="min-w-0 space-y-1">
            <dt class="text-muted-foreground">{{ t('admin.settings.backup.serverVersion') }}</dt>
            <dd class="break-all font-mono text-xs">{{ runtime?.server_version || t('admin.settings.backup.none') }}</dd>
          </div>
          <div class="min-w-0 space-y-1">
            <dt class="text-muted-foreground">{{ t('admin.settings.backup.nextRun') }}</dt>
            <dd class="break-words">{{ dateLabel(runtime?.next_run) }}</dd>
          </div>
          <div class="min-w-0 space-y-1">
            <dt class="text-muted-foreground">{{ t('admin.settings.backup.lastSuccess') }}</dt>
            <dd class="break-words">{{ dateLabel(runtime?.last_success?.finished_at) }}</dd>
          </div>
        </dl>
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.backup.localTimeHint') }}</p>
        <Alert v-if="running">
          <AlertTitle>{{ t('admin.settings.backup.runningTitle') }}</AlertTitle>
          <AlertDescription class="space-y-1">
            <p>{{ t('admin.settings.backup.pollingHint') }}</p>
            <p v-if="runtime?.current?.status === 'running'" class="break-all">
              {{ runtime.current.filename || runtime.current.id }} · {{ stageLabel(runtime.current) }}
            </p>
          </AlertDescription>
        </Alert>

        <div class="space-y-4">
          <h3 class="text-sm font-semibold">{{ t('admin.settings.backup.recordsTitle') }}</h3>
          <Alert v-if="recordsError" variant="destructive">
            <AlertTitle>{{ t('admin.settings.backup.recordsFailed') }}</AlertTitle>
            <AlertDescription class="break-all">{{ recordsError }}</AlertDescription>
          </Alert>
          <p v-if="!recordsLoaded && refreshing" role="status" class="py-6 text-center text-sm text-muted-foreground">{{ t('admin.settings.backup.loading') }}</p>
          <p v-else-if="recordsLoaded && !records.length && !recordsError" class="rounded-lg border border-dashed py-8 text-center text-sm text-muted-foreground">
            {{ t('admin.settings.backup.empty') }}
          </p>
          <article v-for="record in records" :key="record.id" class="min-w-0 space-y-4 rounded-lg border border-border p-4">
            <div class="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div class="min-w-0 space-y-2">
                <p class="break-all font-mono text-xs font-medium">{{ record.filename || record.id }}</p>
                <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <Badge :variant="record.status === 'failed' ? 'destructive' : record.status === 'succeeded' ? 'default' : 'secondary'">{{ statusLabel(record) }}</Badge>
                  <span>{{ sourceLabel(record) }}</span>
                  <span v-if="record.status === 'running'">{{ stageLabel(record) }}</span>
                </div>
              </div>
              <div class="flex shrink-0 flex-wrap gap-2">
                <Button size="sm" variant="outline" :disabled="record.status !== 'succeeded' || busy" @click="download(record)">
                  <LoaderCircle v-if="downloadingId === record.id" class="h-4 w-4 animate-spin" aria-hidden="true" />
                  <Download v-else class="h-4 w-4" aria-hidden="true" />
                  {{ t('admin.settings.backup.download') }}
                </Button>
                <Button size="sm" variant="destructive" :disabled="running || busy || record.status === 'running'" @click="remove(record)">
                  <LoaderCircle v-if="deletingId === record.id" class="h-4 w-4 animate-spin" aria-hidden="true" />
                  <Trash2 v-else class="h-4 w-4" aria-hidden="true" />
                  {{ t('admin.settings.backup.delete') }}
                </Button>
              </div>
            </div>
            <dl class="grid min-w-0 grid-cols-2 gap-3 text-xs lg:grid-cols-4">
              <div class="min-w-0 space-y-1">
                <dt class="text-muted-foreground">{{ t('admin.settings.backup.createdAt') }}</dt>
                <dd class="break-words">{{ dateLabel(record.created_at) }}</dd>
              </div>
              <div class="min-w-0 space-y-1">
                <dt class="text-muted-foreground">{{ t('admin.settings.backup.startedAt') }}</dt>
                <dd class="break-words">{{ dateLabel(record.started_at) }}</dd>
              </div>
              <div class="min-w-0 space-y-1">
                <dt class="text-muted-foreground">{{ t('admin.settings.backup.finishedAt') }}</dt>
                <dd class="break-words">{{ dateLabel(record.finished_at) }}</dd>
              </div>
              <div class="min-w-0 space-y-1">
                <dt class="text-muted-foreground">{{ t('admin.settings.backup.size') }}</dt>
                <dd class="break-words">{{ sizeLabel(record.size) }}</dd>
              </div>
            </dl>
            <Alert v-if="record.error" :variant="record.status === 'failed' ? 'destructive' : 'default'">
              <AlertTitle>{{ t('admin.settings.backup.recordError') }}</AlertTitle>
              <AlertDescription class="whitespace-pre-wrap break-all">{{ backupDiagnostic(record.error, t) }}</AlertDescription>
            </Alert>
            <details class="min-w-0 text-xs">
              <summary class="cursor-pointer text-muted-foreground">{{ t('admin.settings.backup.details') }}</summary>
              <dl class="mt-3 grid min-w-0 gap-3 sm:grid-cols-2">
                <div v-for="(value, key) in { id: record.id, objectKey: record.object_key, sha256: record.sha256, endpoint: record.endpoint, bucket: record.bucket }" :key="key" class="min-w-0 space-y-1" :class="{ 'sm:col-span-2': key === 'sha256' || key === 'objectKey' }">
                  <dt class="text-muted-foreground">{{ t(`admin.settings.backup.${key}`) }}</dt>
                  <dd class="break-all font-mono">{{ value || t('admin.settings.backup.none') }}</dd>
                </div>
              </dl>
            </details>
          </article>
        </div>
      </CardContent>
      <ListPagination :page="page" :total-page="totalPage" :total="total" @change-page="changePage" />
    </Card>
  </div>
</template>
