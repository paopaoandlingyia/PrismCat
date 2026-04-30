/**
 * streamMerge.ts
 *
 * Parses raw streaming response bodies (SSE / NDJSON) and produces a
 * readable merged view when the upstream protocol is recognized.
 */

type StreamFormat = 'sse' | 'ndjson' | 'unknown'
type StreamProtocol = 'openai-chat' | 'openai-responses' | 'claude-messages' | 'ollama' | 'gemini' | 'generic'

export interface MergeResult {
    merged: unknown
    chunks: number
    format: StreamFormat
    protocol: StreamProtocol
}

interface SseEvent {
    event: string
    data: Record<string, unknown>
}

interface ToolCallAccumulator {
    id: string
    type: string
    function: {
        name: string
        arguments: string
    }
}

interface OpenAIChoiceAccumulator {
    role: string
    content: string
    reasoning: string
    reasoningContent: string
    reasoningDetails: unknown[]
    images: unknown[]
    toolCalls: Array<ToolCallAccumulator | undefined>
    finishReason: string | null
}

interface GeminiCandidateAccumulator {
    candidate: Record<string, unknown>
    content: Record<string, unknown>
    parts: Array<Record<string, unknown>>
}

const CLAUDE_EVENT_NAMES = new Set([
    'message_start',
    'message_delta',
    'message_stop',
    'content_block_start',
    'content_block_delta',
    'content_block_stop',
    'ping',
    'error',
])

const CLAUDE_DATA_TYPES = new Set([
    'message_start',
    'message_delta',
    'message_stop',
    'content_block_start',
    'content_block_delta',
    'content_block_stop',
    'ping',
    'error',
])

export function mergeStreamBody(raw: string): MergeResult | null {
    if (!raw || !raw.trim()) return null

    const sseEvents = parseSSE(raw)
    if (sseEvents.length > 0) {
        return mergeSseEvents(sseEvents)
    }

    const ndjsonChunks = parseNDJSON(raw)
    if (ndjsonChunks.length > 0) {
        return mergeNdjsonChunks(ndjsonChunks)
    }

    return null
}

function parseSSE(raw: string): SseEvent[] {
    const events: SseEvent[] = []
    const normalized = raw.replace(/\r\n/g, '\n')
    const blocks = normalized.split('\n\n')

    for (const block of blocks) {
        const lines = block.split('\n')
        let event = 'message'
        const dataLines: string[] = []

        for (const line of lines) {
            if (!line || line.startsWith(':')) continue
            if (line.startsWith('event:')) {
                const value = line.slice(6).trim()
                event = value || 'message'
                continue
            }
            if (line.startsWith('data:')) {
                dataLines.push(line.slice(5).trimStart())
            }
        }

        if (dataLines.length === 0) continue

        const payload = dataLines.join('\n').trim()
        if (!payload || payload === '[DONE]') continue

        try {
            const parsed = JSON.parse(payload)
            if (isRecord(parsed)) {
                events.push({ event, data: parsed })
            }
        } catch {
            // Ignore non-JSON SSE messages in merged view.
        }
    }

    return events
}

function parseNDJSON(raw: string): Record<string, unknown>[] {
    const lines = raw.split('\n').filter((line) => line.trim())
    if (lines.length < 2) return []

    const chunks: Record<string, unknown>[] = []
    let parsedCount = 0

    for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed) continue
        try {
            const parsed = JSON.parse(trimmed)
            parsedCount += 1
            if (isRecord(parsed)) {
                chunks.push(parsed)
            }
        } catch {
            // If most lines are not JSON, this is not NDJSON.
        }
    }

    if (parsedCount < lines.length * 0.5) return []

    return chunks
}

