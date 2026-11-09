import test from 'node:test'
import assert from 'node:assert/strict'
import { generateTotp, getTotpTiming, parseTotpInput } from '../src/utils/totp.ts'

const secret = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ'
const parsed = parseTotpInput(secret)
assert.ok(parsed.secret)

test('SHA-1 TOTP matches RFC 6238 vectors, including leading zeroes and large counters', async () => {
  for (const [seconds, expected] of [
    [59, '287082'], [1111111109, '081804'], [1111111111, '050471'],
    [1234567890, '005924'], [2000000000, '279037'], [20000000000, '353130'],
  ] as const) {
    assert.equal(await generateTotp(parsed.secret!, seconds * 1000), expected)
  }
})

test('counter and countdown use absolute time across period boundaries', async () => {
  assert.deepEqual(getTotpTiming(29999), { counter: 0, remaining: 1 })
  assert.deepEqual(getTotpTiming(30000), { counter: 1, remaining: 30 })
  assert.deepEqual(getTotpTiming(59999), { counter: 1, remaining: 1 })
  assert.equal(await generateTotp(parsed.secret!, 29999), '755224')
  assert.equal(await generateTotp(parsed.secret!, 30000), '287082')
  assert.throws(() => getTotpTiming(-1), RangeError)
  assert.throws(() => getTotpTiming(NaN), RangeError)
})

test('Base32 accepts formatting and correct padding, rejects malformed encodings', () => {
  assert.deepEqual(parseTotpInput(' gezd-gnbv gy3tqojq\ngezdgnbvgy3tqojq '), parsed)
  assert.deepEqual(parseTotpInput('MY======'), { secret: new Uint8Array([102]) })
  assert.deepEqual(parseTotpInput('MY'), { secret: new Uint8Array([102]) })
  for (const value of ['', 'A', 'ABC', 'ABCDEF', 'MZ', 'MY=', 'MY=======', 'MY======A', '12345678']) {
    assert.deepEqual(parseTotpInput(value), { error: 'invalidSecret' }, value)
  }
})

test('otpauth accepts supported explicit parameters and defaults', () => {
  assert.deepEqual(parseTotpInput(`otpauth://totp/Example:alice?secret=${secret}&issuer=Example`), parsed)
  assert.deepEqual(parseTotpInput(`otpauth://totp/alice?secret=${secret}&algorithm=sha1&digits=6&period=30`), parsed)
})

test('otpauth rejects missing, ambiguous and unsupported settings', () => {
  for (const value of ['otpauth://totp/alice', `otpauth://totp/?secret=${secret}`,
    `https://totp/alice?secret=${secret}`, `otpauth://totp/alice?secret=${secret}&secret=MY`]) {
    assert.deepEqual(parseTotpInput(value), { error: 'invalidUri' }, value)
  }
  assert.deepEqual(parseTotpInput(`otpauth://hotp/alice?secret=${secret}&counter=0`), { error: 'unsupportedType' })
  for (const parameter of ['algorithm=SHA256', 'algorithm=', 'digits=8', 'digits=abc', 'period=60', 'period=0']) {
    assert.deepEqual(parseTotpInput(`otpauth://totp/alice?secret=${secret}&${parameter}`), { error: 'unsupportedParameters' })
  }
})
