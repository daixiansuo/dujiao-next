import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { generateTotp, getTotpTiming, parseTotpInput, type TotpInputError } from '../utils/totp'

export const useTotpTool = () => {
  const appStore = useAppStore()
  const input = ref('')
  const code = ref('')
  const remaining = ref(30)
  const error = ref<TotpInputError | 'unavailable' | 'generationFailed' | ''>('')
  let secret: Uint8Array | null = null
  let version = 0
  let codeCounter: number | null = null
  let pending: { version: number; counter: number; promise: Promise<string> } | null = null
  let timer: ReturnType<typeof setInterval> | undefined

  const refresh = (): Promise<string> => {
    const timestamp = appStore.getServerTime()
    const { counter, remaining: seconds } = getTotpTiming(timestamp)
    remaining.value = seconds
    if (!secret || error.value) return Promise.resolve('')
    if (codeCounter === counter && code.value) return Promise.resolve(code.value)
    if (pending?.version === version && pending.counter === counter) return pending.promise
    code.value = ''
    codeCounter = null
    const requestVersion = version
    const promise = generateTotp(secret, timestamp).then(value => {
      if (version !== requestVersion) return ''
      if (getTotpTiming(appStore.getServerTime()).counter !== counter) return refresh()
      codeCounter = counter
      code.value = value
      return value
    }).catch(() => {
      if (version === requestVersion) {
        code.value = ''
        error.value = 'generationFailed'
      }
      return ''
    }).finally(() => {
      if (pending?.promise === promise) pending = null
    })
    pending = { version, counter, promise }
    return promise
  }

  watch(input, value => {
    version += 1
    secret = null
    code.value = ''
    codeCounter = null
    error.value = ''
    if (!value.trim()) return
    const parsed = parseTotpInput(value)
    if (parsed.error) {
      error.value = parsed.error
      return
    }
    if (!globalThis.crypto?.subtle) {
      error.value = 'unavailable'
      return
    }
    secret = parsed.secret!
    void refresh()
  }, { flush: 'sync' })

  const onVisibilityChange = () => {
    if (!document.hidden) void refresh()
  }
  onMounted(() => {
    timer = setInterval(() => { void refresh() }, 250)
    document.addEventListener('visibilitychange', onVisibilityChange)
  })
  onUnmounted(() => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    version += 1
    secret = null
    input.value = ''
    code.value = ''
  })

  return { input, code, remaining, error, refresh }
}
