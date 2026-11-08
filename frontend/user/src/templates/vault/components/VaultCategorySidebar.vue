<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import CategorySidebar from '../../../components/CategorySidebar.vue'
import CategoryTree from '../../../components/CategoryTree.vue'
import { getImageUrl } from '../../../utils/image'
import { useLocalized } from '../../../composables/useProduct'
import { buildCategoryPath, createCategoryMap, flattenCategoryGroups, type CategoryGroup, type PublicCategory } from '../../../utils/category'

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
</script>

<template>
  <aside class="min-w-0 lg:sticky lg:top-[88px]">
    <div class="rounded-xl border bg-card p-2.5 sm:p-4">
      <div class="mb-3 hidden items-center gap-2 px-1 lg:flex">
        <span class="h-4 w-1 flex-none rounded-full bg-primary"></span>
        <h4 class="text-sm font-bold">{{ t('products.categories') }}</h4>
      </div>

      <div class="space-y-2 lg:hidden">
        <CategorySidebar
          :categories="categoryGroups"
          :selected-category="selectedCategory"
          :expanded-parent-ids="expandedParentIds"
          :show-drawer="drawerOpen"
          mobile-only
          @update:show-drawer="drawerOpen = $event"
          @select-category="emit('select', $event)"
          @toggle-parent="emit('toggle', $event)"
        />
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
