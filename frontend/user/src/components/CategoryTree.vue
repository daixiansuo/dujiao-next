<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ChevronRight } from 'lucide-vue-next'
import { useLocalized } from '../composables/useProduct'
import { getImageUrl } from '../utils/image'
import type { CategoryGroup } from '../utils/category'

withDefaults(defineProps<{
  categories: CategoryGroup[]
  selectedCategory: number | null
  expandedParentIds: number[]
  depth?: number
  compact?: boolean
  variant?: 'classic' | 'vault'
}>(), { depth: 0, variant: 'classic' })

const emit = defineEmits<{ select: [id: number]; toggle: [id: number] }>()
const { t } = useI18n()
const { getLocalizedText } = useLocalized()
const label = (category: CategoryGroup) => getLocalizedText(category.name) || category.slug || ''
</script>

<template>
  <ul class="min-w-0 space-y-1">
    <li v-for="category in categories" :key="category.id" class="min-w-0">
      <div class="flex min-w-0 items-center gap-1" :style="{ paddingLeft: `${depth * 12}px` }">
        <button
          type="button"
          class="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-xl border border-transparent px-3 py-2 text-left transition-colors"
          :class="[
            compact || variant === 'vault' ? 'text-sm' : '',
            variant === 'vault' ? 'font-semibold' : '',
            selectedCategory === category.id
              ? 'bg-primary/10 text-primary'
              : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
          ]"
          :title="label(category)"
          :aria-current="selectedCategory === category.id ? 'true' : undefined"
          @click="emit('select', category.id)"
        >
          <img v-if="category.icon" :src="getImageUrl(category.icon)" alt="" loading="lazy" class="h-5 w-5 flex-none rounded object-cover" />
          <span class="min-w-0 break-words [overflow-wrap:anywhere]">{{ label(category) }}</span>
        </button>
        <button
          v-if="category.children.length"
          type="button"
          class="grid h-11 w-11 flex-none place-items-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          :aria-expanded="expandedParentIds.includes(category.id)"
          :aria-label="t('products.toggleCategory', { name: label(category) })"
          @click="emit('toggle', category.id)"
        >
          <ChevronRight class="h-4 w-4 transition-transform" :class="{ 'rotate-90': expandedParentIds.includes(category.id) }" />
        </button>
      </div>
      <CategoryTree
        v-if="category.children.length && expandedParentIds.includes(category.id)"
        :categories="category.children"
        :selected-category="selectedCategory"
        :expanded-parent-ids="expandedParentIds"
        :depth="depth + 1"
        :compact="compact"
        :variant="variant"
        @select="emit('select', $event)"
        @toggle="emit('toggle', $event)"
      />
    </li>
  </ul>
</template>
