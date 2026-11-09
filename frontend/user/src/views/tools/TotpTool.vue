<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Trash2 } from 'lucide-vue-next'
import ToolPage from '../../components/tools/ToolPage.vue'
import FormField from '../../components/FormField.vue'
import { Textarea } from '../../components/ui/textarea'
import { Button } from '../../components/ui/button'
import { Card } from '../../components/ui/card'
import { usePageSeo } from '../../composables/usePageSeo'
import { useTotpTool } from '../../composables/useTotpTool'
import { toast } from '../../composables/useToast'
import { copyText } from '../../utils/clipboard'

const { t } = useI18n()
const { input, code, remaining, error, refresh } = useTotpTool()
const errorMessage = computed(() => error.value ? t(`tools.totp.errors.${error.value}`) : '')
const copyCode = async () => {
  const value = await refresh()
  if (!value) return
  try {
    await copyText(value)
    toast.success(t('tools.copied'))
  } catch {
    toast.error(t('tools.copyFailed'))
  }
}
usePageSeo({ title: () => t('tools.totp.title'), description: () => t('tools.totp.description'), canonicalPath: () => '/tools/2fa' })
</script>

<template>
  <ToolPage :title="t('tools.totp.title')" :description="t('tools.totp.description')" detail>
    <div class="grid items-start gap-5 md:grid-cols-2">
      <Card class="p-5 sm:p-7">
        <FormField v-slot="field" :label="t('tools.totp.inputLabel')" :error="errorMessage" :hint="t('tools.totp.inputHint')">
          <Textarea :id="field.id" v-model="input" rows="5" class="resize-y break-all font-mono text-sm" :aria-invalid="field.hasError" :aria-describedby="field.describedBy" :placeholder="t('tools.totp.placeholder')" autocomplete="off" autocapitalize="off" :spellcheck="false" />
        </FormField>
        <div class="mt-5 flex justify-end">
          <Button variant="outline" :disabled="!input" @click="input = ''"><Trash2 class="mr-2 h-4 w-4" aria-hidden="true" />{{ t('tools.clear') }}</Button>
        </div>
      </Card>
      <Card class="overflow-hidden p-5 text-center sm:p-7">
        <h2 class="text-sm font-medium text-muted-foreground">{{ t('tools.totp.codeLabel') }}</h2>
        <div class="my-6 min-h-14 font-mono text-4xl font-semibold tracking-[0.15em] sm:text-5xl" role="status" aria-live="polite" aria-atomic="true">{{ code || '••••••' }}</div>
        <template v-if="code">
          <p class="text-sm text-muted-foreground">{{ t('tools.totp.remaining', { seconds: remaining }) }}</p>
          <div class="my-4 h-1.5 overflow-hidden rounded-full bg-secondary" role="progressbar" :aria-label="t('tools.totp.countdown')" :aria-valuenow="remaining" :aria-valuemin="0" :aria-valuemax="30">
            <div class="h-full rounded-full bg-primary" :style="{ width: `${remaining / 30 * 100}%` }" />
          </div>
        </template>
        <p v-else class="mb-5 text-sm text-muted-foreground">{{ t('tools.totp.waiting') }}</p>
        <Button class="w-full" :disabled="!code" @click="copyCode"><Copy class="mr-2 h-4 w-4" aria-hidden="true" />{{ t('tools.totp.copy') }}</Button>
      </Card>
    </div>
    <p class="mt-5 text-xs leading-6 text-muted-foreground">{{ t('tools.totp.compatibility') }}</p>
  </ToolPage>
</template>