function mergeSseEvents(events: SseEvent[]): MergeResult {
    const protocol = detectSseProtocol(events)

    switch (protocol) {
        case 'claude-messages':
            return {
                merged: mergeClaudeMessages(events),
                chunks: events.length,
                format: 'sse',
                protocol,
            }
        case 'openai-responses':
            return {
                merged: mergeOpenAIResponsesEvents(events),
                chunks: events.length,
                format: 'sse',
                protocol,
            }
        case 'openai-chat':
            return {
                merged: mergeOpenAIChatChunks(events.map(({ data }) => data)),
                chunks: events.length,
                format: 'sse',
                protocol,
            }
        case 'ollama':
            return {
                merged: mergeOllamaChunks(events.map(({ data }) => data)),
                chunks: events.length,
                format: 'sse',
                protocol,
            }
        case 'gemini':
            return {
                merged: mergeGeminiChunks(events.map(({ data }) => data)),
                chunks: events.length,
                format: 'sse',
                protocol,
            }
        default:
            return {
                merged: events.map(toGenericSseEntry),
                chunks: events.length,
                format: 'sse',
                protocol: 'generic',
            }
    }
}

function mergeNdjsonChunks(chunks: Record<string, unknown>[]): MergeResult {
    const protocol = detectNdjsonProtocol(chunks)

    switch (protocol) {
        case 'claude-messages':
            return {
                merged: mergeClaudeMessages(chunks.map((data) => ({ event: asString(data.type) ?? 'message', data }))),
                chunks: chunks.length,
                format: 'ndjson',
                protocol,
            }
        case 'openai-responses':
            return {
                merged: mergeOpenAIResponsesEvents(chunks.map((data) => ({ event: asString(data.type) ?? 'message', data }))),
                chunks: chunks.length,
                format: 'ndjson',
                protocol,
            }
        case 'openai-chat':
            return {
                merged: mergeOpenAIChatChunks(chunks),
                chunks: chunks.length,
                format: 'ndjson',
                protocol,
            }
        case 'ollama':
            return {
                merged: mergeOllamaChunks(chunks),
                chunks: chunks.length,
                format: 'ndjson',
                protocol,
            }
        case 'gemini':
            return {
                merged: mergeGeminiChunks(chunks),
                chunks: chunks.length,
                format: 'ndjson',
                protocol,
            }
        default:
            return {
                merged: chunks,
                chunks: chunks.length,
                format: 'ndjson',
                protocol: 'generic',
            }
    }
}

function detectSseProtocol(events: SseEvent[]): StreamProtocol {
    if (events.some(isClaudeEvent)) return 'claude-messages'
    if (events.some((item) => isOpenAIResponsesType(item.event) || isOpenAIResponsesType(asString(item.data.type)))) {
        return 'openai-responses'
    }

    const chunks = events.map(({ data }) => data)
    if (isOpenAIChatChunks(chunks)) return 'openai-chat'
    if (isOllamaChunks(chunks)) return 'ollama'
    if (isGeminiChunks(chunks)) return 'gemini'
    return 'generic'
}

function detectNdjsonProtocol(chunks: Record<string, unknown>[]): StreamProtocol {
    if (chunks.some(isClaudeChunk)) return 'claude-messages'
    if (chunks.some((chunk) => isOpenAIResponsesType(asString(chunk.type)))) return 'openai-responses'
    if (isOpenAIChatChunks(chunks)) return 'openai-chat'
    if (isOllamaChunks(chunks)) return 'ollama'
    if (isGeminiChunks(chunks)) return 'gemini'
    return 'generic'
}

function isClaudeEvent(item: SseEvent): boolean {
    return CLAUDE_EVENT_NAMES.has(item.event) || CLAUDE_DATA_TYPES.has(asString(item.data.type) ?? '')
}

function isClaudeChunk(chunk: Record<string, unknown>): boolean {
    return CLAUDE_DATA_TYPES.has(asString(chunk.type) ?? '')
}

function isOpenAIResponsesType(value: string | undefined): boolean {
    return typeof value === 'string' && value.startsWith('response.')
}

function isOpenAIChatChunks(chunks: Record<string, unknown>[]): boolean {
    return chunks.some((chunk) => {
        if (asString(chunk.object) === 'chat.completion.chunk') return true
        const choices = getArray(chunk, 'choices')
        if (!choices || choices.length === 0) return false
        const firstChoice = choices[0]
        return isRecord(firstChoice) && isRecord(firstChoice.delta)
    })
}

function isOllamaChunks(chunks: Record<string, unknown>[]): boolean {
    return chunks.some((chunk) => {
        const message = getRecord(chunk, 'message')
        return Boolean(message && 'content' in message && 'done' in chunk)
    })
}

