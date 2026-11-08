const diagnostics: Record<string, string> = {
  'error.backup_busy': 'busy',
  'error.backup_not_found': 'notFound',
  'error.backup_unsupported': 'unsupported',
  'error.backup_invalid_config': 'invalidConfig',
  'error.backup_target_change': 'targetChange',
  'error.backup_not_ready': 'notReady',
  'error.backup_failed': 'failed',
  'error.backup_config_private': 'configPrivate',
  'database backup tool unavailable': 'toolUnavailable',
  'cannot create backup temporary directory': 'tempDirectory',
  'database export failed': 'exportFailed',
  'database backup canceled': 'canceled',
  'cannot open exported database': 'openExport',
  'invalid database export': 'invalidExport',
  'cannot hash exported database': 'hashExport',
  'cannot read exported database': 'readExport',
  'cannot persist backup upload metadata': 'uploadMetadata',
  'uploaded database verification failed': 'verificationFailed',
  'uploaded database object not found': 'objectNotFound',
  'upload outcome pending verification': 'pendingVerification',
  'database backup interrupted during export': 'interrupted',
}

export const backupDiagnostic = (message: string, t: (key: string) => string) => {
  const key = diagnostics[message]
  return key ? t(`admin.settings.backup.errors.${key}`) : message
}
