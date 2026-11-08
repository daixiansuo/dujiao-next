<template>
  <RouterLink :to="`/products/${product.slug}`" class="flex h-full flex-col gap-2.5 rounded-lg border bg-card p-3 text-left transition hover:-translate-y-[3px] hover:border-hairline-strong hover:shadow-[var(--shadow)]" :class="{ 'opacity-[0.74]': soldOut }">
    <div class="flex flex-col items-stretch gap-[11px]">
      <span
        class="relative grid aspect-[8/5] w-full flex-none place-items-center overflow-hidden rounded-[13px] bg-muted"
      >
        <SmartImage :src="coverImage" :alt="title" img-class="absolute inset-0 h-full w-full object-contain">
          <template #fallback>
            <span class="absolute inset-0 grid place-items-center" :class="coverClass">
              <Package class="h-[58px] w-[58px] text-white" />
            </span>
          </template>
        </SmartImage>
      </span>
      <div class="flex min-w-0 flex-col gap-0.5 px-1">
        <span v-if="categoryName" class="truncate text-xs font-semibold text-muted-foreground">{{ categoryName }}</span>
        <h3 class="line-clamp-2 text-base font-bold leading-[1.3]">{{ title }}</h3>
        <div v-if="!soldOut && product.tags && product.tags.length" class="mt-1 flex flex-wrap gap-1">
          <span v-for="(tag, i) in product.tags.slice(0, 2)" :key="i" class="inline-flex max-w-full items-center truncate rounded-md bg-secondary px-2 py-0.5 text-[11px] font-semibold text-muted-foreground">{{ tag }}</span>
        </div>
      </div>
    </div>

    <!-- 属性徽章：交付方式 · 购买类型 · 库存 -->
    <div class="mx-1 flex flex-wrap items-center gap-1.5">
      <span class="inline-flex items-center gap-1 rounded-full bg-[color:var(--teal-soft)] px-2 py-0.5 text-[11px] font-semibold text-[color:var(--teal-strong)]">
        <component :is="product.fulfillment_type === 'auto' ? Zap : Pencil" class="h-3 w-3" />
        {{ getFulfillmentTypeLabel(product.fulfillment_type) }}
      </span>
      <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold" :class="product.purchase_type === 'guest' ? 'bg-[color:var(--gold-soft)] text-[color:var(--gold-strong)]' : 'bg-[color:var(--teal-soft)] text-[color:var(--teal-strong)]'">
        <component :is="product.purchase_type === 'guest' ? UserPlus : Lock" class="h-3 w-3" />
        {{ getPurchaseTypeLabel(product.purchase_type) }}
      </span>
      <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold" :class="stockPill.tone">
        <component :is="stockPill.icon" class="h-3 w-3" />
        {{ stockPill.label }}
      </span>
    </div>

    <div class="mt-auto flex items-end justify-between gap-2 px-1">
      <div class="flex min-w-0 flex-col gap-1">
        <div class="flex flex-wrap items-baseline gap-x-1.5">
          <template v-if="promo">
            <span class="text-xl font-extrabold tabular-nums text-primary">{{ formatPrice(getPromotionPriceAmount(product), siteCurrency) }}</span>
            <span class="text-sm font-semibold text-muted-foreground line-through">{{ formatPrice(product.price_amount, siteCurrency) }}</span>
          </template>
          <span v-else class="text-xl font-extrabold tabular-nums text-foreground">{{ formatPrice(product.price_amount, siteCurrency) }}</span>
        </div>
        <span v-if="priceSignal" class="inline-flex w-fit items-center rounded-full px-2 py-0.5 text-[11px] font-semibold" :class="priceSignal.tone">{{ priceSignal.label }}</span>
      </div>
      <span v-if="soldOut" class="inline-flex flex-none items-center rounded-full border-2 border-hairline-strong px-3.5 py-1.5 text-[13px] font-bold text-foreground" aria-disabled="true">{{ t('products.stockStatus.outOfStock') }}</span>
      <button
        v-else
        type="button"
        class="inline-flex flex-none items-center gap-1.5 rounded-full bg-primary px-3.5 py-1.5 text-[13px] font-bold text-white transition hover:bg-primary/90"
        :aria-label="t('products.quickBuyAria')"
        @click.prevent.stop="$emit('quickBuy', product)"
      >
        <Zap class="h-3.5 w-3.5" />
        {{ t('products.quickBuy') }}
      </button>
    </div>
  </RouterLink>
</template>

<script setup lang="ts">
import SmartImage from '../../../components/SmartImage.vue'
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlarmClock, Lock, Package, Pencil, UserPlus, XCircle, Zap } from 'lucide-vue-next'
import { getFirstImageUrl, getImageUrl } from '../../../utils/image'
import { useLocalized, useProductLabels } from '../../../composables/useProduct'

const props = withDefaults(defineProps<{ product: any; index?: number }>(), { index: 0 })

defineEmits<{ quickBuy: [product: any] }>()

const { t } = useI18n()
const { getLocalizedText, siteCurrency, formatPrice } = useLocalized()
const {
  getStockStatusLabel, getPurchaseTypeLabel, getFulfillmentTypeLabel,
  isSoldOut, hasPromotionPrice, getPromotionPriceAmount, hasWholesalePrices, hasPromotionRules,
} = useProductLabels()

// 封面渐变（对应原 cover-red/teal/plum/gold/ink）
const covers = [
  'bg-[linear-gradient(135deg,#7b74f2,var(--red))]',
  'bg-[linear-gradient(135deg,#1cc0bf,var(--teal))]',
  'bg-[linear-gradient(135deg,#9b6cf5,var(--plum))]',
  'bg-[linear-gradient(135deg,#f7bd4e,var(--gold))]',
  'bg-[linear-gradient(135deg,#3a3950,var(--ink))]',
]
const coverClass = computed(() => covers[(props.index ?? 0) % covers.length])

const title = computed(() => getLocalizedText(props.product?.title))
const categoryName = computed(() => getLocalizedText(props.product?.category?.name))
const soldOut = computed(() => isSoldOut(props.product))
const promo = computed(() => hasPromotionPrice(props.product))

const coverImage = computed(() => {
  const primary = getFirstImageUrl(props.product?.images)
  if (primary) return primary
  const icon = props.product?.category?.icon
  return icon ? getImageUrl(icon) : ''
})

const stockPill = computed<{ tone: string; icon: Component; label: string }>(() => {
  if (soldOut.value) {
    return { tone: 'bg-secondary text-muted-foreground', icon: XCircle, label: t('products.stockStatus.outOfStock') }
  }
  if (props.product?.stock_status === 'low_stock') {
    return { tone: 'bg-[color:var(--gold-soft)] text-[color:var(--gold-strong)]', icon: AlarmClock, label: getStockStatusLabel(props.product) }
  }
  return { tone: 'bg-[color:var(--teal-soft)] text-[color:var(--teal-strong)]', icon: Zap, label: getStockStatusLabel(props.product) }
})

// 价签徽章：促销 / 批发 / 活动（择一，对齐 classic 优先级）
const priceSignal = computed<{ tone: string; label: string } | null>(() => {
  if (promo.value) return { tone: 'bg-primary/10 text-primary', label: t('products.promotionTag') }
  if (hasWholesalePrices(props.product)) return { tone: 'bg-[color:var(--teal-soft)] text-[color:var(--teal-strong)]', label: t('products.wholesaleTag') }
  if (hasPromotionRules(props.product)) return { tone: 'bg-[color:var(--gold-soft)] text-[color:var(--gold-strong)]', label: t('products.promotionBadge') }
  return null
})
</script>
