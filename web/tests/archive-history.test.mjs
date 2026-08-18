import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/lib/archiveHistory.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
vm.runInNewContext(compiled, { module, exports: module.exports })
const { getArchivePagination } = module.exports

test('archive pagination handles empty, middle, and out-of-range pages', () => {
    assert.deepEqual({ ...getArchivePagination(0, 50, 0) }, {
        current: 0, pages: 0, previousOffset: 0, nextOffset: 0, hasPrevious: false, hasNext: false,
    })
    assert.deepEqual({ ...getArchivePagination(50, 50, 121) }, {
        current: 2, pages: 3, previousOffset: 0, nextOffset: 100, hasPrevious: true, hasNext: true,
    })
    assert.deepEqual({ ...getArchivePagination(500, 50, 121) }, {
        current: 3, pages: 3, previousOffset: 50, nextOffset: 100, hasPrevious: true, hasNext: false,
    })
})
