<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMediaQuery } from '@vueuse/core'
import { DialogClose, DialogContent, DialogOverlay, DialogPortal, DialogRoot, DialogTitle, DialogTrigger } from 'reka-ui'
import { Filter, X } from 'lucide-vue-next'
import { Button } from '../../../components/ui/button'
import CategoryTree from '../../../components/CategoryTree.vue'
import { getImageUrl } from '../../../utils/image'
import { useLocalized } from '../../../composables/useProduct'
import { buildCategoryPath, createCategoryMap, flattenCategoryGroups, getCategoryAncestors, type CategoryGroup, type PublicCategory } from '../../../utils/category'

const props = defineProps<{
  categoryGroups: CategoryGroup[]
  selectedCategory: number | null
  expandedParentIds: number[]
}>()
const emit = defineEmits<{ select: [id: number | null]; toggle: [id: number] }>()
const drawerOpen = ref(false)
const { t } = useI18n()
const { getLocalizedText } = useLocalized()
const catName = (category: PublicCategory) => getLocalizedText(category.name) || category.slug || ''
const categoryMap = computed(() => createCategoryMap(flattenCategoryGroups(props.categoryGroups)))
const selectedPath = computed(() => buildCategoryPath(categoryMap.value.get(props.selectedCategory || 0), categoryMap.value, getLocalizedText))
// 抽屉复用递归分类树，恢复深层链接的祖先路径，并在进入桌面布局时释放模态焦点。
const isDesktop = useMediaQuery('(min-width: 1024px)')
watch(isDesktop, (desktop) => { if (desktop) drawerOpen.value = false })
watch(drawerOpen, (open) => {
  if (!open) return
  for (const ancestor of getCategoryAncestors(categoryMap.value.get(props.selectedCategory || 0), categoryMap.value)) {
    if (!props.expandedParentIds.includes(ancestor.id)) emit('toggle', ancestor.id)
  }
})
const selectMobileCategory = (id: number | null) => {
  emit('select', id)
  drawerOpen.value = false
}
</script>

<template>
  <aside class="min-w-0 lg:sticky lg:top-[88px]">
    <div class="rounded-xl border bg-card p-2.5 sm:p-4">
      <div class="mb-3 hidden items-center gap-2 px-1 lg:flex">
        <span class="h-4 w-1 flex-none rounded-full bg-primary"></span>
        <h4 class="text-sm font-bold">{{ t('products.categories') }}</h4>
      </div>

      <div class="space-y-2 lg:hidden">
        <DialogRoot v-model:open="drawerOpen">
          <DialogTrigger as-child>
            <Button variant="secondary" class="min-h-11 gap-2 rounded-xl text-sm font-medium">
              <Filter class="h-4 w-4" />{{ t('products.browseCategories') }}
            </Button>
          </DialogTrigger>
          <DialogPortal>
            <DialogOverlay class="fixed inset-0 z-[80] bg-black/50" />
            <DialogContent :aria-describedby="undefined" class="fixed bottom-0 left-0 top-0 z-[90] w-80 max-w-[88vw] overflow-y-auto overscroll-contain border-r bg-card p-4 text-foreground shadow-xl outline-none pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1rem,env(safe-area-inset-bottom))]">
              <div class="mb-4 flex items-center justify-between gap-2">
                <DialogTitle class="text-base font-bold">{{ t('products.categories') }}</DialogTitle>
                <DialogClose as-child>
                  <Button variant="ghost" size="icon" class="min-h-11 min-w-11" :aria-label="t('common.close')"><X class="h-5 w-5" /></Button>
                </DialogClose>
              </div>
              <p v-if="selectedPath" class="mb-4 break-words text-sm text-muted-foreground">{{ selectedPath }}</p>
              <button type="button" class="mb-1 min-h-11 w-full rounded-xl px-3 py-2 text-left" :class="selectedCategory === null ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary'" @click="selectMobileCategory(null)">{{ t('products.allCategories') }}</button>
              <CategoryTree :categories="categoryGroups" :selected-category="selectedCategory" :expanded-parent-ids="expandedParentIds" variant="vault" @select="selectMobileCategory" @toggle="emit('toggle', $event)" />
            </DialogContent>
          </DialogPortal>
        </DialogRoot>
        <div class="flex gap-1.5 overflow-x-auto pb-1">
          <button type="button" class="min-h-11 flex-none whitespace-nowrap rounded-full px-3.5 py-2 text-sm font-semibold transition-colors" :class="selectedCategory === null ? 'bg-primary text-white' : 'bg-secondary text-muted-foreground'" @click="emit('select', null)">{{ t('products.allCategories') }}</button>
          <button v-for="category in categoryGroups" :key="category.id" type="button" class="flex min-h-11 max-w-[65vw] flex-none items-center gap-2 rounded-full px-3.5 py-2 text-sm font-semibold transition-colors" :class="selectedCategory === category.id ? 'bg-primary text-white' : 'bg-secondary text-muted-foreground'" @click="emit('select', category.id)">
            <img v-if="category.icon" :src="getImageUrl(category.icon)" alt="" loading="lazy" class="h-5 w-5 flex-none rounded-md object-cover" />
            <span class="min-w-0 truncate">{{ catName(category) }}</span>
          </button>
        </div>
        <p v-if="selectedPath" class="break-words px-1 text-xs text-muted-foreground">{{ selectedPath }}</p>
      </div>

      <div class="hidden max-h-[calc(100dvh-10rem)] overflow-y-auto overscroll-contain lg:block">
        <button type="button" class="mb-1 min-h-11 w-full rounded-lg px-3 py-2 text-left text-sm font-semibold transition-colors" :class="selectedCategory === null ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary'" @click="emit('select', null)">{{ t('products.allCategories') }}</button>
        <CategoryTree :categories="categoryGroups" :selected-category="selectedCategory" :expanded-parent-ids="expandedParentIds" variant="vault" @select="emit('select', $event)" @toggle="emit('toggle', $event)" />
      </div>
    </div>
  </aside>
</template>
