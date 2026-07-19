import assert from 'node:assert/strict'
import test from 'node:test'

import { createServer } from 'vite'

async function withStreamMerge(run) {
    const server = await createServer({
        appType: 'custom',
        logLevel: 'silent',
        server: { middlewareMode: true },
    })
    try {
        const streamMerge = await server.ssrLoadModule('/src/lib/streamMerge.ts')
        await run(streamMerge)
    } finally {
        await server.close()
    }
}

test('Gemini thought text and answer text remain separate parts', async () => {
    await withStreamMerge(({ mergeStreamBody }) => {
        const raw = [
            {
                candidates: [{ content: { role: 'model', parts: [{ text: 'thinking', thought: true }] } }],
            },
            {
                candidates: [{ content: { role: 'model', parts: [{ text: '1' }] } }],
            },
            {
                candidates: [{ content: { role: 'model', parts: [{ text: '', thoughtSignature: 'sig' }] } }],
            },
        ]
            .map((chunk) => `data: ${JSON.stringify(chunk)}\n\n`)
            .join('')

        const result = mergeStreamBody(raw)
        assert.ok(result)
        assert.deepEqual(result.merged.candidates[0].content.parts, [
            { text: 'thinking', thought: true },
            { text: '1', thoughtSignature: 'sig' },
        ])
    })
})
