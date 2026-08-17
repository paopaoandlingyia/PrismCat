import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('../src/lib/loggingRules.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
new Function('module', 'exports', compiled)(module, module.exports)
const { mergeLoggingPathRules, normalizeLoggingPathRule } = module.exports

test('normalizes Ant paths and preserves regex text', () => {
  assert.deepEqual(normalizeLoggingPathRule({ matcher: 'ant', pattern: ' v1/messages ' }), {
    matcher: 'ant',
    pattern: '/v1/messages',
  })
  assert.deepEqual(normalizeLoggingPathRule({ matcher: 'regex', pattern: ' ^/custom/.+$ ' }), {
    matcher: 'regex',
    pattern: '^/custom/.+$',
  })
})

test('merges in order and reports only incoming duplicates as skipped', () => {
  const result = mergeLoggingPathRules(
    [
      { matcher: 'ant', pattern: '/v1/responses' },
      { matcher: 'ant', pattern: 'v1/responses' },
    ],
    [
      { matcher: 'ant', pattern: '/v1/responses' },
      { matcher: 'ant', pattern: '/v1/chat/completions' },
      { matcher: 'ant', pattern: 'v1/chat/completions' },
      { matcher: 'regex', pattern: '^/custom/.+$' },
    ],
  )

  assert.deepEqual(result, {
    rules: [
      { matcher: 'ant', pattern: '/v1/responses' },
      { matcher: 'ant', pattern: '/v1/chat/completions' },
      { matcher: 'regex', pattern: '^/custom/.+$' },
    ],
    added: 2,
    skipped: 2,
  })
})
