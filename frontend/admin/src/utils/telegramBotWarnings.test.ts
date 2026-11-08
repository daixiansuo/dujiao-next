import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { formatTelegramBotWarning } from './telegramBotWarnings'

const diagnostics = [
  'configuration_refresh_failed',
  'public_configuration_refresh_failed',
  'bot_token_changed_restart_required',
  'telegram_identity_check_failed',
  'site_telegram_bot_mismatch',
]

describe('Telegram Bot runtime warnings', () => {
  it.each(['zh-CN', 'zh-TW', 'en-US'] as const)('localizes all Bot diagnostics in %s', (locale) => {
    const translate = (key: string) => {
      expect(i18n.global.te(key, locale)).toBe(true)
      return i18n.global.t(key, {}, { locale })
    }
    for (const code of diagnostics) {
      const formatted = formatTelegramBotWarning(code, translate)
      expect(formatted).not.toBe(code)
      expect(formatted).not.toContain('telegramBot.status.')
      expect(formatted.length).toBeGreaterThan(10)
    }
    expect(formatTelegramBotWarning('  CONFIGURATION_REFRESH_FAILED  ', translate))
      .toBe(formatTelegramBotWarning('configuration_refresh_failed', translate))
  })

  it('preserves unrecognized third-party warnings exactly', () => {
    const original = '  Upstream extension: custom_WARNING  '
    expect(formatTelegramBotWarning(original, () => { throw new Error('unexpected translation') })).toBe(original)
    expect(formatTelegramBotWarning('constructor', () => { throw new Error('unexpected translation') })).toBe('constructor')
  })
})
