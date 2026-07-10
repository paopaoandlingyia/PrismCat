import { getValueSearchFragments } from './bodyContent'

export const MAX_RENDERED_SEARCH_MATCHES = 500

export interface TextSearchRange {
    start: number
    end: number
}

export interface TextSearchResult {
    ranges: TextSearchRange[]
    truncated: boolean
}

export interface JsonSearchPlan {
    term: string
    matchCount: number
    truncated: boolean
    matchesBySlot: ReadonlyMap<string, number>
    expandedPaths: ReadonlySet<string>
    visiblePaths: ReadonlySet<string>
}

interface FoldedText {
    text: string
    starts: number[]
    ends: number[]
}

function foldTextWithOriginalIndexes(value: string): FoldedText {
    let text = ''
    const starts: number[] = []
    const ends: number[] = []

    for (let index = 0; index < value.length;) {
        const codePoint = value.codePointAt(index)
        if (codePoint === undefined) break

        const original = String.fromCodePoint(codePoint)
        const end = index + original.length
        const folded = original.toLowerCase()
        text += folded
        for (let foldedIndex = 0; foldedIndex < folded.length; foldedIndex++) {
            starts.push(index)
            ends.push(end)
        }
        index = end
    }

    return { text, starts, ends }
}

export function collectTextSearchMatches(
    text: string,
    term: string,
    limit = MAX_RENDERED_SEARCH_MATCHES,
): TextSearchResult {
    if (!text || !term || limit < 0) return { ranges: [], truncated: false }

    const lowerText = text.toLowerCase()
    const lowerTerm = term.toLowerCase()
    if (!lowerTerm) return { ranges: [], truncated: false }

    const needsIndexMap = lowerText.length !== text.length || lowerTerm.length !== term.length
    const folded = needsIndexMap ? foldTextWithOriginalIndexes(text) : null
    const searchableText = folded?.text ?? lowerText
    const ranges: TextSearchRange[] = []
    let searchIndex = 0

    while (searchIndex <= searchableText.length - lowerTerm.length) {
        const matchIndex = searchableText.indexOf(lowerTerm, searchIndex)
        if (matchIndex === -1) break
        if (ranges.length >= limit) return { ranges, truncated: true }

        if (folded) {
            const lastFoldedIndex = matchIndex + lowerTerm.length - 1
            const start = folded.starts[matchIndex]
            const end = folded.ends[lastFoldedIndex]
            if (start !== undefined && end !== undefined) {
                ranges.push({ start, end })
            }
        } else {
            ranges.push({ start: matchIndex, end: matchIndex + term.length })
        }

        searchIndex = matchIndex + lowerTerm.length
    }

    return { ranges, truncated: false }
}

export function childJsonSearchPath(parentPath: string, key: string): string {
    return `${parentPath}/${key.replace(/~/g, '~0').replace(/\//g, '~1')}`
}

export function jsonKeySearchSlot(nodePath: string): string {
    return `${nodePath}#key`
}

export function jsonValueSearchSlot(nodePath: string, fragmentIndex = 0): string {
    return `${nodePath}#value:${fragmentIndex}`
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function addPathAncestors(paths: Set<string>, nodePath: string) {
    let current = nodePath
    while (true) {
        paths.add(current)
        if (!current) return
        const separatorIndex = current.lastIndexOf('/')
        current = separatorIndex > 0 ? current.slice(0, separatorIndex) : ''
    }
}

export function createJsonSearchPlan(
    data: unknown,
    term: string,
    limit = MAX_RENDERED_SEARCH_MATCHES,
): JsonSearchPlan {
    const matchesBySlot = new Map<string, number>()
    const expandedPaths = new Set<string>()
    const visiblePaths = new Set<string>()
    let matchCount = 0
    let truncated = false

    const addText = (slot: string, text: string, matchedNodePath: string, containingNodePath: string): boolean => {
        const result = collectTextSearchMatches(text, term, Math.max(0, limit - matchCount))
        if (result.ranges.length) {
            matchesBySlot.set(slot, result.ranges.length)
            matchCount += result.ranges.length
            addPathAncestors(expandedPaths, containingNodePath)
            addPathAncestors(visiblePaths, matchedNodePath)
        }
        if (result.truncated) {
            truncated = true
            return false
        }
        return true
    }

    const traverse = (value: unknown, nodePath: string, containingNodePath: string, isRoot: boolean): boolean => {
        if (Array.isArray(value)) {
            for (let index = 0; index < value.length; index++) {
                if (!traverse(value[index], childJsonSearchPath(nodePath, String(index)), nodePath, false)) return false
            }
            return true
        }

        if (isRecord(value)) {
            for (const [key, child] of Object.entries(value)) {
                const childPath = childJsonSearchPath(nodePath, key)
                if (!addText(jsonKeySearchSlot(childPath), key, childPath, nodePath)) return false
                if (!traverse(child, childPath, nodePath, false)) return false
            }
            return true
        }

        const fragments = getValueSearchFragments(value, isRoot)
        for (let index = 0; index < fragments.length; index++) {
            if (!addText(jsonValueSearchSlot(nodePath, index), fragments[index], nodePath, containingNodePath)) return false
        }
        return true
    }

    if (term) traverse(data, '', '', true)
    return { term, matchCount, truncated, matchesBySlot, expandedPaths, visiblePaths }
}
