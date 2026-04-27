export type JsonValue =
    | null
    | boolean
    | number
    | string
    | JsonValue[]
    | { [key: string]: JsonValue }

export type JsonDiffType = 'added' | 'removed' | 'changed'

export interface JsonDiffChange {
    type: JsonDiffType
    path: string
    before?: JsonValue
    after?: JsonValue
}

export interface JsonParseResult {
    ok: boolean
    value?: JsonValue
    error?: string
}

export function parseJsonText(text: string): JsonParseResult {
    try {
        return { ok: true, value: JSON.parse(text) as JsonValue }
    } catch (err) {
        return {
            ok: false,
            error: err instanceof Error ? err.message : 'Invalid JSON',
        }
    }
}

export function buildJsonDiff(before: JsonValue, after: JsonValue): JsonDiffChange[] {
    const changes: JsonDiffChange[] = []
    walkDiff(before, after, '', changes)
    return changes
}

export function formatJsonPointer(path: string): string {
    return path || '/'
}

function walkDiff(before: JsonValue, after: JsonValue, path: string, changes: JsonDiffChange[]) {
    if (jsonEquals(before, after)) return

    if (isPlainObject(before) && isPlainObject(after)) {
        const keys = new Set([...Object.keys(before), ...Object.keys(after)])
        for (const key of Array.from(keys).sort()) {
            const childPath = joinPointer(path, key)
            if (!(key in before)) {
                changes.push({ type: 'added', path: childPath, after: after[key] })
                continue
            }
            if (!(key in after)) {
                changes.push({ type: 'removed', path: childPath, before: before[key] })
                continue
            }
            walkDiff(before[key], after[key], childPath, changes)
        }
        return
    }

    if (Array.isArray(before) && Array.isArray(after)) {
        const maxLength = Math.max(before.length, after.length)
        for (let index = 0; index < maxLength; index++) {
            const childPath = joinPointer(path, String(index))
            if (index >= before.length) {
                changes.push({ type: 'added', path: childPath, after: after[index] })
                continue
            }
            if (index >= after.length) {
                changes.push({ type: 'removed', path: childPath, before: before[index] })
                continue
            }
            walkDiff(before[index], after[index], childPath, changes)
        }
        return
    }

    changes.push({ type: 'changed', path, before, after })
}

function joinPointer(base: string, token: string): string {
    return `${base}/${escapePointerToken(token)}`
}

function escapePointerToken(token: string): string {
    return token.replace(/~/g, '~0').replace(/\//g, '~1')
}

function isPlainObject(value: JsonValue): value is { [key: string]: JsonValue } {
    return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function jsonEquals(left: JsonValue, right: JsonValue): boolean {
    if (Object.is(left, right)) return true
    if (Array.isArray(left) && Array.isArray(right)) {
        if (left.length !== right.length) return false
        return left.every((item, index) => jsonEquals(item, right[index]))
    }
    if (isPlainObject(left) && isPlainObject(right)) {
        const leftKeys = Object.keys(left)
        const rightKeys = Object.keys(right)
        if (leftKeys.length !== rightKeys.length) return false
        return leftKeys.every((key) => key in right && jsonEquals(left[key], right[key]))
    }
    return false
}
