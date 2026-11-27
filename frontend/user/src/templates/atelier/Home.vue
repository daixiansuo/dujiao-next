<template>
  <div class="min-h-screen pb-16 pt-[88px] text-foreground">
    <!-- 搜索、分类与实时商品货架，按后台配置保留 Banner -->
    <AtelierCatalog
      :layout="isListMode ? 'list' : 'grid'"
      @category-name="seoCategoryName = $event"
    >
      <template #showcase>
        <!-- 仅展示后台已配置的 Banner -->
        <section v-if="showHeroSection">
          <!-- 真实 Banner 轮播主推卡 -->
          <div class="flex flex-col">
            <div v-if="bannerLoading" class="h-full min-h-[224px] flex-1 rounded-[16px] theme-skeleton"></div>
            <div
              v-else
              class="prism-bento-banner prism-card relative isolate flex h-full min-h-[224px] flex-1 flex-col justify-between overflow-hidden rounded-[16px] p-6 sm:p-7"
              :class="heroImage ? 'text-white' : 'bg-card text-foreground'"
              @touchstart="onBannerTouchStart"
              @touchend="onBannerTouchEnd"
            >
              <Transition name="at-banner-fade" mode="out-in">
                <img
                  v-if="heroImage"
                  :key="heroImage"
                  :src="heroImage"
                  alt=""
                  class="absolute inset-0 -z-20 h-full w-full object-cover"
                />
              </Transition>
              <div v-if="heroImage" class="prism-banner-scrim absolute inset-0 -z-10"></div>

              <!-- 顶栏：站点标记 + 多轮播序号 -->
              <div class="flex items-center justify-between gap-3">
                <span
                  class="inline-flex h-6 items-center gap-1.5 rounded-[7px] border px-2.5 text-[11px] font-semibold backdrop-blur-md"
                  :class="heroImage
                    ? 'border-white/20 bg-black/35 text-white/90'
                    : 'border-primary/30 bg-primary/10 text-primary'"
                >
                  <span class="h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
                  <span class="max-w-[14rem] truncate">{{ brandSiteName }}</span>
                </span>

                <span
                  v-if="bannerCount > 1"
                  class="font-mono text-[11px] tracking-[0.06em]"
                  :class="heroImage ? 'text-white/75' : 'text-muted-foreground'"
                >
                  {{ String(currentBannerIndex + 1).padStart(2, '0') }} / {{ String(bannerCount).padStart(2, '0') }}
                </span>
              </div>

              <!-- 主体：真实 Banner 标题 / 副标 / 跳转按钮 + 进度指示器 -->
              <div class="mt-6">
                <h2 class="line-clamp-2 max-w-[20em] text-xl font-bold leading-snug tracking-[-0.025em] sm:text-2xl">
                  {{ heroTitle }}
                </h2>
                <p
                  v-if="heroSubtitle"
                  class="mt-1.5 line-clamp-2 max-w-[48ch] text-[13.5px]"
                  :class="heroImage ? 'text-white/80' : 'text-muted-foreground'"
                >
                  {{ heroSubtitle }}
                </p>

                <div class="mt-5 flex flex-wrap items-center justify-between gap-4">
                  <button
                    type="button"
                    class="inline-flex h-9 items-center gap-1.5 rounded-[10px] px-4 text-xs font-semibold transition-all hover:opacity-90"
                    :class="heroImage
                      ? 'bg-white text-slate-950'
                      : 'bg-primary text-primary-foreground shadow-xs'"
                    @click="goToHeroLink"
                  >
                    <span>{{ heroPrimaryButtonText }}</span>
                    <ArrowRight class="h-3.5 w-3.5" aria-hidden="true" />
                  </button>

                  <div v-if="bannerCount > 1" class="flex items-center gap-1.5">
                    <button
                      v-for="(_, index) in banners"
                      :key="`prism-dot-${index}`"
                      type="button"
                      class="group/dot py-2"
                      :aria-label="t('common.switchBanner', { n: index + 1 })"
                      :aria-current="index === currentBannerIndex"
                      @click="selectHeroBanner(index)"
                    >
                      <span
                        class="block h-[4px] overflow-hidden rounded-full transition-all"
                        :class="[
                          index === currentBannerIndex ? 'w-7' : 'w-3',
                          heroImage ? 'bg-white/35' : 'bg-border',
                        ]"
                      >
                        <span
                          v-if="index === currentBannerIndex"
                          :key="currentBannerIndex"
                          class="at-hero-progress block h-full rounded-full"
                          :class="heroImage ? 'bg-white' : 'bg-primary'"
                        ></span>
                      </span>
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>

        </section>
      </template>
    </AtelierCatalog>

    <AnnouncementModal
      v-if="activeAnnouncement"
      :announcement="activeAnnouncement"
      :visible="announcementVisible"
      @update:visible="announcementVisible = $event"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowRight } from 'lucide-vue-next'
