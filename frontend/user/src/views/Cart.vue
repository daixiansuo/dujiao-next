<template>
  <div class="min-h-screen bg-background text-foreground pt-24 pb-16">
    <div class="container mx-auto px-4">
      <div class="mb-8">
        <h1 class="mb-2 text-3xl font-black text-foreground">{{ t('cart.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('cart.subtitle') }}</p>
      </div>

      <CheckoutSteps class="mb-8" current-step="cart" />

      <!-- Empty State -->
      <EmptyState
        v-if="cartItems.length === 0"
        icon="cart"
        :title="t('cart.empty')"
        :action-label="t('cart.emptyAction')"
        action-to="/products"
      />

      <div v-else class="grid grid-cols-1 gap-8 lg:grid-cols-3">
        <div class="space-y-4 lg:col-span-2">
          <article
            v-for="item in cartItems"
            :key="cartItemKey(item)"
            class="rounded-2xl border bg-card text-card-foreground p-4 md:p-5"
          >
            <div class="grid grid-cols-[96px_minmax(0,1fr)] gap-x-3 gap-y-3 sm:grid-cols-[128px_minmax(0,1fr)_auto] sm:gap-x-5">
              <div class="col-span-2 flex min-w-0 items-start justify-between gap-3 sm:contents">
                <router-link
                  :to="`/products/${item.slug}`"
                  class="min-w-0 line-clamp-2 sm:col-start-2 sm:row-start-1 text-base md:text-lg font-bold text-primary hover:underline"
                >
                  {{ getLocalizedText(item.title) }}
                </router-link>
                <Button
                  variant="ghost"
                  size="sm"
                  @click="removeWithUndo(item)"
                  class="self-start sm:col-start-3 sm:row-span-2 sm:row-start-1 shrink-0 min-w-[44px] min-h-[44px] text-muted-foreground hover:text-destructive hover:bg-transparent"
                >
                  <Trash2 class="md:hidden" />
                  <span class="hidden md:inline">{{ t('cart.remove') }}</span>
                </Button>
              </div>

              <div
                class="row-start-2 aspect-[8/5] self-start overflow-hidden rounded-xl border bg-muted sm:row-span-2 sm:row-start-1"
              >
                <SmartImage
                  :src="cartItemImage(item)"
                  :alt="getLocalizedText(item.title)"
                  img-class="h-full w-full object-contain"
                />
              </div>

              <div class="col-start-2 row-start-2 min-w-0">
                <p class="text-sm text-muted-foreground">{{ t('cart.priceLabel') }}：{{ formatPrice(item.priceAmount, totalCurrency) }}</p>
                <p v-if="itemSkuDisplay(item)" class="mt-1 text-xs text-muted-foreground break-words">{{ t('cart.skuLabel') }}：{{ itemSkuDisplay(item) }}</p>
                <p v-if="itemStockHint(item)" class="mt-1 text-xs text-muted-foreground">{{ itemStockHint(item) }}</p>
                <div class="mt-2 md:mt-3 flex flex-wrap gap-2">
                  <Badge
                    :variant="item.purchaseType === 'guest' ? 'warning' : 'success'"
                    size="xs"
                    class="uppercase tracking-wider"
                  >
                    {{ item.purchaseType === 'guest' ? t('productPurchase.guest') : t('productPurchase.member') }}
                  </Badge>
                  <Badge
                    :variant="item.fulfillmentType === 'auto' ? 'info' : 'neutral'"
                    size="xs"
                    class="uppercase tracking-wider"
                  >
                    {{ item.fulfillmentType === 'auto' ? t('products.fulfillmentType.auto') : t('products.fulfillmentType.manual') }}
                  </Badge>
                </div>
              </div>

              <div class="col-span-2 flex flex-wrap items-center justify-between gap-3 border-t pt-3 md:pt-4 sm:col-start-2">
                <div class="flex items-center gap-2">
                  <Button
                    variant="secondary"
                    size="icon"
                    @click="updateQty(item, item.quantity - 1)"
                    :disabled="item.quantity <= itemPurchaseMin(item)"
                    class="h-10 w-10 rounded-lg text-base font-medium"
                  >
                    -
                  </Button>
                  <input
                    type="number"
                    :id="`cart-qty-${item.productId}`"
                    :name="`cart-qty-${item.productId}`"
                    :value="item.quantity"
                    @change="handleQtyChange(item, $event)"
                    :min="itemPurchaseMin(item)"
                    :max="itemMaxQuantity(item)"
                    class="cart-qty-input w-12 text-center text-sm font-mono text-foreground bg-transparent border-none p-0 focus:ring-0 focus:outline-none"
                  />
                  <Button
                    variant="secondary"
                    size="icon"
                    @click="updateQty(item, item.quantity + 1)"
                    :disabled="item.quantity >= itemMaxQuantity(item)"
                    class="h-10 w-10 rounded-lg text-base font-medium"
                  >
                    +
                  </Button>
                </div>

                <div class="ml-auto text-right">
                  <p class="text-xs uppercase tracking-wider text-muted-foreground">{{ t('checkout.totalPriceLabel') }}</p>
                  <p class="text-sm font-semibold text-foreground">{{ itemSubtotal(item) }}</p>
                </div>
              </div>
              <p v-if="quantityWarning(item)" class="col-span-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2 text-xs font-medium text-warning sm:col-start-2">
                {{ quantityWarning(item) }}
              </p>
            </div>
          </article>
        </div>

        <div class="h-fit rounded-2xl border bg-card text-card-foreground p-6 lg:sticky lg:top-24">
          <h2 class="mb-4 text-lg font-bold text-foreground">{{ t('cart.summaryTitle') }}</h2>
          <div class="space-y-3 text-sm text-muted-foreground">
            <div class="flex items-center justify-between">
              <span>{{ t('cart.itemsCount') }}</span>
              <span class="font-mono text-foreground">{{ totalItems }}</span>
            </div>
            <div class="flex items-center justify-between">
              <span>{{ t('cart.totalLabel') }}</span>
              <span class="theme-price-sm text-foreground">{{ formatPrice(totalAmount, totalCurrency) }}</span>
            </div>
            <div class="rounded-lg border bg-secondary p-3 text-xs text-muted-foreground">
              {{ t('cart.disclaimer') }}
            </div>
          </div>

          <div class="mt-6 space-y-2">
            <Button as-child size="lg" class="w-full font-semibold">
              <router-link to="/checkout">
                {{ t('cart.checkout') }}
              </router-link>
            </Button>
            <Button as-child variant="secondary" size="lg" class="w-full font-semibold">
              <router-link to="/products">
                {{ t('cart.emptyAction') }}
              </router-link>
            </Button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Trash2 } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import EmptyState from '../components/EmptyState.vue'
import SmartImage from '../components/SmartImage.vue'
import CheckoutSteps from '../components/checkout/CheckoutSteps.vue'
import { useCart } from '../composables/useCart'

const { t } = useI18n()

const {
  getLocalizedText, formatPrice, totalCurrency,
  cartItems, totalItems, totalAmount,
  cartItemKey, cartItemImage, itemSkuDisplay, itemSubtotal, itemStockHint, quantityWarning,
  itemPurchaseMin, itemMaxQuantity,
  removeWithUndo, updateQty, handleQtyChange,
} = useCart()
</script>
<style scoped>
.cart-qty-input {
  appearance: textfield;
}
.cart-qty-input::-webkit-outer-spin-button,
.cart-qty-input::-webkit-inner-spin-button {
  appearance: none;
  margin: 0;
}
</style>
