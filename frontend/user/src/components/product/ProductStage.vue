<template>
  <div>
    <div
      class="relative flex aspect-[8/5] items-center justify-center overflow-hidden rounded-[22px] md:rounded-[28px]"
      :class="currentImage ? 'bg-muted' : 'bg-[hsl(var(--h)_62%_94%)] dark:bg-[hsl(var(--h)_32%_17%)]'"
      :style="{ '--h': hue }"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onTouchEnd"
    >
      <ProductCardStage v-if="currentImage" :src="currentImage" :alt="productTitle" />
      <span v-else class="select-none text-8xl font-black text-[hsl(var(--h)_55%_38%)] md:text-[160px] dark:text-[hsl(var(--h)_60%_76%)]" aria-hidden="true">
        {{ Array.from(productTitle.trim())[0] }}
      </span>
    </div>

    <div v-if="images.length > 1" class="mt-3 flex gap-2 overflow-x-auto p-1 scrollbar-hide">
      <button
        v-for="(image, index) in images"
        :key="index"
        type="button"
        class="aspect-[8/5] w-20 shrink-0 overflow-hidden rounded-xl bg-muted transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        :class="currentImage === image ? 'ring-2 ring-foreground ring-offset-2 ring-offset-background' : 'opacity-60 hover:opacity-100'"
        :aria-label="`${productTitle} ${index + 1}`"
        :aria-pressed="currentImage === image"
        @click="emit('update:currentImage', image)"
      >
        <SmartImage :src="image" alt="" img-class="h-full w-full object-contain" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { placeholderHue } from '../../utils/image'
import SmartImage from '../SmartImage.vue'
import ProductCardStage from './ProductCardStage.vue'

const props = defineProps<{
  images: string[]
  currentImage: string
  productTitle: string
  productId?: number | string
}>()

const emit = defineEmits<{
  'update:currentImage': [value: string]
}>()

const hue = computed(() => placeholderHue(props.productId))

// 左右滑动切图
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