import { useBannerCarousel } from '../../composables/useBannerCarousel'
import { useNavConfig } from '../../composables/useNavConfig'
import { useFooterConfig } from '../../composables/useFooterConfig'
import { usePageSeo } from '../../composables/usePageSeo'
import { useAppStore } from '../../stores/app'
import { useAnnouncement, type HomeAnnouncement } from '../../composables/useAnnouncement'
import AtelierCatalog from './components/AtelierCatalog.vue'
import AnnouncementModal from '../../components/AnnouncementModal.vue'

const { t } = useI18n()
const appStore = useAppStore()
const route = useRoute()

const { isListMode } = useNavConfig()
const { brandSiteName } = useFooterConfig()
const { shouldShow } = useAnnouncement()
const activeAnnouncement = ref<HomeAnnouncement | null>(null)
const announcementVisible = ref(false)

const {
  banners,
  bannerLoading,
  currentBannerIndex,
  bannerCount,
  showHeroSection,
  heroImage,
  heroTitle,
  heroSubtitle,
  heroPrimaryButtonText,
  loadBanners,
  selectHeroBanner,
  goToHeroLink,
  onBannerTouchStart,
  onBannerTouchEnd,
  stopHeroAutoPlay,
} = useBannerCarousel()

const seoCategoryName = ref('')
usePageSeo({
  canonicalPath: () => route.path,
  title: () => {
    if (route.name === 'category-products') return seoCategoryName.value || t('nav.products')
    if (route.name === 'products') return t('nav.products')
    return undefined
  },
})

const showAnnouncementIfNeeded = () => {
  const announcement = appStore.config?.announcement as HomeAnnouncement | undefined
  if (announcement && shouldShow(announcement)) {
    activeAnnouncement.value = announcement
    announcementVisible.value = true
  }
}

onMounted(async () => {
  await appStore.loadConfig()
  await loadBanners()
  showAnnouncementIfNeeded()
})

onUnmounted(() => {
  stopHeroAutoPlay()
})
</script>

<style scoped>
.prism-bento-banner {
  background-image: linear-gradient(135deg, var(--at-brand-soft) 0%, transparent 62%);
}

.prism-banner-scrim {
  background: linear-gradient(15deg, rgba(8, 9, 13, 0.84) 0%, rgba(8, 9, 13, 0.42) 55%, rgba(8, 9, 13, 0.16) 100%);
}

.at-banner-fade-enter-active,
.at-banner-fade-leave-active {
  transition: opacity 280ms ease;
}
.at-banner-fade-enter-from,
.at-banner-fade-leave-to {
  opacity: 0;
}

.at-hero-progress {
  transform-origin: left;
  animation: at-hero-progress 5s linear both;
}

@keyframes at-hero-progress {
  from {
    transform: scaleX(0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .at-hero-progress {
    animation: none;
  }
}
</style>
