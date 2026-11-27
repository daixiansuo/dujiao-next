<template>
  <div>
    <!-- 卡面自带底色与阴影，有图时保持透明；只给缺图占位加底板。 -->
    <div
      class="relative flex aspect-[8/5] items-center justify-center rounded-[14px]"
      :class="!shownImage && 'atelier-stage'"
      :style="{ '--h': hue }"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onTouchEnd"
    >
      <ProductCardStage v-if="shownImage" :src="currentImage" :alt="productTitle" @image-error="markFailed(currentImage)" />
      <div v-else class="flex select-none flex-col items-center gap-2 text-muted-foreground/60" role="img" :aria-label="productTitle">
        <ImageIcon class="h-8 w-8" :stroke-width="1.25" aria-hidden="true" />
        <span class="text-xs">{{ t('atelier.coverUnavailable') }}</span>
      </div>
    </div>

    <div v-if="images.length > 1" class="mt-2 flex gap-1.5 overflow-x-auto pb-0.5 scrollbar-hide">
      <button
        v-for="(image, index) in images"
        :key="index"
        type="button"
        class="grid h-10 w-12 shrink-0 place-items-center overflow-hidden rounded-[8px] border bg-secondary/70 transition-all focus-visible:outline-2 focus-visible:outline-primary"
        :class="currentImage === image ? 'border-primary opacity-100 ring-1 ring-primary' : 'border-border opacity-60 hover:opacity-100'"
        :aria-label="`${productTitle} ${index + 1}`"
        :aria-pressed="currentImage === image"
        @click="emit('update:currentImage', image)"
      >
        <img
          v-if="!failedSet.has(image)"
          :src="image"
          alt=""
          loading="lazy"
          class="h-full w-full object-contain"
          @error="markFailed(image)"
        />
        <span v-else class="font-mono text-[10px] font-semibold text-muted-foreground">
          {{ String(index + 1).padStart(2, '0') }}
        </span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Image as ImageIcon } from 'lucide-vue-next'
import ProductCardStage from '../../../components/product/ProductCardStage.vue'
import { placeholderHue } from '../../../utils/image'

const props = defineProps<{
  images: string[]
  currentImage: string
  productTitle: string
  productId?: number | string
}>()

const emit = defineEmits<{
  'update:currentImage': [value: string]
}>()

const { t } = useI18n()
const hue = computed(() => placeholderHue(props.productId))
const failedSet = reactive(new Set<string>())
const markFailed = (src: string) => {
  if (src) failedSet.add(src)
}
watch(() => props.images, () => {
  failedSet.clear()
})
const shownImage = computed(() => (props.currentImage && !failedSet.has(props.currentImage) ? props.currentImage : ''))

let touchStartX = 0
const onTouchStart = (e: TouchEvent) => {
  touchStartX = e.touches[0]?.clientX ?? 0
}
const onTouchEnd = (e: TouchEvent) => {
  const diff = touchStartX - (e.changedTouches[0]?.clientX ?? 0)
  const count = props.images.length
  const index = props.images.indexOf(props.currentImage)
  if (Math.abs(diff) < 50 || count <= 1 || index === -1) return
  emit('update:currentImage', props.images[(index + (diff > 0 ? 1 : -1) + count) % count] ?? '')
}
</script>

<style scoped>
.scrollbar-hide {
  -ms-overflow-style: none;
  scrollbar-width: none;
}
.scrollbar-hide::-webkit-scrollbar {
  display: none;
}
</style>
