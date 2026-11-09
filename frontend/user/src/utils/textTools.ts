/// <reference lib="es2022.intl" />

export type TextOperation = 'split' | 'replace' | 'deduplicate' | 'trim' | 'reverse'
export type TextToolResult = { output: string; error?: never } | { output?: never; error: 'emptyDelimiter' | 'emptySearch' }

export const processText = (
  input: string,
  operation: TextOperation,
  options: { delimiter?: string; search?: string; replacement?: string } = {},
): TextToolResult => {
  const text = input.replace(/\r\n?/g, '\n')
  switch (operation) {
    case 'split': {
      const delimiter = (options.delimiter ?? '').replace(/\r\n?/g, '\n')
      return delimiter ? { output: text.split(delimiter).join('\n') } : { error: 'emptyDelimiter' }
    }
    case 'replace': {
      const search = (options.search ?? '').replace(/\r\n?/g, '\n')
      const replacement = (options.replacement ?? '').replace(/\r\n?/g, '\n')
      return search ? { output: text.split(search).join(replacement) } : { error: 'emptySearch' }
    }
    case 'deduplicate':
      return { output: [...new Set(text.split('\n'))].join('\n') }
    case 'trim':
      return { output: text.split('\n').map(line => line.trim()).join('\n') }
    case 'reverse': {
      const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' })
      return { output: text.split('\n').map(line =>
        Array.from(segmenter.segment(line), item => item.segment).reverse().join(''),
      ).join('\n') }
    }
  }
}
