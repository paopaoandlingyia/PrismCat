export function containsSearchMatch(data: unknown, term: string): boolean {
    if (!term) return false
    const lower = term.toLowerCase()
    function check(value: unknown, key?: string): boolean {
        if (key !== undefined && key.toLowerCase().includes(lower)) return true
        if (value === null) return 'null'.includes(lower)
        if (value === undefined) return false
        if (typeof value === 'string') return value.toLowerCase().includes(lower)
        if (typeof value === 'number') return String(value).includes(lower)
        if (typeof value === 'boolean') return String(value).includes(lower)
        if (Array.isArray(value)) return value.some((v) => check(v))
        if (typeof value === 'object') return Object.entries(value as Record<string, unknown>).some(([k, v]) => check(v, k))
        return false
    }
    return check(data)
}

export function countJsonSearchMatches(data: unknown, term: string): number {
    if (!term) return 0
    const lower = term.toLowerCase()
    let count = 0
    function countIn(text: string) {
        const t = text.toLowerCase()
        let idx = 0
        while ((idx = t.indexOf(lower, idx)) !== -1) {
            count++
            idx += lower.length
        }
    }
    function traverse(value: unknown, key?: string, isArrayItem?: boolean) {
        if (key !== undefined && !isArrayItem) countIn(key)
        if (value === null) {
            countIn('null')
            return
        }
        if (value === undefined) return
        if (typeof value === 'string') {
            countIn(value)
            return
        }
        if (typeof value === 'number') {
            countIn(String(value))
            return
        }
        if (typeof value === 'boolean') {
            countIn(String(value))
            return
        }
        if (Array.isArray(value)) {
            value.forEach((v, i) => traverse(v, String(i), true))
            return
        }
        if (typeof value === 'object') {
            for (const [k, v] of Object.entries(value as Record<string, unknown>)) traverse(v, k, false)
        }
    }
    traverse(data)
    return count
}
