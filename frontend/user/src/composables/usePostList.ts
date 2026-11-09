import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'
import { postAPI, postCategoryAPI } from '../api'
import { debounceAsync } from '../utils/debounce'
import { usePageSeo } from './usePageSeo'

/**
 * 文章/公告列表共享逻辑（Blog/Notice，classic + vault 双模板共用）。
 * 分类筛选由页面显式启用，其他调用沿用原有行为。
 * Blog 启用搜索；Notice 无搜索框（searchKeyword 恒为空，watch 永不触发，行为一致）。
 */
export function usePostList(
  type: 'blog' | 'notice',
  seo: { title: () => string; canonicalPath: string },
  options: { categoryFilter?: boolean } = {},
) {
  const router = useRouter()
  const appStore = useAppStore()

  usePageSeo({
    title: seo.title,
    canonicalPath: () => seo.canonicalPath,
  })

  const loading = ref(true)
  const posts = ref<any[]>([])
  const currentPage = ref(1)
  const pageSize = ref(12)
  const total = ref(0)
  const totalPages = ref(0)
  const searchKeyword = ref('')
  const categories = ref<Array<{ id: number; name: Record<string, string> }>>([])
  const selectedCategory = ref<number | null>(null)
  const categoryFilterEnabled = type === 'blog' && options.categoryFilter === true
  let requestVersion = 0

  const getLocalizedText = (jsonData: any) => {
    if (!jsonData) return ''
    const locale = appStore.locale
    return jsonData[locale] || jsonData['zh-CN'] || jsonData['en-US'] || ''
  }

  const formatDate = (dateString: string) => {
    if (!dateString) return ''
    const date = new Date(dateString)
    return date.toLocaleDateString(appStore.locale, {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    })
  }

  const loadPosts = async () => {
    const version = ++requestVersion
    loading.value = true
    try {
      const params: Record<string, any> = {
        type,
        page: currentPage.value,
        page_size: pageSize.value,
      }
      const keyword = searchKeyword.value.trim()
      if (keyword) {
        params.search = keyword
      }
      if (categoryFilterEnabled && selectedCategory.value !== null) {
        params.category_id = selectedCategory.value
      }
      const response = await postAPI.list(params)
      if (version !== requestVersion) return
      posts.value = response.data.data || []
      total.value = response.data.pagination?.total || 0
      totalPages.value = response.data.pagination?.total_page || 0
    } catch (error) {
      if (version !== requestVersion) return
      posts.value = []
      total.value = 0
      totalPages.value = 0
      console.error('Failed to load posts:', error)
    } finally {
      if (version === requestVersion) loading.value = false
    }
  }

  const debouncedLoadPosts = debounceAsync(loadPosts, 300)

  const reloadPosts = () => {
    // 立即使在途请求失效，避免防抖等待期间旧结果覆盖当前选择。
    requestVersion++
    loading.value = true
    debouncedLoadPosts()
  }

  const loadCategories = async () => {
    try {
      const response = await postCategoryAPI.list()
      categories.value = response.data.data || []
    } catch (error) {
      console.error('Failed to load post categories:', error)
    }
  }

  const changeCategory = (categoryID: number | null) => {
    if (!categoryFilterEnabled || categoryID === selectedCategory.value) return
    selectedCategory.value = categoryID
  }

  const goToPost = (slug: string) => {
    router.push(`/blog/${slug}`)
  }

  const changePage = (page: number) => {
    if (page < 1 || page > totalPages.value || page === currentPage.value) return
    currentPage.value = page
    reloadPosts()
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  watch([searchKeyword, selectedCategory], () => {
    currentPage.value = 1
    reloadPosts()
  }, { flush: 'sync' })

  onMounted(() => {
    loadPosts()
    if (categoryFilterEnabled) loadCategories()
  })

  onUnmounted(() => {
    requestVersion++
    debouncedLoadPosts.cancel()
  })

  return {
    loading,
    posts,
    currentPage,
    totalPages,
    total,
    searchKeyword,
    categories,
    selectedCategory,
    changeCategory,
    getLocalizedText,
    formatDate,
    goToPost,
    changePage,
  }
}
