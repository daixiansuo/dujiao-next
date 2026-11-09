<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowUp, Copy, Trash2 } from 'lucide-vue-next'
import ToolPage from '../../components/tools/ToolPage.vue'
import FormField from '../../components/FormField.vue'
import { Input } from '../../components/ui/input'
import { Textarea } from '../../components/ui/textarea'
import { Button } from '../../components/ui/button'
import { Card } from '../../components/ui/card'
import { usePageSeo } from '../../composables/usePageSeo'
import { toast } from '../../composables/useToast'
import { copyText } from '../../utils/clipboard'
import { processText, type TextOperation } from '../../utils/textTools'

const { t } = useI18n()
const input = ref('')
const output = ref('')
const delimiter = ref('')
const search = ref('')
const replacement = ref('')
const error = ref('')
const operation = ref<TextOperation>('deduplicate')
const operations: TextOperation[] = ['split', 'replace', 'deduplicate', 'trim', 'reverse']
const run = () => {
  const result = processText(input.value, operation.value, { delimiter: delimiter.value, search: search.value, replacement: replacement.value })
  error.value = result.error || ''
  output.value = result.output ?? ''
}
const clear = () => {
  input.value = ''
  output.value = ''
  delimiter.value = ''
  search.value = ''
  replacement.value = ''
  error.value = ''
}
const useOutput = () => {
  input.value = output.value
  output.value = ''
  error.value = ''
}
const copyOutput = async () => {
  try {
    await copyText(output.value)
    toast.success(t('tools.copied'))
  } catch {
    toast.error(t('tools.copyFailed'))
  }
}
usePageSeo({ title: () => t('tools.text.title'), description: () => t('tools.text.description'), canonicalPath: () => '/tools/text' })
</script>

<template>
  <ToolPage :title="t('tools.text.title')" :description="t('tools.text.description')" detail>
    <Card class="p-5 sm:p-7">
      <div class="grid gap-6 md:grid-cols-2">
        <FormField v-slot="field" :label="t('tools.text.inputLabel')">
          <Textarea :id="field.id" v-model="input" rows="10" class="resize-y font-mono text-sm" :placeholder="t('tools.text.inputPlaceholder')" autocomplete="off" :spellcheck="false" />
        </FormField>
        <FormField v-slot="field" :label="t('tools.text.outputLabel')" :hint="t('tools.text.outputHint')">
          <Textarea :id="field.id" :model-value="output" rows="10" readonly class="resize-y bg-secondary/30 font-mono text-sm" :aria-describedby="field.describedBy" :placeholder="t('tools.text.outputPlaceholder')" :spellcheck="false" />
        </FormField>
      </div>
      <fieldset class="mt-6">
        <legend class="mb-3 text-sm font-medium">{{ t('tools.text.operationLabel') }}</legend>
        <div class="flex flex-wrap gap-2">
          <Button v-for="item in operations" :key="item" :variant="operation === item ? 'default' : 'outline'" :aria-pressed="operation === item" @click="operation = item; error = ''">{{ t(`tools.text.operations.${item}`) }}</Button>
        </div>
      </fieldset>
      <p class="mt-3 text-xs leading-6 text-muted-foreground">{{ t(`tools.text.hints.${operation}`) }}</p>
      <div v-if="operation === 'split'" class="mt-4 max-w-md">
        <FormField v-slot="field" :label="t('tools.text.delimiterLabel')" :error="error ? t(`tools.text.errors.${error}`) : ''">
          <Input :id="field.id" v-model="delimiter" :aria-invalid="field.hasError" :aria-describedby="field.describedBy" :placeholder="t('tools.text.delimiterPlaceholder')" autocomplete="off" @update:model-value="error = ''" />
        </FormField>
      </div>
      <div v-if="operation === 'replace'" class="mt-4 grid gap-4 sm:grid-cols-2">
        <FormField v-slot="field" :label="t('tools.text.searchLabel')" :error="error ? t(`tools.text.errors.${error}`) : ''">
          <Input :id="field.id" v-model="search" :aria-invalid="field.hasError" :aria-describedby="field.describedBy" autocomplete="off" @update:model-value="error = ''" />
        </FormField>
        <FormField v-slot="field" :label="t('tools.text.replacementLabel')" :hint="t('tools.text.replacementHint')">
          <Input :id="field.id" v-model="replacement" :aria-describedby="field.describedBy" autocomplete="off" />
        </FormField>
      </div>
      <div class="mt-6 flex flex-wrap gap-2 border-t pt-5">
        <Button :disabled="!input" @click="run">{{ t('tools.text.run') }}</Button>
        <Button variant="outline" :disabled="!output" @click="copyOutput"><Copy class="mr-2 h-4 w-4" aria-hidden="true" />{{ t('tools.text.copy') }}</Button>
        <Button variant="outline" :disabled="!output" @click="useOutput"><ArrowUp class="mr-2 h-4 w-4" aria-hidden="true" />{{ t('tools.text.useOutput') }}</Button>
        <Button variant="ghost" :disabled="!input && !output && !delimiter && !search && !replacement" @click="clear"><Trash2 class="mr-2 h-4 w-4" aria-hidden="true" />{{ t('tools.clear') }}</Button>
      </div>
    </Card>
  </ToolPage>
</template>