function isGeminiChunks(chunks: Record<string, unknown>[]): boolean {
    return chunks.some((chunk) => {
        const candidates = getArray(chunk, 'candidates')
        if (!candidates || candidates.length === 0) return false

        const firstCandidate = candidates.find(isRecord)
        if (!firstCandidate) {
            return Boolean(chunk.usageMetadata || chunk.responseId || chunk.modelVersion)
        }

        const content = getRecord(firstCandidate, 'content')
        const parts = content ? getArray(content, 'parts') : undefined

        return Boolean(
            parts ||
            firstCandidate.finishReason !== undefined ||
            chunk.usageMetadata ||
            chunk.responseId ||
            chunk.modelVersion
        )
    })
}

function mergeOpenAIChatChunks(chunks: Record<string, unknown>[]): Record<string, unknown> {
    const base = { ...chunks[chunks.length - 1] }
    const choiceMap = new Map<number, OpenAIChoiceAccumulator>()

    for (const chunk of chunks) {
        const choices = getArray(chunk, 'choices')
        if (!choices) continue

        for (const choiceValue of choices) {
            if (!isRecord(choiceValue)) continue
            const index = asNumber(choiceValue.index) ?? 0
            if (!choiceMap.has(index)) {
                choiceMap.set(index, {
                    role: '',
                    content: '',
                    reasoning: '',
                    reasoningContent: '',
                    reasoningDetails: [],
                    images: [],
                    toolCalls: [],
                    finishReason: null,
                })
            }

            const acc = choiceMap.get(index)
            if (!acc) continue

            const delta = getRecord(choiceValue, 'delta')
            if (delta) {
                const role = asString(delta.role)
                if (role) acc.role = role

                acc.content += readTextDelta(delta.content)
                const reasoning = asString(delta.reasoning)
                if (reasoning) acc.reasoning += reasoning
                const reasoningContent = asString(delta.reasoning_content)
                if (reasoningContent) acc.reasoningContent += reasoningContent
                mergeArrayValues(acc.reasoningDetails, getArray(delta, 'reasoning_details'))
                mergeArrayValues(acc.images, getArray(delta, 'images'))
                mergeToolCalls(acc, getArray(delta, 'tool_calls'))
            }

            const finishReason = asString(choiceValue.finish_reason)
            if (finishReason) acc.finishReason = finishReason
        }
    }

    const mergedChoices = [...choiceMap.entries()]
        .sort((a, b) => a[0] - b[0])
        .map(([index, acc]) => {
            const message: Record<string, unknown> = {
                role: acc.role || 'assistant',
                content: acc.content,
            }
            if (acc.reasoning) {
                message.reasoning = acc.reasoning
            }
            if (acc.reasoningContent) {
                message.reasoning_content = acc.reasoningContent
            }
            if (acc.reasoningDetails.length > 0) {
                message.reasoning_details = acc.reasoningDetails
            }
            if (acc.images.length > 0) {
                message.images = acc.images
            }
            const toolCalls = acc.toolCalls.filter(isPresent).map((toolCall) => ({
                id: toolCall.id,
                type: toolCall.type,
                function: {
                    name: toolCall.function.name,
                    arguments: toolCall.function.arguments,
                },
            }))
            if (toolCalls.length > 0) {
                message.tool_calls = toolCalls
            }
            return {
                index,
                message,
                finish_reason: acc.finishReason,
            }
        })

    const result: Record<string, unknown> = { ...base }
    if (base.object !== undefined) result.object = 'chat.completion'
    result.choices = mergedChoices

    return result
}

function mergeArrayValues(target: unknown[], values: unknown[] | undefined): void {
    if (!values || values.length === 0) return
    target.push(...values)
}

