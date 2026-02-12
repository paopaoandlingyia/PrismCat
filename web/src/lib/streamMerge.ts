/**
 * streamMerge.ts
 *
 * Parses raw streaming response bodies (SSE / NDJSON) and merges
 * OpenAI-style delta chunks into a single, readable response object.
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface MergeResult {
    /** The merged object (or array of parsed chunks if no merge strategy matched). */
    merged: unknown
    /** How many chunks were parsed. */
    chunks: number
    /** The detected format. */
    format: 'sse' | 'ndjson' | 'unknown'
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Attempt to merge a raw streaming body string into a single readable object.
 *
 * Supports:
 * - SSE (text/event-stream): lines starting with "data: "
 * - NDJSON: one JSON object per line
 *
 * For OpenAI-compatible responses it deep-merges `choices[].delta` into
 * `choices[].message` so the result resembles a non-streaming response.
 */
export function mergeStreamBody(raw: string): MergeResult | null {
    if (!raw || !raw.trim()) return null

    // 1. Try SSE
    const sseChunks = parseSSE(raw)
    if (sseChunks.length > 0) {
        return {
            merged: mergeChunks(sseChunks),
            chunks: sseChunks.length,
            format: 'sse',
        }
    }

    // 2. Try NDJSON
    const ndjsonChunks = parseNDJSON(raw)
    if (ndjsonChunks.length > 0) {
        return {
            merged: mergeChunks(ndjsonChunks),
            chunks: ndjsonChunks.length,
            format: 'ndjson',
        }
    }

    return null
}

// ---------------------------------------------------------------------------
// Parsers
// ---------------------------------------------------------------------------

/** Parse SSE formatted text into an array of JSON objects. */
function parseSSE(raw: string): Record<string, unknown>[] {
    const chunks: Record<string, unknown>[] = []
    const lines = raw.split('\n')

    for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed.startsWith('data:')) continue

        const payload = trimmed.slice(5).trim()
        if (!payload || payload === '[DONE]') continue

        try {
            const parsed = JSON.parse(payload)
            if (parsed && typeof parsed === 'object') {
                chunks.push(parsed as Record<string, unknown>)
            }
        } catch {
            // not valid JSON – skip
        }
    }

    return chunks
}

/** Parse NDJSON (one JSON per line) into an array of JSON objects. */
function parseNDJSON(raw: string): Record<string, unknown>[] {
    const lines = raw.split('\n').filter((l) => l.trim())
    if (lines.length < 2) return [] // need at least 2 lines to be meaningful

    const chunks: Record<string, unknown>[] = []
    let parsedCount = 0

    for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed) continue
        try {
            const parsed = JSON.parse(trimmed)
            if (parsed && typeof parsed === 'object') {
                chunks.push(parsed as Record<string, unknown>)
            }
            parsedCount++
        } catch {
            // If majority of lines aren't JSON, this isn't NDJSON
        }
    }

    // Require at least 50% lines to be valid JSON to consider it NDJSON
    if (parsedCount < lines.length * 0.5) return []

    return chunks
}

// ---------------------------------------------------------------------------
// Merge strategies
// ---------------------------------------------------------------------------

/**
 * Merge an array of streaming chunks into a single response object.
 * Tries OpenAI-style merge first, then falls back to generic concatenation.
 */
function mergeChunks(chunks: Record<string, unknown>[]): unknown {
    if (chunks.length === 0) return null

    // Detect OpenAI-compatible format: has "choices" with "delta"
    const isOpenAI = chunks.some(
        (c) =>
            Array.isArray(c.choices) &&
            c.choices.length > 0 &&
            typeof (c.choices as any[])[0]?.delta === 'object'
    )

    if (isOpenAI) {
        return mergeOpenAIChunks(chunks)
    }

    // Detect Ollama format: has "message" with "content" and "done" field
    const isOllama = chunks.some(
        (c) =>
            typeof c.message === 'object' &&
            c.message !== null &&
            'content' in (c.message as Record<string, unknown>) &&
            'done' in c
    )

    if (isOllama) {
        return mergeOllamaChunks(chunks)
    }

    // Fallback: return all chunks as array
    return chunks
}

// ---------------------------------------------------------------------------
// OpenAI merge
// ---------------------------------------------------------------------------

function mergeOpenAIChunks(chunks: Record<string, unknown>[]): Record<string, unknown> {
    // Use the last chunk as the base (it usually has usage/finish_reason)
    const base = { ...chunks[chunks.length - 1] }
    const mergedChoices: Record<string, unknown>[] = []

    // Group deltas by choice index
    const choiceMap = new Map<number, { role: string; content: string; tool_calls: any[]; finish_reason: string | null }>()

    for (const chunk of chunks) {
        const choices = chunk.choices as any[] | undefined
        if (!Array.isArray(choices)) continue

        for (const choice of choices) {
            const idx = choice.index ?? 0
            if (!choiceMap.has(idx)) {
                choiceMap.set(idx, { role: '', content: '', tool_calls: [], finish_reason: null })
            }
            const acc = choiceMap.get(idx)!

            const delta = choice.delta
            if (delta) {
                if (delta.role) acc.role = delta.role
                if (delta.content) acc.content += delta.content

                // Merge tool_calls if present
                if (Array.isArray(delta.tool_calls)) {
                    for (const tc of delta.tool_calls) {
                        const tcIdx = tc.index ?? 0
                        if (!acc.tool_calls[tcIdx]) {
                            acc.tool_calls[tcIdx] = { id: tc.id || '', type: tc.type || 'function', function: { name: '', arguments: '' } }
                        }
                        if (tc.function?.name) acc.tool_calls[tcIdx].function.name += tc.function.name
                        if (tc.function?.arguments) acc.tool_calls[tcIdx].function.arguments += tc.function.arguments
                    }
                }
            }

            if (choice.finish_reason) acc.finish_reason = choice.finish_reason
        }
    }

    // Build merged choices
    for (const [idx, acc] of choiceMap) {
        const message: Record<string, unknown> = {
            role: acc.role || 'assistant',
            content: acc.content,
        }
        if (acc.tool_calls.length > 0) {
            message.tool_calls = acc.tool_calls.filter(Boolean)
        }
        mergedChoices.push({
            index: idx,
            message,
            finish_reason: acc.finish_reason,
        })
    }

    // Sort by index
    mergedChoices.sort((a, b) => (a.index as number) - (b.index as number))

    // Build final result
    const result: Record<string, unknown> = {}
    if (base.id !== undefined) result.id = base.id
    if (base.object !== undefined) result.object = 'chat.completion'
    if (base.created !== undefined) result.created = base.created
    if (base.model !== undefined) result.model = base.model
    result.choices = mergedChoices
    if (base.usage !== undefined) result.usage = base.usage

    return result
}

// ---------------------------------------------------------------------------
// Ollama merge
// ---------------------------------------------------------------------------

function mergeOllamaChunks(chunks: Record<string, unknown>[]): Record<string, unknown> {
    const lastChunk = chunks[chunks.length - 1]
    let content = ''
    let role = ''

    for (const chunk of chunks) {
        const msg = chunk.message as Record<string, unknown> | undefined
        if (msg) {
            if (msg.role && typeof msg.role === 'string') role = msg.role
            if (msg.content && typeof msg.content === 'string') content += msg.content
        }
    }

    const result: Record<string, unknown> = {
        ...lastChunk,
        message: {
            role: role || 'assistant',
            content,
        },
    }

    return result
}
