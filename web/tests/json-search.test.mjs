import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

async function withModules(run) {
    const server = await createServer({
        appType: 'custom',
        logLevel: 'silent',
        server: { middlewareMode: true },
    })
    try {
        const jsonSearch = await server.ssrLoadModule('/src/lib/jsonSearch.ts')
        const jsonViewer = await server.ssrLoadModule('/src/components/JsonViewer.tsx')
        await run({ ...jsonSearch, ...jsonViewer })
    } finally {
        await server.close()
    }
}

test('text search preserves original indexes for expanding Unicode lowercase mappings', async () => {
    await withModules(({ collectTextSearchMatches }) => {
        const result = collectTextSearchMatches('İİ', 'İ', 10)
        assert.deepEqual(result, {
            ranges: [
                { start: 0, end: 1 },
                { start: 1, end: 2 },
            ],
            truncated: false,
        })
    })
})

test('text search stops collecting ranges at the render limit', async () => {
    await withModules(({ collectTextSearchMatches }) => {
        const result = collectTextSearchMatches('a'.repeat(10 * 1024 * 1024), 'a', 500)
        assert.equal(result.ranges.length, 500)
        assert.equal(result.truncated, true)
    })
})

test('JSON search creates one bounded plan with ancestor expansion paths', async () => {
    await withModules(({ createJsonSearchPlan }) => {
        const plan = createJsonSearchPlan({
            needle: 'needle',
            nested: { value: 'needle' },
        }, 'needle', 10)

        assert.equal(plan.matchCount, 3)
        assert.equal(plan.truncated, false)
        assert.equal(plan.expandedPaths.has(''), true)
        assert.equal(plan.expandedPaths.has('/nested'), true)
        assert.equal(plan.visiblePaths.has('/nested/value'), true)
    })
})

test('JSON search limits both highlighted matches and rendered result paths', async () => {
    await withModules(({ createJsonSearchPlan }) => {
        const plan = createJsonSearchPlan({
            items: Array.from({ length: 1_000 }, () => 'needle'),
        }, 'needle', 5)

        assert.equal(plan.matchCount, 5)
        assert.equal(plan.truncated, true)
        assert.equal(plan.visiblePaths.has('/items/0'), true)
        assert.equal(plan.visiblePaths.has('/items/4'), true)
        assert.equal(plan.visiblePaths.has('/items/5'), false)
    })
})

test('JSON search excludes collapsed base64 payloads from visible matches', async () => {
    await withModules(({ createJsonSearchPlan }) => {
        const pngBase64 = `iVBORw0KGgo${'A'.repeat(220)}`
        const plan = createJsonSearchPlan({ blob: pngBase64 }, 'iVBOR', 10)
        assert.equal(plan.matchCount, 0)
    })
})
