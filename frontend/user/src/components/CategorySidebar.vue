<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMediaQuery } from '@vueuse/core'
import { DialogClose, DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle, DialogTrigger } from 'reka-ui'
import { Filter, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import CategoryTree from './CategoryTree.vue'
import { useLocalized } from '../composables/useProduct'
import { buildCategoryPath, createCategoryMap, flattenCategoryGroups, getCategoryAncestors, type CategoryGroup } from '../utils/category'

const props = defineProps<{
  categories: CategoryGroup[]
  selectedCategory: number | null
  expandedParentIds: number[]
  showDrawer: boolean
  compact?: boolean
  showSearch?: boolean
  searchQuery?: string
  mobileOnly?: boolean
}>()

const emit = defineEmits<{
  'selectCategory': [categoryId: number | null, closeDrawer?: boolean]
  'toggleParent': [categoryId: number]
  'update:showDrawer': [value: boolean]
  'update:searchQuery': [value: string]
  'clearSearch': []
}>()

const { t } = useI18n()
const { getLocalizedText } = useLocalized()
const categoryMap = computed(() => createCategoryMap(flattenCategoryGroups(props.categories)))
const selectedPath = computed(() => buildCategoryPath(categoryMap.value.get(props.selectedCategory || 0), categoryMap.value, getLocalizedText))
const isDesktop = useMediaQuery('(min-width: 1024px)')
watch(isDesktop, (desktop) => {
  if (desktop && props.showDrawer) emit('update:showDrawer', false)
})
watch(() => props.showDrawer, (open) => {
  if (!open) return
  const selected = categoryMap.value.get(props.selectedCategory || 0)
  for (const ancestor of getCategoryAncestors(selected, categoryMap.value)) {
    if (!props.expandedParentIds.includes(ancestor.id)) emit('toggleParent', ancestor.id)
  }
})

const selectMobileCategory = (id: number | null) => {
  emit('selectCategory', id, true)
  emit('update:showDrawer', false)
}
</script>

<template>
  <DialogRoot :open="showDrawer" @update:open="emit('update:showDrawer', $event)">
    <DialogTrigger as-child>
      <Button variant="secondary" class="min-h-11 gap-2 rounded-xl text-sm font-medium lg:hidden">
        <Filter class="h-4 w-4" />
        {{ mobileOnly ? t('products.browseCategories') : t('products.filter') }}
        <span v-if="selectedCategory" class="h-2 w-2 rounded-full bg-primary"></span>
      </Button>
    </DialogTrigger>
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-[80] bg-black/50" />
      <DialogContent
        :aria-describedby="undefined"
        class="fixed bottom-0 left-0 top-0 z-[90] w-80 max-w-[88vw] overflow-y-auto overscroll-contain border-r bg-card p-4 text-foreground shadow-xl outline-none pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1rem,env(safe-area-inset-bottom))]"
      >
        <div class="mb-4 flex items-center justify-between gap-2">
          <DialogTitle class="text-base font-bold">{{ showSearch ? t('products.filter') : t('products.categories') }}</DialogTitle>
          <DialogClose as-child>
            <Button variant="ghost" size="icon" class="min-h-11 min-w-11" :aria-label="t('common.close')"><X class="h-5 w-5" /></Button>
          </DialogClose>
        </div>
        <p v-if="selectedPath" class="mb-4 break-words text-sm text-muted-foreground">{{ selectedPath }}</p>
        <div v-if="showSearch" class="mb-5">
          <label class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{{ t('products.searchLabel') }}</label>
          <div class="mt-3 flex items-center gap-2">
            <Input :model-value="searchQuery" @update:model-value="emit('update:searchQuery', String($event))" type="text" class="min-w-0 flex-1" :placeholder="t('products.searchPlaceholder')" />
            <Button v-if="searchQuery" variant="secondary" size="sm" type="button" class="shrink-0 whitespace-nowrap rounded-xl text-xs" @click="emit('clearSearch')">{{ t('common.cancel') }}</Button>
          </div>
        </div>
        <button type="button" class="mb-1 min-h-11 w-full rounded-xl px-3 py-2 text-left transition-colors" :class="selectedCategory === null ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary'" @click="selectMobileCategory(null)">{{ t('products.allCategories') }}</button>
        <CategoryTree :categories="categories" :selected-category="selectedCategory" :expanded-parent-ids="expandedParentIds" @select="selectMobileCategory" @toggle="emit('toggleParent', $event)" />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>

  <aside v-if="!mobileOnly" class="hidden flex-shrink-0 lg:block" :class="compact ? 'lg:w-60' : 'lg:w-64'">
    <div class="sticky top-24 max-h-[calc(100dvh-7rem)] overflow-y-auto overscroll-contain rounded-2xl border bg-card backdrop-blur-xl" :class="compact ? 'p-4' : 'p-5'">
      <div v-if="showSearch" :class="compact ? 'mb-4' : 'mb-6'">
        <label class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{{ t('products.searchLabel') }}</label>
        <div class="mt-3 flex items-center gap-2">
          <Input :model-value="searchQuery" @update:model-value="emit('update:searchQuery', String($event))" type="text" class="min-w-0 flex-1" :placeholder="t('products.searchPlaceholder')" />
          <Button v-if="searchQuery" variant="secondary" size="sm" type="button" class="shrink-0 whitespace-nowrap rounded-xl text-xs" @click="emit('clearSearch')">{{ t('common.cancel') }}</Button>
        </div>
      </div>
      <h2 class="mb-4 flex items-center gap-2 font-bold text-foreground" :class="compact ? 'text-base' : 'text-lg'">
        <span class="h-5 w-1 rounded-full bg-primary"></span>{{ t('products.categories') }}
      </h2>
      <button type="button" class="mb-1 min-h-11 w-full rounded-xl px-3 py-2 text-left transition-colors" :class="selectedCategory === null ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary'" @click="emit('selectCategory', null)">{{ t('products.allCategories') }}</button>
      <CategoryTree :categories="categories" :selected-category="selectedCategory" :expanded-parent-ids="expandedParentIds" :compact="compact" @select="emit('selectCategory', $event)" @toggle="emit('toggleParent', $event)" />
    </div>
  </aside>
</template>
