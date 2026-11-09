<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ArrowUpRight, KeyRound, TextCursorInput } from 'lucide-vue-next'
import ToolPage from '../../components/tools/ToolPage.vue'
import { Card } from '../../components/ui/card'
import { usePageSeo } from '../../composables/usePageSeo'

const { t } = useI18n()
const tools = [
  { key: 'totp', path: '/tools/2fa', icon: KeyRound },
  { key: 'text', path: '/tools/text', icon: TextCursorInput },
] as const
usePageSeo({ title: () => t('tools.title'), description: () => t('tools.description'), canonicalPath: () => '/tools' })
</script>

<template>
  <ToolPage :title="t('tools.title')" :description="t('tools.description')">
    <div class="grid gap-5 sm:grid-cols-2">
      <RouterLink v-for="tool in tools" :key="tool.key" :to="tool.path" class="group rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <Card class="h-full p-6 transition-colors group-hover:border-primary/50 sm:p-8">
          <div class="mb-6 flex items-center justify-between">
            <span class="rounded-xl bg-primary/10 p-3 text-primary"><component :is="tool.icon" class="h-6 w-6" aria-hidden="true" /></span>
            <ArrowUpRight class="h-5 w-5 text-muted-foreground transition-colors group-hover:text-primary" aria-hidden="true" />
          </div>
          <h2 class="text-xl font-semibold">{{ t(`tools.${tool.key}.title`) }}</h2>
          <p class="mt-3 text-sm leading-7 text-muted-foreground">{{ t(`tools.${tool.key}.description`) }}</p>
          <p class="mt-6 text-sm font-medium text-primary">{{ t('tools.open') }} <span aria-hidden="true">→</span></p>
        </Card>
      </RouterLink>
    </div>
  </ToolPage>
</template>
