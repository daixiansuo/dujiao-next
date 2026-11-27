import i18n from '@/i18n'
import type { ApiResponse } from './types'
import { notifyError } from '@/utils/notify'
import { adminUrl } from '@/utils/adminBase'

export type { ApiResponse }

const t = (key: string, params?: Record<string, unknown>) =>
  (params ? i18n.global.t(key, params) : i18n.global.t(key)) as string

interface NotifiedError extends Error {
  __notified?: boolean
}

const createNotifiedError = (message: string, notified = true): NotifiedError => {
  const error = new Error(message) as NotifiedError
  if (notified) error.__notified = true
  return error
}

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || ''
const API_PREFIX = '/api/v1'

const redirectToLogin = () => {
  localStorage.removeItem('admin_token')
  window.location.href = adminUrl('/login')
}

const isLoginEndpoint = (url: string) => /\/admin\/login\b/.test(url)

export interface RequestOptions {
  // 由调用页面组合完整业务提示时关闭通用 toast，错误仍然向上传递。
  skipErrorNotification?: boolean
  params?: Record<string, any>
  headers?: Record<string, string>
  blob?: boolean
  data?: any
  [key: string]: any
}

function buildUrl(base: string, path: string, params?: Record<string, any>): string {
  const url = `${base}${path}`
  if (!params) return url
  const searchParams = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null) {
      searchParams.append(key, String(value))
    }
  }
  const qs = searchParams.toString()
  return qs ? `${url}?${qs}` : url
}

function getLocale(): string {
  return (i18n.global.locale as any).value || i18n.global.locale || ''
}

function getHttpErrorMessage(status: number): string {
  switch (status) {
    case 401: return t('common.api.unauthorized')
    case 403: return t('common.api.forbidden')
    case 404: return t('common.api.notFound')
    case 500: return t('common.api.serverError')
    case 502: return t('common.api.badGateway')
    case 503: return t('common.api.serviceUnavailable')
    default: return t('common.api.requestFailedStatus', { status })
  }
}

const baseURL = `${API_BASE_URL}${API_PREFIX}`
// 与后端 http.Server WriteTimeout（120s）对齐，允许完整价格重算等较慢请求完成。
const timeout = 120000

function responseHeadersToObject(headers: Headers): Record<string, string> {
  const result: Record<string, string> = {}
  headers.forEach((value, key) => {
    result[key] = value
  })
  return result
}

async function readBlobErrorMessage(blob: Blob, fallbackMessage: string): Promise<string> {
  try {
    const text = await blob.text()
    const payload = JSON.parse(text)
    return payload?.msg || fallbackMessage
  } catch {
    return fallbackMessage
  }
}

async function request(method: string, path: string, bodyOrOptions?: any, options?: RequestOptions): Promise<{ data: any; headers?: Record<string, string> }> {
  let body: any = undefined
  let opts: RequestOptions = {}

  if (method === 'GET' || method === 'DELETE') {
    opts = bodyOrOptions || {}
    if (opts.data) {
      body = opts.data
    }
  } else {
    body = bodyOrOptions
    opts = options || {}
  }

  const rejectRequest = (message: string) => {
    if (!opts.skipErrorNotification) notifyError(message)
    return Promise.reject(createNotifiedError(message, !opts.skipErrorNotification))
  }

  const url = buildUrl(baseURL, path, opts.params)
  const headers: Record<string, string> = { ...opts.headers }

  const locale = getLocale()
  if (locale) {
    headers['X-Lang'] = locale
  }

  const token = localStorage.getItem('admin_token')
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  if (body !== undefined && !(body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)

  let response: Response
  try {
    response = await fetch(url, {
      method,
      headers,
      body: body instanceof FormData ? body : body !== undefined ? JSON.stringify(body) : undefined,
      signal: controller.signal,
    })
  } catch (err: any) {
    const message = t(err?.name === 'AbortError' ? 'common.api.timeout' : 'common.api.networkError')
    return rejectRequest(message)
  } finally {
    clearTimeout(timer)
  }

  // Blob response
  if (opts.blob) {
    const contentType = response.headers.get('content-type') || ''
    const blob = await response.blob()
    if (!response.ok) {
      const fallbackMessage = getHttpErrorMessage(response.status)
      const message = contentType.includes('application/json')
        ? await readBlobErrorMessage(blob, fallbackMessage)
        : fallbackMessage
      if (response.status === 401 && !isLoginEndpoint(path)) {
        redirectToLogin()
      }
      return rejectRequest(message)
    }
    if (contentType.includes('application/json')) {
      const fallbackMessage = t('common.api.requestFailed')
      const message = await readBlobErrorMessage(blob, fallbackMessage)
      return rejectRequest(message)
    }
    return { data: blob, headers: responseHeadersToObject(response.headers) }
  }

  // Parse JSON
  let data: ApiResponse
  try {
    data = await response.json()
  } catch {
    if (!response.ok) {
      const status = response.status
      const message = getHttpErrorMessage(status)
      if (status === 401 && !isLoginEndpoint(path)) {
        redirectToLogin()
      }
      return rejectRequest(message)
    }
    const message = t('common.api.responseMissing')
    return rejectRequest(message)
  }

  // HTTP error with JSON body
  if (!response.ok) {
    const status = response.status
    const message = data?.msg || getHttpErrorMessage(status)
    if (status === 401 && !isLoginEndpoint(path)) {
      redirectToLogin()
    }
    return rejectRequest(message)
  }

  // Business error check
  if (typeof data.status_code !== 'undefined' && data.status_code !== 0) {
    const fallbackMessage = t('common.api.requestFailed')
    const message = data.msg || fallbackMessage
    if (data.status_code === 401 && !isLoginEndpoint(path)) {
      redirectToLogin()
      return rejectRequest(message)
    }
    // 合规声明拦截：消息由路由守卫/页面 wrapper 处理，不展示通用 toast
    if (message === 'compliance_required' || message === 'compliance_required_by_super_admin') {
      return Promise.reject(createNotifiedError(message))
    }
    return rejectRequest(message)
  }

  return { data }
}

export const api = {
  get: (path: string, options?: RequestOptions) => request('GET', path, options),
  post: (path: string, body?: any, options?: RequestOptions) => request('POST', path, body, options),
  put: (path: string, body?: any, options?: RequestOptions) => request('PUT', path, body, options),
  patch: (path: string, body?: any, options?: RequestOptions) => request('PATCH', path, body, options),
  delete: (path: string, options?: RequestOptions) => request('DELETE', path, options),
}
