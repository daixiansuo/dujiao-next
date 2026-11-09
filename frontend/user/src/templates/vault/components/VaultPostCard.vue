<template>
  <RouterLink
    class="group flex flex-col overflow-hidden rounded-xl border bg-card shadow-sm transition hover:-translate-y-1 hover:border-hairline-strong hover:shadow-[var(--shadow)]"
    :to="`/blog/${post.slug}`"
  >
    <div v-if="post.thumbnail" class="aspect-[15/7] w-full shrink-0 overflow-hidden">
      <img :src="getImageUrl(post.thumbnail)" :alt="getLocalizedText(post.title)" loading="lazy" class="h-full w-full object-cover" />
    </div>
    <div class="flex flex-1 flex-col p-[22px]">
      <div class="mb-1 flex items-center gap-2.5">
        <Badge :variant="post.type === 'blog' ? 'info' : 'warning'" size="sm" class="rounded-full">{{ post.type === 'blog' ? t('nav.blog') : t('nav.notice') }}</Badge>
        <span v-if="post.type === 'notice' && post.published_at" class="text-xs font-semibold text-muted-foreground">{{ new Date(post.published_at).toLocaleDateString() }}</span>
      </div>
      <h3 class="text-lg font-bold">{{ getLocalizedText(post.title) }}</h3>
      <p class="mt-1.5 flex-1 text-sm leading-relaxed text-muted-foreground">{{ getLocalizedText(post.summary) }}</p>
      <span class="mt-3 inline-flex items-center gap-1.5 text-sm font-semibold text-primary">
        {{ t('blog.readMore') }} <ArrowRight class="h-4 w-4 transition group-hover:translate-x-0.5" />
      </span>
    </div>
  </RouterLink>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ArrowRight } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { useLocalized } from '../../../composables/useProduct'
import { getImageUrl } from '../../../utils/image'

defineProps<{
  post: {
    slug: string
    type: string
    thumbnail?: string
    title?: Record<string, string>
    summary?: Record<string, string>
    published_at?: string
  }
}>()

const { t } = useI18n()
const { getLocalizedText } = useLocalized()
</script>
