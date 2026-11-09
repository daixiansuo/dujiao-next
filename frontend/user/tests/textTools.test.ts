import test from 'node:test'
import assert from 'node:assert/strict'
import { processText } from '../src/utils/textTools.ts'

test('split treats delimiters literally and normalizes line endings', () => {
  assert.deepEqual(processText('a.*b.*\r\nc', 'split', { delimiter: '.*' }), { output: 'a\nb\n\nc' })
  assert.deepEqual(processText('a\r\nb\rc', 'split', { delimiter: '\r\n' }), { output: 'a\nb\nc' })
  assert.deepEqual(processText('a', 'split'), { error: 'emptyDelimiter' })
})

test('replace treats search and replacement literally, allows removal', () => {
  assert.deepEqual(processText('a.*b.*c', 'replace', { search: '.*', replacement: '$&' }), { output: 'a$&b$&c' })
  assert.deepEqual(processText('哈哈你好哈哈', 'replace', { search: '哈哈' }), { output: '你好' })
  assert.deepEqual(processText('a', 'replace'), { error: 'emptySearch' })
})

test('deduplication preserves exact contents and first occurrence order', () => {
  assert.deepEqual(processText(' b\r\na\rb\n b\na\n\n', 'deduplicate'), { output: ' b\na\nb\n' })
})

test('trim preserves interior whitespace, empty lines and line order', () => {
  assert.deepEqual(processText('  a  b \r\n\t 中文 \r\n   \nc', 'trim'), { output: 'a  b\n中文\n\nc' })
})

test('reverse operates within each line without splitting emoji or combining marks', () => {
  assert.deepEqual(processText('中文👩🏽‍💻a\r\ne\u0301🇨🇳\n', 'reverse'), { output: 'a👩🏽‍💻文中\n🇨🇳e\u0301\n' })
})

test('empty input remains empty for all operations with valid options', () => {
  for (const operation of ['split', 'replace', 'deduplicate', 'trim', 'reverse'] as const) {
    assert.deepEqual(processText('', operation, { delimiter: ',', search: 'a' }), { output: '' })
  }
})
