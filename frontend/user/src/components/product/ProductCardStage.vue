<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pause, Play } from 'lucide-vue-next'
import SmartImage from '../SmartImage.vue'
import { useAppStore } from '../../stores/app'

const props = defineProps<{
  src: string
  alt: string
}>()

const { t } = useI18n()
const appStore = useAppStore()
const brandName = computed(() => String(appStore.config?.brand?.site_name || '').trim() || 'IDID')
const cardSurface = computed(() => ({ '--card-art': `url(${JSON.stringify(props.src)})` }))
const paused = ref(false)
watch(() => props.src, () => { paused.value = false })
</script>

<template>
  <div class="card-stage" :style="cardSurface" :class="{ 'is-paused': paused }" role="group" :aria-label="alt">
    <div :key="src" class="card-float">
      <div class="card-rotate">
        <div v-for="depth in [-1, 0, 1]" :key="depth" class="card-edge" :style="{ transform: `translateZ(${depth}px)` }" aria-hidden="true"></div>
        <div class="card-face card-front">
          <SmartImage :src="src" :alt="alt" loading="eager" img-class="h-full w-full object-contain" />
        </div>
        <div class="card-face card-back" aria-hidden="true">
          <div class="card-back-rim"></div>
          <span class="card-brand">{{ brandName }}</span>
        </div>
      </div>
    </div>
    <button
      type="button"
      class="motion-control"
      :aria-label="t(paused ? 'productDetail.cardMotion.resume' : 'productDetail.cardMotion.pause')"
      :title="t(paused ? 'productDetail.cardMotion.resume' : 'productDetail.cardMotion.pause')"
      :aria-pressed="paused"
      @click="paused = !paused"
    >
      <Play v-if="paused" class="h-3.5 w-3.5" aria-hidden="true" />
      <Pause v-else class="h-3.5 w-3.5" aria-hidden="true" />
    </button>
  </div>
</template>

<style scoped>
.card-stage {
  --card-radius: clamp(14px, 2vw, 26px);
  position: relative;
  display: grid;
  place-items: center;
  width: 100%;
  aspect-ratio: 8 / 5;
  perspective: 1400px;
}

.card-float {
  position: relative;
  width: 92%;
  aspect-ratio: 8 / 5;
  transform-style: preserve-3d;
  animation: card-float 4.2s ease-in-out infinite;
}

.card-rotate {
  position: absolute;
  inset: 0;
  transform-style: preserve-3d;
  will-change: transform;
  animation: card-orbit 22s linear infinite;
}

.card-edge {
  position: absolute;
  inset: 0;
  border-radius: var(--card-radius);
  background: var(--card-art) center / cover, var(--ui-bg-muted);
}

.card-face {
  position: absolute;
  inset: 0;
  overflow: hidden;
  border-radius: var(--card-radius);
  backface-visibility: hidden;
  -webkit-backface-visibility: hidden;
  box-shadow: 0 18px 28px -18px rgb(16 12 29 / 55%);
}

.card-front {
  display: grid;
  background: var(--ui-bg-muted);
  transform: translateZ(1.5px);
}

.card-front :deep(img) {
  /* 原卡面自带的留白与阴影在展示时统一裁掉，保留原始图片。 */
  transform: scale(1.06);
}

.card-back {
  display: grid;
  place-items: center;
  background: var(--ui-bg-muted);
  box-shadow:
    inset 0 1px 1px rgb(255 255 255 / 22%),
    inset 0 -1px 1px rgb(0 0 0 / 65%),
    0 18px 28px -18px rgb(16 12 29 / 55%);
  transform: rotateY(180deg) translateZ(1.5px);
}

.card-back::before,
.card-back::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
}

.card-back::before {
  /* 沿用原图的配色，模糊隐藏正面的图文，不需要取色或维护额外的背面素材。 */
  background: var(--card-art) center / cover;
  filter: blur(32px);
  transform: scale(1.3);
}

.card-back::after {
  background:
    radial-gradient(ellipse at 20% -10%, rgb(255 255 255 / 20%), transparent 60%),
    linear-gradient(125deg, transparent 25%, rgb(0 0 0 / 10%) 65%, transparent);
  box-shadow: inset 0 0 12px rgb(0 0 0 / 18%);
}

.card-back-rim {
  position: absolute;
  z-index: 2;
  inset: 3px;
  border: 1px solid rgb(238 234 245 / 55%);
  border-radius: calc(var(--card-radius) - 3px);
  box-shadow: 0 0 0 1px rgb(33 29 42 / 30%), inset 0 0 0 1px rgb(33 29 42 / 15%);
  pointer-events: none;
}

.card-brand {
  position: relative;
  z-index: 1;
  max-width: 75%;
  padding: 0.08em 0.01em 0.12em;
  font-family: 'Rubik', system-ui, sans-serif;
  font-size: clamp(32px, 4.4vw, 60px);
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: 0.01em;
  overflow-wrap: anywhere;
  text-align: center;
  color: #aaa;
  background: linear-gradient(145deg, #e2e2e2 5%, #b9b9b9 32%, #919191 62%, #c6c6c6 95%);
  background-clip: text;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  -webkit-text-stroke: 0.5px rgb(255 255 255 / 45%);
  /* 只替换明暗，保留卡面的色相；四色卡面共用同一套金属浮雕。 */
  mix-blend-mode: luminosity;
  filter:
    drop-shadow(0 -1px 0 rgb(255 255 255 / 65%))
    drop-shadow(0 1px 0 rgb(34 34 34 / 85%))
    drop-shadow(0 2px 0 rgb(34 34 34 / 55%))
    drop-shadow(0 4px 3px rgb(0 0 0 / 25%));
}

.motion-control {
  position: absolute;
  right: 0;
  bottom: 0;
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border: 1px solid var(--ui-border);
  border-radius: 50%;
  background: var(--ui-bg-page);
  color: var(--ui-text-muted);
  cursor: pointer;
}

.motion-control:hover { color: var(--ui-text-primary); }
.motion-control:focus-visible { outline: 2px solid var(--ui-accent); outline-offset: 3px; }
.is-paused .card-float,
.is-paused .card-rotate { animation-play-state: paused; }

@media (hover: hover) {
  .card-stage:hover .card-rotate { animation-play-state: paused; }
}

@keyframes card-orbit {
  0% { transform: rotateX(3deg) rotateY(-8deg); }
  25% { transform: rotateX(-2deg) rotateY(82deg); }
  50% { transform: rotateX(3deg) rotateY(172deg); }
  75% { transform: rotateX(-3deg) rotateY(262deg); }
  100% { transform: rotateX(3deg) rotateY(352deg); }
}

@keyframes card-float {
  0%, 100% { transform: translateY(2px) rotate(-0.2deg); }
  50% { transform: translateY(-6px) rotate(0.2deg); }
}

@media (prefers-reduced-motion: reduce) {
  .card-float,
  .card-rotate {
    animation: none !important;
    transform: none;
    will-change: auto;
  }
  .motion-control { display: none; }
}
</style>
