import type { LoggingPathRule } from '@/lib/api'

export function normalizeLoggingPathRule(rule: LoggingPathRule): LoggingPathRule | null {
    const matcher = rule.matcher === 'regex' ? 'regex' : 'ant'
    let pattern = rule.pattern.trim()
    if (!pattern) return null
    if (matcher === 'ant' && !pattern.startsWith('/')) pattern = `/${pattern}`
    return { matcher, pattern }
}

export function mergeLoggingPathRules(existing: LoggingPathRule[], incoming: LoggingPathRule[]) {
    const rules: LoggingPathRule[] = []
    const seen = new Set<string>()
    let skipped = 0
    let added = 0

    for (const source of existing) {
        const rule = normalizeLoggingPathRule(source)
        if (!rule) continue
        const key = `${rule.matcher}\u0000${rule.pattern}`
        if (seen.has(key)) continue
        seen.add(key)
        rules.push(rule)
    }

    for (const source of incoming) {
        const rule = normalizeLoggingPathRule(source)
        if (!rule) continue
        const key = `${rule.matcher}\u0000${rule.pattern}`
        if (seen.has(key)) {
            skipped += 1
            continue
        }
        seen.add(key)
        rules.push(rule)
        added += 1
    }

    return {
        rules,
        added,
        skipped,
    }
}