function mergeToolCalls(acc: OpenAIChoiceAccumulator, toolCalls: unknown[] | undefined): void {
    if (!toolCalls) return

    for (const toolCallValue of toolCalls) {
        if (!isRecord(toolCallValue)) continue
        const index = asNumber(toolCallValue.index) ?? 0
        if (!acc.toolCalls[index]) {
            acc.toolCalls[index] = {
                id: '',
                type: 'function',
                function: {
                    name: '',
                    arguments: '',
                },
            }
        }

        const toolCall = acc.toolCalls[index]
        if (!toolCall) continue

        const id = asString(toolCallValue.id)
        if (id) toolCall.id = id

        const type = asString(toolCallValue.type)
        if (type) toolCall.type = type

        const fn = getRecord(toolCallValue, 'function')
        if (!fn) continue

        const name = asString(fn.name)
        if (name) toolCall.function.name += name

        const args = asString(fn.arguments)
        if (args) toolCall.function.arguments += args
    }
}

function mergeOpenAIResponsesEvents(events: SseEvent[]): Record<string, unknown> {
    const response: Record<string, unknown> = { object: 'response' }
    const output: Array<Record<string, unknown> | undefined> = []

    for (const { data } of events) {
        const type = asString(data.type)
        if (!type) continue

        switch (type) {
            case 'response.created':
            case 'response.in_progress':
            case 'response.completed':
            case 'response.failed': {
                mergeResponseEnvelope(response, getRecord(data, 'response'), output)
                const error = getRecord(data, 'error')
                if (error) response.error = error
                break
            }
            case 'response.output_item.added':
            case 'response.output_item.done': {
                const item = getRecord(data, 'item')
                if (!item) break
                const outputIndex = resolveOutputIndex(output, data, output.length)
                mergeOutputItem(ensureOutputItem(output, outputIndex, item), item)
                break
            }
            case 'response.content_part.added':
            case 'response.content_part.done': {
                const part = getRecord(data, 'part')
                if (!part) break
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'message', role: 'assistant' })
                const parts = getContentParts(item)
                const contentIndex = resolveContentIndex(data, parts.length)
                mergePart(ensureContentPart(item, contentIndex, part), part)
                break
            }
            case 'response.output_text.delta': {
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'message', role: 'assistant' })
                const contentIndex = resolveContentIndex(data, 0)
                const part = ensureContentPart(item, contentIndex, { type: 'output_text', text: '' })
                part.type = asString(part.type) ?? 'output_text'
                part.text = `${asString(part.text) ?? ''}${asString(data.delta) ?? ''}`
                break
            }
            case 'response.output_text.done': {
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'message', role: 'assistant' })
                const contentIndex = resolveContentIndex(data, 0)
                const part = ensureContentPart(item, contentIndex, { type: 'output_text', text: '' })
                const text = asString(data.text)
                if (text !== undefined) part.text = text
                break
            }
            case 'response.refusal.delta': {
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'message', role: 'assistant' })
                const contentIndex = resolveContentIndex(data, 0)
                const part = ensureContentPart(item, contentIndex, { type: 'refusal', refusal: '' })
                part.type = 'refusal'
                part.refusal = `${asString(part.refusal) ?? ''}${asString(data.delta) ?? ''}`
                break
            }
            case 'response.refusal.done': {
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'message', role: 'assistant' })
                const contentIndex = resolveContentIndex(data, 0)
                const part = ensureContentPart(item, contentIndex, { type: 'refusal', refusal: '' })
                const refusal = asString(data.refusal)
                if (refusal !== undefined) part.refusal = refusal
                break
            }
            case 'response.function_call_arguments.delta': {
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'function_call', arguments: '' })
                item.type = asString(item.type) ?? 'function_call'
                const itemId = asString(data.item_id)
                if (itemId) item.id = itemId
                const callId = asString(data.call_id)
                if (callId) item.call_id = callId
                const name = asString(data.name)
                if (name) item.name = name
                item.arguments = `${asString(item.arguments) ?? ''}${asString(data.delta) ?? ''}`
                break
            }
            case 'response.function_call_arguments.done': {
                const outputIndex = resolveOutputIndex(output, data, 0)
                const item = ensureOutputItem(output, outputIndex, { type: 'function_call', arguments: '' })
                const itemId = asString(data.item_id)
                if (itemId) item.id = itemId
                const callId = asString(data.call_id)
                if (callId) item.call_id = callId
                const name = asString(data.name)
                if (name) item.name = name
                const argumentsText = asString(data.arguments)
                if (argumentsText !== undefined) item.arguments = argumentsText
                break
            }
        }
    }

    response.output = output.filter(isPresent).map(normalizeOutputItem)
    return response
}

