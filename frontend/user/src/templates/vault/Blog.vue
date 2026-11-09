<template>
  <div class="mx-auto w-full max-w-[1180px] px-6 pb-10 pt-2">
    <header class="mt-6 flex flex-col items-start gap-2">
      <p class="text-muted-foreground">{{ t('blog.subtitle') }}</p>
    </header>

    <div class="relative my-7 max-w-[520px]">
      <Search class="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <Input v-model="searchKeyword" type="text" class="h-11 pl-10 pr-[88px]" :placeholder="t('blog.searchPlaceholder')" />
      <Button v-if="searchKeyword" type="button" variant="ghost" size="sm" class="absolute right-1.5 top-1/2 h-8 -translate-y-1/2 rounded-full" @click="searchKeyword = ''">{{ t('blog.searchClear') }}</Button>
    </div>

    <div v-if="categories.length" class="mb-7 flex items-start gap-3">
      <span class="shrink-0 py-2 text-sm text-muted-foreground">{{ t('blog.categoryLabel') }}</span>
      <div class="flex min-w-0 flex-wrap gap-2" role="group" :aria-label="t('blog.categoryLabel')">
        <button
          v-for="category in [{ id: null, name: null }, ...categories]"
          :key="category.id ?? 'all'"
          type="button"
          class="max-w-full break-words rounded-lg px-3 py-2 text-left text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          :class="selectedCategory === category.id ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary hover:text-foreground'"
          :aria-pressed="selectedCategory === category.id"
          @click="changeCategory(category.id)"
        >
          {{ category.id === null ? t('blog.allCategories') : getLocalizedText(category.name) }}
        </button>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="grid gap-5 grid-cols-[repeat(auto-fill,minmax(280px,1fr))]">
      <div v-for="i in 6" :key="i" class="h-[280px] rounded-xl border bg-card opacity-50"></div>
    </div>

    <!-- Posts -->
    <template v-else-if="posts.length > 0">
      <div class="grid gap-5 grid-cols-[repeat(auto-fill,minmax(280px,1fr))]">
        <RouterLink
          v-for="post in posts"
          :key="post.id"
          class="group flex flex-col overflow-hidden rounded-xl border bg-card shadow-sm transition hover:-translate-y-1 hover:border-hairline-strong hover:shadow-[var(--shadow)]"
          :to="`/blog/${post.slug}`"
        >
          <div v-if="post.thumbnail" class="aspect-[15/7] w-full shrink-0 overflow-hidden">
            <img :src="getImageUrl(post.thumbnail)" :alt="getLocalizedText(post.title)" loading="lazy" class="h-full w-full object-cover" />
          </div>
          <div class="flex flex-1 flex-col p-[22px]">
            <div class="mb-1 flex items-center gap-2.5">
              <Badge :variant="post.type === 'blog' ? 'info' : 'warning'" size="sm" class="rounded-full">{{ post.type === 'blog' ? t('nav.blog') : t('nav.notice') }}</Badge>
            </div>
            <h3 class="text-lg font-bold">{{ getLocalizedText(post.title) }}</h3>
            <p class="mt-1.5 flex-1 text-sm leading-relaxed text-muted-foreground">{{ getLocalizedText(post.summary) }}</p>
            <span class="mt-3 inline-flex items-center gap-1.5 text-sm font-semibold text-primary">
              {{ t('blog.readMore') }} <ArrowRight class="h-4 w-4 transition group-hover:translate-x-0.5" />
            </span>
          </div>
        </RouterLink>
      </div>

      <div v-if="totalPages > 1" class="mt-8 flex items-center justify-center gap-3.5">
        <Button variant="outline" size="icon" class="rounded-full" :disabled="currentPage <= 1" @click="changePage(currentPage - 1)"><ChevronLeft /></Button>
        <span class="text-sm font-bold">{{ currentPage }} / {{ totalPages }}</span>
        <Button variant="outline" size="icon" class="rounded-full" :disabled="currentPage >= totalPages" @click="changePage(currentPage + 1)"><ChevronRight /></Button>
      </div>
    </template>

    <!-- Empty -->
    <div v-else class="flex flex-col items-center gap-3 rounded-xl border border-dashed py-16 text-center text-muted-foreground">
      <BookOpen class="h-10 w-10 opacity-60" />
      <p>{{ searchKeyword.trim() ? t('blog.noResults') : t('blog.empty') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ArrowRight, BookOpen, ChevronLeft, ChevronRight, Search } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getImageUrl } from '../../utils/image'
import { usePostList } from '../../composables/usePostList'

const { t } = useI18n()

const {
  loading, posts, currentPage, totalPages, searchKeyword,
  categories, selectedCategory, changeCategory, getLocalizedText, changePage,
} = usePostList('blog', { title: () => t('nav.blog'), canonicalPath: '/blog' }, { categoryFilter: true })
</script>
