const warningKeys = new Map<string, string>([
  ['license_lease_expiring_soon', 'warningLeaseExpiringSoon'],
  ['license_lease_expired', 'warningLeaseExpired'],
  ['configuration_refresh_failed', 'warningConfigurationRefreshFailed'],
  ['public_configuration_refresh_failed', 'warningPublicConfigurationRefreshFailed'],
  ['bot_token_changed_restart_required', 'warningBotTokenChangedRestartRequired'],
  ['telegram_identity_check_failed', 'warningTelegramIdentityCheckFailed'],
  ['site_telegram_bot_mismatch', 'warningSiteTelegramBotMismatch'],
])

export const formatTelegramBotWarning = (value: string, translate: (key: string) => string): string => {
  const key = warningKeys.get(value.trim().toLowerCase())
  return key ? translate(`telegramBot.status.${key}`) : value
}
