export type TotpInputError = 'invalidSecret' | 'invalidUri' | 'unsupportedType' | 'unsupportedParameters'
export type TotpInput = { secret: Uint8Array; error?: never } | { secret?: never; error: TotpInputError }

export const TOTP_PERIOD = 30

const decodeBase32 = (input: string): TotpInput => {
  const normalized = input.replace(/[\s-]/g, '').toUpperCase()
  if (!/^[A-Z2-7]+=*$/.test(normalized)) return { error: 'invalidSecret' }
  const unpadded = normalized.replace(/=+$/, '')
  const remainder = unpadded.length % 8
  if (![0, 2, 4, 5, 7].includes(remainder)) return { error: 'invalidSecret' }
  if (normalized.includes('=') && normalized.length - unpadded.length !== (8 - remainder) % 8) {
    return { error: 'invalidSecret' }
  }

  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  const bytes: number[] = []
  let bits = 0
  let buffer = 0
  for (const character of unpadded) {
    buffer = (buffer << 5) | alphabet.indexOf(character)
    bits += 5
    if (bits >= 8) {
      bits -= 8
      bytes.push((buffer >>> bits) & 255)
      buffer &= (1 << bits) - 1
    }
  }
  // Nonzero unused bits are not a canonical Base32 encoding.
  if (!bytes.length || buffer !== 0) return { error: 'invalidSecret' }
  return { secret: new Uint8Array(bytes) }
}

export const parseTotpInput = (input: string): TotpInput => {
  const value = input.trim()
  if (!value.includes(':')) return decodeBase32(value)

  let uri: URL
  try {
    uri = new URL(value)
  } catch {
    return { error: 'invalidUri' }
  }
  if (uri.protocol !== 'otpauth:' || uri.username || uri.password || uri.port || uri.hash) {
    return { error: 'invalidUri' }
  }
  if (uri.hostname !== 'totp') return { error: 'unsupportedType' }
  if (uri.pathname.length <= 1) return { error: 'invalidUri' }
  for (const key of ['secret', 'algorithm', 'digits', 'period']) {
    if (uri.searchParams.getAll(key).length > 1) return { error: 'invalidUri' }
  }
  const secret = uri.searchParams.get('secret')
  if (!secret) return { error: 'invalidUri' }
  const algorithm = uri.searchParams.get('algorithm')
  const digits = uri.searchParams.get('digits')
  const period = uri.searchParams.get('period')
  if ((algorithm !== null && algorithm.toUpperCase() !== 'SHA1') ||
      (digits !== null && digits !== '6') || (period !== null && period !== '30')) {
    return { error: 'unsupportedParameters' }
  }
  return decodeBase32(secret)
}

export const getTotpTiming = (timestampMs: number) => {
  if (!Number.isSafeInteger(Math.floor(timestampMs)) || timestampMs < 0) throw new RangeError('Invalid timestamp')
  const seconds = Math.floor(timestampMs / 1000)
  return { counter: Math.floor(seconds / TOTP_PERIOD), remaining: TOTP_PERIOD - seconds % TOTP_PERIOD }
}

export const generateTotp = async (secret: Uint8Array, timestampMs: number): Promise<string> => {
  const { counter } = getTotpTiming(timestampMs)
  const message = new ArrayBuffer(8)
  new DataView(message).setBigUint64(0, BigInt(counter))
  const key = await globalThis.crypto.subtle.importKey(
    'raw', new Uint8Array(secret), { name: 'HMAC', hash: 'SHA-1' }, false, ['sign'],
  )
  const signature = new Uint8Array(await globalThis.crypto.subtle.sign('HMAC', key, message))
  const offset = signature[signature.length - 1]! & 15
  const binary = new DataView(signature.buffer).getUint32(offset) & 0x7fffffff
  return String(binary % 1_000_000).padStart(6, '0')
}
