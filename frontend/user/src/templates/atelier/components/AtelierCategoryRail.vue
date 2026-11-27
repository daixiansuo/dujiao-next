<template>
  <div class="relative min-w-0">
    <nav ref="railRef" class="category-rail overflow-x-auto overscroll-x-contain py-1" :aria-label="label" @scroll.passive="updateEdges">
      <div ref="contentRef" class="flex w-max min-w-full items-center gap-1 px-1">
        <slot />
      </div>
    </nav>
    <div v-if="canPrevious" class="pointer-events-none absolute inset-y-0 left-0 flex w-10 items-center bg-gradient-to-r from-background via-background/95 to-transparent">
      <button type="button" class="pointer-events-auto grid h-7 w-7 place-items-center rounded-full border bg-card text-muted-foreground shadow-xs transition-colors hover:text-primary focus-visible:outline-2 focus-visible:outline-primary" :aria-label="t('atelier.categoryPrevious')" @click="scrollPage(-1)">
        <ChevronLeft class="h-3.5 w-3.5" aria-hidden="true" />
      </button>
    </div>
    <div v-if="canNext" class="pointer-events-none absolute inset-y-0 right-0 flex w-10 items-center justify-end bg-gradient-to-l from-background via-background/95 to-transparent">
      <button type="button" class="pointer-events-auto grid h-7 w-7 place-items-center rounded-full border bg-card text-muted-foreground shadow-xs transition-colors hover:text-primary focus-visible:outline-2 focus-visible:outline-primary" :aria-label="t('atelier.categoryNext')" @click="scrollPage(1)">
        <ChevronRight class="h-3.5 w-3.5" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useResizeObserver } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'

const props = defineProps<{ label: string; activeKey: number | string }>()
const { t } = useI18n()
const railRef = ref<HTMLElement | null>(null)
const contentRef = ref<HTMLElement | null>(null)
const canPrevious = ref(false)
const canNext = ref(false)

const updateEdges = () => {
  const rail = railRef.value
  if (!rail) return
  canPrevious.value = rail.scrollLeft > 1
  canNext.value = rail.scrollWidth - rail.clientWidth - rail.scrollLeft > 1
}

// 只调整轨道横向位置，避免深层链接恢复或键盘选择时把整页滚动到分类处。
const revealSelected = async () => {
  await nextTick()
  const rail = railRef.value
  const selected = rail?.querySelector<HTMLElement>('[aria-pressed="true"]')
  if (rail && selected) {
    const bounds = rail.getBoundingClientRect()
    const item = selected.getBoundingClientRect()
    const leftInset = rail.scrollLeft > 1 ? 36 : 0
    const rightInset = rail.scrollWidth - rail.clientWidth - rail.scrollLeft > 1 ? 36 : 0
    if (item.left < bounds.left + leftInset) rail.scrollLeft += item.left - bounds.left - leftInset
    else if (item.right > bounds.right - rightInset) rail.scrollLeft += item.right - bounds.right + rightInset
  }
  updateEdges()
}

const scrollPage = (direction: number) => {
  const rail = railRef.value
  if (!rail) return
  rail.scrollBy({
    left: direction * rail.clientWidth * 0.75,
    behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
  })
}

watch(() => props.activeKey, revealSelected, { immediate: true, flush: 'post' })
useResizeObserver([railRef, contentRef], revealSelected)
</script>

<style scoped>
.category-rail {
  scrollbar-width: none;
  scroll-padding-inline: 2.25rem;
}
.category-rail::-webkit-scrollbar {
  display: none;
}
</style>
