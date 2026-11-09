<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ShieldCheck } from 'lucide-vue-next'
import { getActiveTemplate } from '../../templates/registry'

defineProps<{ title: string; description: string; detail?: boolean }>()
const { t } = useI18n()
const isVault = computed(() => getActiveTemplate() === 'vault')
</script>

<template>
  <div class="min-h-[70vh] pb-16 text-foreground" :class="isVault ? 'pt-8' : 'pt-28'">
    <div class="mx-auto max-w-5xl px-4 sm:px-6">
      <router-link
        v-if="detail"
        to="/tools"
        class="mb-6 inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft class="h-4 w-4" aria-hidden="true" />
        {{ t('tools.backToTools') }}
      </router-link>
      <header class="mb-8">
        <p class="mb-3 inline-flex items-center gap-2 text-sm font-medium text-primary">
          <ShieldCheck class="h-4 w-4" aria-hidden="true" />{{ t('tools.localBadge') }}
        </p>
        <h1 class="text-3xl font-bold tracking-tight sm:text-4xl">{{ title }}</h1>
        <p class="mt-3 max-w-2xl text-sm leading-7 text-muted-foreground sm:text-base">{{ description }}</p>
      </header>
      <slot />
      <p class="mt-6 flex items-start gap-2 text-xs leading-6 text-muted-foreground">
        <ShieldCheck class="mt-1 h-4 w-4 shrink-0" aria-hidden="true" />{{ t('tools.privacy') }}
      </p>
    </div>
  </div>
</template>
