import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/lib/backupStatus.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
vm.runInNewContext(compiled, { module, exports: module.exports })
const { getLogBackupState } = module.exports

test('backup status distinguishes pending, verified cleanup, saved, and restored logs', () => {
    assert.equal(getLogBackupState({ origin: 'live', annotation: { saved: false } }), 'pending')
    assert.equal(getLogBackupState({ origin: 'live', backup_verified_at: '2026-08-18T00:00:00Z', annotation: { saved: false } }), 'verified_cleanup')
    assert.equal(getLogBackupState({ origin: 'live', backup_verified_at: '2026-08-18T00:00:00Z', annotation: { saved: true } }), 'verified_saved')
    assert.equal(getLogBackupState({ origin: 'archive_import', annotation: { saved: false } }), 'restored')
})
