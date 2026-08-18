import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/lib/bodyStatus.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
vm.runInNewContext(compiled, { module, exports: module.exports })
const { getLogBodyStatus } = module.exports

const body = (part, truncated = false) => ({ part, truncated })

test('complete blob bodies are not marked truncated when response transfer was interrupted', () => {
    assert.deepEqual({ ...getLogBodyStatus({
        truncated: true,
        bodies: [body('request'), body('response')],
    }) }, {
        requestTruncated: false,
        responseTruncated: false,
        responseInterrupted: true,
    })
})

test('request and response truncation are derived independently', () => {
    assert.deepEqual({ ...getLogBodyStatus({
        truncated: true,
        bodies: [body('request', true), body('response')],
    }) }, {
        requestTruncated: true,
        responseTruncated: false,
        responseInterrupted: false,
    })
    assert.deepEqual({ ...getLogBodyStatus({
        truncated: true,
        bodies: [body('request'), body('response', true)],
    }) }, {
        requestTruncated: false,
        responseTruncated: true,
        responseInterrupted: false,
    })
})

test('body storage failures are not mislabeled as response interruption', () => {
    assert.equal(getLogBodyStatus({
        truncated: true,
        body_storage_error: 'response: write failed',
        bodies: [],
    }).responseInterrupted, false)
})