function mergeClaudeMessages(events: SseEvent[]): Record<string, unknown> {
    const message: Record<string, unknown> = {
        type: 'message',
        role: 'assistant',
    }
    const contentBlocks: Array<Record<string, unknown> | undefined> = []

    for (const { event, data } of events) {
        const type = asString(data.type) ?? event

        switch (type) {
            case 'message_start': {
                const initialMessage = getRecord(data, 'message')
                if (initialMessage) {
                    mergeTopLevelFields(message, initialMessage, ['content'])
                    const usage = getRecord(initialMessage, 'usage')
                    if (usage) mergeUsage(message, usage)
                }
                break
            }
            case 'content_block_start': {
                const index = asNumber(data.index) ?? contentBlocks.length
                const block = getRecord(data, 'content_block') ?? {}
                mergePart(ensureClaudeContentBlock(contentBlocks, index, block), block)
                break
            }
            case 'content_block_delta': {
                const index = asNumber(data.index) ?? 0
                const block = ensureClaudeContentBlock(contentBlocks, index, {})
                const delta = getRecord(data, 'delta')
                if (!delta) break

                const deltaType = asString(delta.type)
                switch (deltaType) {
                    case 'text_delta': {
                        block.type = asString(block.type) ?? 'text'
                        block.text = `${asString(block.text) ?? ''}${asString(delta.text) ?? ''}`
                        break
                    }
                    case 'thinking_delta': {
                        block.type = asString(block.type) ?? 'thinking'
                        const thinkingText = asString(delta.thinking) ?? asString(delta.text) ?? ''
                        block.thinking = `${asString(block.thinking) ?? ''}${thinkingText}`
                        break
                    }
                    case 'input_json_delta': {
                        block.type = asString(block.type) ?? 'tool_use'
                        block._partial_json = `${asString(block._partial_json) ?? ''}${asString(delta.partial_json) ?? ''}`
                        break
                    }
                    case 'signature_delta': {
                        const signature = asString(delta.signature)
                        if (signature) block.signature = signature
                        break
                    }
                    default:
                        mergePart(block, delta)
                        break
                }
                break
            }
            case 'message_delta': {
                const delta = getRecord(data, 'delta')
                if (delta) mergeTopLevelFields(message, delta)
                const usage = getRecord(data, 'usage')
                if (usage) mergeUsage(message, usage)
                break
            }
            case 'error': {
                const error = getRecord(data, 'error')
                if (error) message.error = error
                break
            }
        }
    }

    message.content = contentBlocks.filter(isPresent).map(finalizeClaudeContentBlock)
    return message
}

function mergeOllamaChunks(chunks: Record<string, unknown>[]): Record<string, unknown> {
    const lastChunk = chunks[chunks.length - 1]
    let content = ''
    let role = ''

    for (const chunk of chunks) {
        const message = getRecord(chunk, 'message')
        if (!message) continue

        const chunkRole = asString(message.role)
        if (chunkRole) role = chunkRole

        const chunkContent = asString(message.content)
        if (chunkContent) content += chunkContent
    }

    return {
        ...lastChunk,
        message: {
            role: role || 'assistant',
            content,
        },
    }
}

function mergeGeminiChunks(chunks: Record<string, unknown>[]): Record<string, unknown> {
    const result: Record<string, unknown> = {}
    const candidateMap = new Map<number, GeminiCandidateAccumulator>()

    for (const chunk of chunks) {
        mergeGeminiEnvelope(result, chunk)

        const candidates = getArray(chunk, 'candidates')
        if (!candidates) continue

        for (const candidateValue of candidates) {
            if (!isRecord(candidateValue)) continue

            const index = asNumber(candidateValue.index) ?? 0
            if (!candidateMap.has(index)) {
                candidateMap.set(index, {
                    candidate: {},
                    content: {},
                    parts: [],
                })
            }

            const acc = candidateMap.get(index)
            if (!acc) continue

            mergeTopLevelFields(acc.candidate, candidateValue, ['content'])

            const content = getRecord(candidateValue, 'content')
            if (!content) continue

            mergeTopLevelFields(acc.content, content, ['parts'])
            appendGeminiParts(acc.parts, getArray(content, 'parts'))
        }
    }

    const mergedCandidates = [...candidateMap.entries()]
        .sort((a, b) => a[0] - b[0])
        .map(([index, acc]) => {
            const candidate: Record<string, unknown> = { ...acc.candidate, index }
            if (Object.keys(acc.content).length > 0 || acc.parts.length > 0) {
                candidate.content = {
                    ...acc.content,
                    parts: acc.parts.map((part) => ({ ...part })),
                }
            }
            return candidate
        })

    if (mergedCandidates.length > 0) {
        result.candidates = mergedCandidates
    }

    return result
}

