<template>
  <div class="min-h-screen pb-16 pt-[88px] text-foreground">
    <div class="mx-auto w-full max-w-[1240px] px-4 sm:px-6">
      <section class="mx-auto max-w-[780px] pb-7 pt-4 md:pt-6">
        <h1 class="sr-only">{{ t('nav.blog') }}</h1>
        <div class="prism-search-hub mx-auto flex max-w-[640px] items-center gap-2.5 rounded-[16px] px-3.5 py-1.5">
          <Search class="h-[17px] w-[17px] shrink-0 text-muted-foreground" aria-hidden="true" />
          <input
            v-model="searchKeyword"
            type="search"
            :placeholder="t('blog.searchPlaceholder')"
            :aria-label="t('blog.searchPlaceholder')"
            class="at-search h-10 min-w-0 flex-1 border-none bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground sm:text-[15px]"
          />
          <button
            v-if="searchKeyword"
            type="button"
            class="grid h-7 w-7 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            :aria-label="t('blog.searchClear')"
            @click="searchKeyword = ''"
          >
            <X class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
      </section>

      <div v-if="categories.length" class="mb-7 border-b pb-3">
        <AtelierCategoryRail
          :active-key="selectedCategory ?? 'all'"
          :label="t('blog.categoryLabel')"
        >
          <button
            v-for="category in [{ id: null, name: null }, ...categories]"
            :key="category.id ?? 'all'"
            type="button"
            class="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg px-3 text-xs font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-primary"
            :class="selectedCategory === category.id ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary hover:text-foreground'"
            :aria-pressed="selectedCategory === category.id"
            @click="changeCategory(category.id)"
          >
            <LayoutGrid v-if="category.id === null" class="h-3.5 w-3.5" aria-hidden="true" />
            {{ category.id === null ? t('blog.allCategories') : getLocalizedText(category.name) }}
          </button>
        </AtelierCategoryRail>
      </div>

      <div v-if="loading" class="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
        <div v-for="i in 6" :key="i" class="theme-skeleton h-[280px] rounded-[16px]"></div>
      </div>

      <div v-else-if="posts.length > 0">
        <div class="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
          <article
            v-for="post in posts"
            :key="post.id"
            class="at-post-card atelier-card group flex min-w-0 cursor-pointer flex-col overflow-hidden rounded-[16px] bg-card"
            @click="goToPost(post.slug)"
          >
            <!-- 沿用原定制的 15:7 顶部封面；缺图或加载失败时收起，不额外撑高卡片。 -->
            <SmartImage
              v-if="post.thumbnail"
              :src="getImageUrl(post.thumbnail)"
              alt=""
              img-class="aspect-[15/7] w-full shrink-0 object-cover"
            >
              <template #fallback><span class="hidden" aria-hidden="true"></span></template>
            </SmartImage>

            <div class="flex flex-1 flex-col p-[22px]">
              <div class="flex items-center gap-2 text-xs text-muted-foreground">
                <span class="inline-flex h-5 items-center rounded-[5px] border bg-background px-1.5 text-[11px] font-semibold text-foreground/80">
                  {{ t('nav.blog') }}
                </span>
              </div>
              <h2
                class="mt-2.5 break-words text-lg font-bold leading-snug tracking-[-0.02em] text-foreground"
              >
                <RouterLink :to="`/blog/${post.slug}`" class="focus-visible:outline-2 focus-visible:outline-primary" @click.stop>
                  {{ getLocalizedText(post.title) }}
                </RouterLink>
              </h2>
              <p class="mt-2 line-clamp-3 text-[13.5px] leading-relaxed text-muted-foreground">
                {{ getLocalizedText(post.summary) }}
              </p>
              <div class="mt-auto pt-4">
                <span class="inline-flex items-center gap-1 text-xs font-semibold text-foreground/80 transition-colors group-hover:text-foreground">
                  {{ t('blog.readMore') }}
                  <ArrowRight class="h-3.5 w-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
                </span>
              </div>
            </div>
          </article>
        </div>

        <PaginationNav :current-page="currentPage" :total-pages="totalPages" @change-page="changePage" />
      </div>

      <EmptyState v-else size="lg" :title="searchKeyword.trim() ? t('blog.noResults') : t('blog.empty')">
        <template #icon>
          <BookOpen class="h-14 w-14 text-muted-foreground opacity-65" :stroke-width="1.5" />
        </template>
      </EmptyState>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ArrowRight, BookOpen, LayoutGrid, Search, X } from 'lucide-vue-next'
import AtelierCategoryRail from './components/AtelierCategoryRail.vue'
import { getImageUrl } from '../../utils/image'
import SmartImage from '../../components/SmartImage.vue'
import EmptyState from '../../components/EmptyState.vue'
import PaginationNav from '../../components/PaginationNav.vue'
import { usePostList } from '../../composables/usePostList'

const { t } = useI18n()

const {
  loading, posts, currentPage, totalPages, searchKeyword,
  categories, selectedCategory, changeCategory,
  getLocalizedText, goToPost, changePage,
} = usePostList('blog', { title: () => t('nav.blog'), canonicalPath: '/blog' }, { categoryFilter: true })
</script>

<style scoped>
.at-post-card {
  transition: transform 240ms var(--ui-ease-out), box-shadow 240ms var(--ui-ease-out), border-color 180ms ease;
}
.at-post-card:hover {
  transform: translateY(-2px);
  border-color: var(--at-line-hi);
  box-shadow: var(--at-shadow-card-hover);
}
@media (prefers-reduced-motion: reduce) {
  .at-post-card { transition: none; }
  .at-post-card:hover { transform: none; }
}
</style>
