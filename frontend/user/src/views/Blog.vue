<template>
  <div class="min-h-screen bg-background pb-16 pt-24 text-foreground">
    <div class="container mx-auto px-4">
      <div class="mb-6 flex flex-wrap items-end justify-between gap-x-8 gap-y-4">
        <p class="text-sm text-muted-foreground">{{ t('blog.subtitle') }}</p>
        <label class="flex h-11 w-full items-center gap-2 rounded-full bg-card px-4 theme-float focus-within:outline-2 focus-within:outline-primary sm:w-80">
          <Search class="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <input
            v-model="searchKeyword"
            type="text"
            :placeholder="t('blog.searchPlaceholder')"
            :aria-label="t('blog.searchPlaceholder')"
            class="min-w-0 flex-1 border-none bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
          />
          <button
            v-if="searchKeyword"
            type="button"
            class="-mr-2 flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            :aria-label="t('blog.searchClear')"
            @click="searchKeyword = ''"
          >
            <X class="h-4 w-4" />
          </button>
        </label>
      </div>

      <div v-if="categories.length" class="mb-6 flex flex-wrap gap-2" role="group" :aria-label="t('blog.categoryLabel')">
        <button
          v-for="category in [{ id: null, name: null }, ...categories]"
          :key="category.id ?? 'all'"
          type="button"
          class="rounded-full px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-primary"
          :class="selectedCategory === category.id ? 'bg-foreground text-background' : 'bg-card text-muted-foreground hover:bg-secondary hover:text-foreground'"
          :aria-pressed="selectedCategory === category.id"
          @click="changeCategory(category.id)"
        >
          {{ category.id === null ? t('blog.allCategories') : getLocalizedText(category.name) }}
        </button>
      </div>

      <div v-if="loading" class="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
        <div v-for="i in 6" :key="i" class="theme-skeleton h-[280px] rounded-[22px]"></div>
      </div>

      <div v-else-if="posts.length > 0">
        <div class="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
          <article
            v-for="post in posts"
            :key="post.id"
            class="post-card theme-float group flex cursor-pointer flex-col rounded-[22px] bg-card p-2"
            @click="goToPost(post.slug)"
          >
            <!-- 没封面时用标题首字占位，色相按文章 id 取，和商品卡同一套做法 -->
            <div
              class="flex aspect-[8/5] shrink-0 items-center justify-center overflow-hidden rounded-2xl"
              :class="[
                post.thumbnail ? 'bg-secondary' : 'bg-[hsl(var(--h)_62%_94%)] dark:bg-[hsl(var(--h)_32%_17%)]',
              ]"
              :style="{ '--h': placeholderHue(post.id) }"
            >
              <img
                v-if="post.thumbnail"
                :src="getImageUrl(post.thumbnail)"
                alt=""
                loading="lazy"
                class="h-full w-full object-cover transition-transform duration-500 group-hover:scale-[1.04]"
              />
              <span v-else class="select-none text-6xl font-black text-[hsl(var(--h)_55%_38%)] dark:text-[hsl(var(--h)_60%_76%)]" aria-hidden="true">
                {{ Array.from(getLocalizedText(post.title).trim())[0] }}
              </span>
            </div>

            <div class="flex flex-1 flex-col p-4">
              <time class="text-xs tabular-nums text-muted-foreground">{{ formatDate(post.published_at) }}</time>
              <h2
                class="mt-2 line-clamp-2 text-[17px] font-bold leading-snug tracking-tight"
              >
                <router-link :to="`/blog/${post.slug}`" class="focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary" @click.stop>
                  {{ getLocalizedText(post.title) }}
                </router-link>
              </h2>
              <p class="mt-2 line-clamp-3 text-sm leading-relaxed text-muted-foreground">{{ getLocalizedText(post.summary) }}</p>
              <div class="mt-auto pt-4">
                <span class="theme-pill-link group-hover:bg-foreground group-hover:text-background">{{ t('blog.readMore') }}<ChevronRight class="h-3.5 w-3.5" aria-hidden="true" /></span>
              </div>
            </div>
          </article>
        </div>

        <PaginationNav
          :current-page="currentPage"
          :total-pages="totalPages"
          @change-page="changePage"
        />
      </div>

      <EmptyState v-else size="lg" :title="searchKeyword.trim() ? t('blog.noResults') : t('blog.empty')">
        <template #icon>
          <BookOpen class="h-16 w-16 text-muted-foreground opacity-70" :stroke-width="1.5" />
        </template>
      </EmptyState>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { BookOpen, ChevronRight, Search, X } from 'lucide-vue-next'
import { getImageUrl, placeholderHue } from '../utils/image'
import EmptyState from '../components/EmptyState.vue'
import PaginationNav from '../components/PaginationNav.vue'
import { usePostList } from '../composables/usePostList'

const { t } = useI18n()

const {
  loading, posts, currentPage, totalPages, searchKeyword,
  categories, selectedCategory, changeCategory, getLocalizedText, formatDate, goToPost, changePage,
} = usePostList('blog', { title: () => t('nav.blog'), canonicalPath: '/blog' }, { categoryFilter: true })
</script>

<style scoped>
.post-card {
  transition: transform 200ms var(--ui-ease-out), box-shadow 200ms var(--ui-ease-out);
}
.post-card:hover {
  transform: translateY(-2px);
}
@media (prefers-reduced-motion: reduce) {
  .post-card { transition: none; }
  .post-card:hover { transform: none; }
}
</style>