function mergeGeminiEnvelope(target: Record<string, unknown>, chunk: Record<string, unknown>): void {
    for (const [key, value] of Object.entries(chunk)) {
        if (value === undefined || key === 'candidates') continue
        if (key === 'usageMetadata' && isRecord(value)) {
            const merged = isRecord(target.usageMetadata) ? { ...target.usageMetadata } : {}
            Object.assign(merged, value)
            target.usageMetadata = merged
            continue
        }
        target[key] = value
    }
}

function appendGeminiParts(target: Array<Record<string, unknown>>, parts: unknown[] | undefined): void {
    if (!parts) return

    for (const partValue of parts) {
        if (!isRecord(partValue)) continue

        const text = asString(partValue.text)
        if (text === undefined) {
            target.push({ ...partValue })
            continue
        }

        const lastPart = target[target.length - 1]
        if (lastPart && asString(lastPart.text) !== undefined) {
            lastPart.text = `${asString(lastPart.text) ?? ''}${text}`
            for (const [key, value] of Object.entries(partValue)) {
                if (key === 'text' || value === undefined) continue
                lastPart[key] = value
            }
            continue
        }

        target.push({ ...partValue })
    }
}

function mergeResponseEnvelope(
    target: Record<string, unknown>,
    source: Record<string, unknown> | undefined,
    output: Array<Record<string, unknown> | undefined>
): void {
    if (!source) return

    for (const [key, value] of Object.entries(source)) {
        if (value === undefined || key === 'output') continue
        if (key === 'usage' && isRecord(value)) {
            mergeUsage(target, value)
            continue
        }
        target[key] = value
    }

    const sourceOutput = getArray(source, 'output')
    if (!sourceOutput) return

    for (let index = 0; index < sourceOutput.length; index += 1) {
        const item = sourceOutput[index]
        if (!isRecord(item)) continue
        mergeOutputItem(ensureOutputItem(output, index, item), item)
    }
}

function mergeOutputItem(target: Record<string, unknown>, source: Record<string, unknown>): Record<string, unknown> {
    for (const [key, value] of Object.entries(source)) {
        if (value === undefined) continue
        if (key === 'content' && Array.isArray(value)) {
            const targetParts = getContentParts(target)
            for (let index = 0; index < value.length; index += 1) {
                const part = value[index]
                if (!isRecord(part)) continue
                mergePart(ensureContentPart(target, index, part), part)
            }
            target.content = targetParts
            continue
        }
        target[key] = value
    }
    return target
}

function mergePart(target: Record<string, unknown>, source: Record<string, unknown>): Record<string, unknown> {
    for (const [key, value] of Object.entries(source)) {
        if (value !== undefined) {
            target[key] = value
        }
    }
    return target
}

function mergeTopLevelFields(
    target: Record<string, unknown>,
    source: Record<string, unknown>,
    exclude: string[] = []
): void {
    for (const [key, value] of Object.entries(source)) {
        if (exclude.includes(key) || value === undefined) continue
        if (key === 'usage' && isRecord(value)) {
            mergeUsage(target, value)
            continue
        }
        target[key] = value
    }
}

function mergeUsage(target: Record<string, unknown>, usage: Record<string, unknown>): void {
    const merged = isRecord(target.usage) ? { ...target.usage } : {}
    Object.assign(merged, usage)
    target.usage = merged
}

function ensureOutputItem(
    output: Array<Record<string, unknown> | undefined>,
    index: number,
    seed: Record<string, unknown>
): Record<string, unknown> {
    const existing = output[index]
    if (existing) {
        return mergeOutputItem(existing, seed)
    }

    const created = { ...seed }
    output[index] = created
    return created
}

