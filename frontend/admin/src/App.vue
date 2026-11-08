<script setup lang="ts">
import { watchEffect } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSiteBrandStore } from '@/stores/siteBrand'
import NoticeHost from '@/components/NoticeHost.vue'
import ConfirmDialogHost from '@/components/ConfirmDialogHost.vue'

const { t } = useI18n()
const route = useRoute()
const siteBrandStore = useSiteBrandStore()

watchEffect(() => {
  if (route.name === 'login') {
    document.title = siteBrandStore.siteName || t('admin.layout.browserTitle')
  }
})
</script>

<template>
  <RouterView />
  <NoticeHost />
  <ConfirmDialogHost />
</template>
