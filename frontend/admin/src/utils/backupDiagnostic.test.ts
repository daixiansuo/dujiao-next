import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { backupDiagnostic } from './backupDiagnostic'

describe('backup translations', () => {
  it.each(['zh-CN', 'zh-TW', 'en-US'] as const)('translates status diagnostics and all tab messages in %s', locale => {
    const messages = i18n.global.getLocaleMessage(locale) as { admin: { settings: { backup: Record<string, unknown> } } }
    const english = i18n.global.getLocaleMessage('en-US') as typeof messages
    const keys = (object: Record<string, unknown>, prefix = ''): string[] => Object.entries(object).flatMap(([key, value]) => {
      const path = prefix ? `${prefix}.${key}` : key
      return typeof value === 'object' && value !== null ? keys(value as Record<string, unknown>, path) : [path]
    })
    expect(keys(messages.admin.settings.backup)).toEqual(keys(english.admin.settings.backup))
    const t = (key: string) => i18n.global.t(key, {}, { locale })
    for (const value of ['error.backup_not_ready', 'error.backup_failed', 'database export failed', 'upload outcome pending verification']) {
      const label = backupDiagnostic(value, t)
      expect(label).not.toBe(value)
      expect(label).not.toContain('admin.settings.backup.')
    }
    const localizedDiagnostics = {
      'zh-CN': ['备份操作失败', '备份尚未就绪，请检查环境和配置'],
      'zh-TW': ['備份操作失敗', '備份尚未就緒，請檢查環境與設定'],
      'en-US': ['Backup operation failed', 'Backup is not ready; check the environment and configuration'],
    }
    for (const message of localizedDiagnostics[locale]) {
      expect(backupDiagnostic(message, t)).toBe(message)
    }
    expect(backupDiagnostic('Unexpected storage diagnostic', t)).toBe('Unexpected storage diagnostic')
  })
})