function ensureContentPart(
    item: Record<string, unknown>,
    index: number,
    seed: Record<string, unknown>
): Record<string, unknown> {
    const content = getContentParts(item)
    const existing = content[index]
    if (existing) {
        return mergePart(existing, seed)
    }

    const created = { ...seed }
    content[index] = created
    item.content = content
    return created
}

function getContentParts(item: Record<string, unknown>): Array<Record<string, unknown> | undefined> {
    if (!Array.isArray(item.content)) {
        const parts: Array<Record<string, unknown> | undefined> = []
        item.content = parts
        return parts
    }

    const normalized = item.content.map((part) => (isRecord(part) ? part : undefined))
    item.content = normalized
    return normalized
}

function ensureClaudeContentBlock(
    blocks: Array<Record<string, unknown> | undefined>,
    index: number,
    seed: Record<string, unknown>
): Record<string, unknown> {
    const existing = blocks[index]
    if (existing) {
        return mergePart(existing, seed)
    }

    const created = { ...seed }
    blocks[index] = created
    return created
}

function finalizeClaudeContentBlock(block: Record<string, unknown>): Record<string, unknown> {
    const finalized = { ...block }
    const partialJson = asString(finalized._partial_json)
    delete finalized._partial_json

    if (partialJson !== undefined) {
        const parsed = tryParseJson(partialJson)
        if (parsed !== undefined) {
            finalized.input = parsed
        } else if (partialJson) {
            finalized.input_json = partialJson
        }
    }

    return finalized
}

function normalizeOutputItem(item: Record<string, unknown>): Record<string, unknown> {
    const normalized = { ...item }

    if (Array.isArray(item.content)) {
        normalized.content = item.content
            .filter(isPresent)
            .map((part) => ({ ...part }))
    }

    return normalized
}

function resolveOutputIndex(
    output: Array<Record<string, unknown> | undefined>,
    data: Record<string, unknown>,
    fallback: number
): number {
    const directIndex = asNumber(data.output_index)
    if (directIndex !== undefined) return directIndex

    const itemId = asString(data.item_id)
    if (itemId) {
        const foundIndex = findOutputIndexById(output, itemId)
        if (foundIndex !== undefined) return foundIndex
    }

    return fallback
}

function resolveContentIndex(data: Record<string, unknown>, fallback: number): number {
    return asNumber(data.content_index) ?? fallback
}

function findOutputIndexById(
    output: Array<Record<string, unknown> | undefined>,
    itemId: string
): number | undefined {
    for (let index = 0; index < output.length; index += 1) {
        const item = output[index]
        if (item && asString(item.id) === itemId) {
            return index
        }
    }

    return undefined
}

function readTextDelta(value: unknown): string {
    if (typeof value === 'string') return value
    if (!Array.isArray(value)) return ''

    let text = ''
    for (const part of value) {
        if (!isRecord(part)) continue
        const directText = asString(part.text)
        if (directText) {
            text += directText
            continue
        }

        const nestedText = getRecord(part, 'text')
        const nestedValue = nestedText ? asString(nestedText.value) : undefined
        if (nestedValue) text += nestedValue
    }

    return text
}

function toGenericSseEntry(item: SseEvent): Record<string, unknown> {
    if (item.event === 'message') return item.data
    return {
        event: item.event,
        data: item.data,
    }
}

function tryParseJson(value: string): unknown {
    try {
        return JSON.parse(value)
    } catch {
        return undefined
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isPresent<T>(value: T | undefined): value is T {
    return value !== undefined
}

function getArray(source: Record<string, unknown>, key: string): unknown[] | undefined {
    const value = source[key]
    return Array.isArray(value) ? value : undefined
}

function getRecord(source: Record<string, unknown>, key: string): Record<string, unknown> | undefined {
    const value = source[key]
    return isRecord(value) ? value : undefined
}

function asString(value: unknown): string | undefined {
    return typeof value === 'string' ? value : undefined
}

function asNumber(value: unknown): number | undefined {
    return typeof value === 'number' ? value : undefined
}
