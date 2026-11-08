import { defineStore } from 'pinia'

interface SiteBrand {
  site_name?: string
  site_logo?: string
  site_url?: string
}

export const useSiteBrandStore = defineStore('adminSiteBrand', {
  state: () => ({
    siteName: '',
    siteLogo: '',
    siteUrl: '',
  }),
  actions: {
    setBrand(brand?: SiteBrand) {
      this.siteName = String(brand?.site_name || '').trim()
      this.siteLogo = String(brand?.site_logo || '').trim()
      this.siteUrl = String(brand?.site_url || '').trim()
    },
  },
})
