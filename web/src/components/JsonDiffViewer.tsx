import { AlertTriangle } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from './ui/badge'
import { cn } from '@/lib/utils'
import {
    buildJsonDiff,
    formatJsonPointer,
    parseJsonText,
    type JsonDiffChange,
    type JsonDiffType,
    type JsonValue,
} from '@/lib/jsonDiff'

interface JsonDiffViewerProps {
    beforeText: string
    afterText: string
}

const typeClassNames: Record<JsonDiffType, string> = {
    added: 'border-success/30 bg-success/10 text-success',
    removed: 'border-danger/30 bg-danger/10 text-danger',
    changed: 'border-warning/30 bg-warning/10 text-warning',
}

export function JsonDiffViewer({ beforeText, afterText }: JsonDiffViewerProps) {
    const { t } = useTranslation()
    const diff = useMemo(() => {
        const before = parseJsonText(beforeText)
        const after = parseJsonText(afterText)
        if (!before.ok || !after.ok || before.value === undefined || after.value === undefined) {
            return {
                parsed: false as const,
                beforeError: before.error,
                afterError: after.error,
            }
        }

        return {
            parsed: true as const,
            changes: buildJsonDiff(before.value, after.value),
        }
    }, [beforeText, afterText])

    if (!diff.parsed) {
        return (
            <div className="space-y-3">
                <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs leading-relaxed text-warning">
                    <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    <span>{t('json_diff.json_required', 'Diff needs both sides to be valid JSON. Showing raw bodies instead.')}</span>
                </div>
                <RawSideBySide beforeText={beforeText} afterText={afterText} />
            </div>
        )
    }

    if (diff.changes.length === 0) {
        return (
            <div className="rounded-lg border border-dashed border-border/50 bg-background/60 px-4 py-8 text-center text-xs font-medium text-muted-foreground">
                {t('json_diff.no_changes', 'No JSON changes')}
            </div>
        )
    }

    const counts = countByType(diff.changes)

    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2 text-xs font-medium text-muted-foreground">
                <span>{t('json_diff.summary', '{{count}} changes', { count: diff.changes.length })}</span>
                <DiffCount type="added" count={counts.added} />
                <DiffCount type="removed" count={counts.removed} />
                <DiffCount type="changed" count={counts.changed} />
            </div>
            <div className="space-y-2">
                {diff.changes.map((change) => (
                    <DiffChangeRow key={`${change.type}:${change.path}`} change={change} />
                ))}
            </div>
        </div>
    )
}

function DiffCount({ type, count }: { type: JsonDiffType; count: number }) {
    const { t } = useTranslation()
    if (count === 0) return null

    return (
        <Badge variant="outline" className={cn('h-5 rounded-md px-1.5 text-xs font-medium', typeClassNames[type])}>
            {t(`json_diff.${type}`, type)} {count}
        </Badge>
    )
}

function DiffChangeRow({ change }: { change: JsonDiffChange }) {
    const { t } = useTranslation()

    return (
        <div className="overflow-hidden rounded-lg border border-border/60 bg-background shadow-sm">
            <div className="flex flex-wrap items-center gap-2 border-b border-border/50 bg-muted/25 px-3 py-2">
                <Badge variant="outline" className={cn('h-5 rounded-md px-1.5 text-xs font-medium', typeClassNames[change.type])}>
                    {t(`json_diff.${change.type}`, change.type)}
                </Badge>
                <code className="break-all text-xs font-medium text-foreground">
                    {formatJsonPointer(change.path)}
                </code>
            </div>
            <div className="grid gap-px bg-border/50 lg:grid-cols-2">
                <DiffValuePanel
                    title={t('json_diff.before', 'Before')}
                    value={change.before}
                    side="before"
                    state={change.type === 'added' ? 'missing' : 'removed'}
                />
                <DiffValuePanel
                    title={t('json_diff.after', 'After')}
                    value={change.after}
                    side="after"
                    state={change.type === 'removed' ? 'missing' : 'added'}
                />
            </div>
        </div>
    )
}

function DiffValuePanel({
    title,
    value,
    side,
    state,
}: {
    title: string
    value?: JsonValue
    side: 'before' | 'after'
    state: 'added' | 'removed' | 'missing'
}) {
    const { t } = useTranslation()
    const sign = side === 'before' ? '-' : '+'

    return (
        <div
            className={cn(
                'min-w-0 p-3',
                state === 'added' && 'bg-success/[0.07]',
                state === 'removed' && 'bg-danger/[0.07]',
                state === 'missing' && 'bg-muted/35'
            )}
        >
            <div
                className={cn(
                    'mb-2 text-xs font-medium',
                    state === 'added' && 'text-success',
                    state === 'removed' && 'text-danger',
                    state === 'missing' && 'text-muted-foreground'
                )}
            >
                {title}
            </div>
            {value === undefined ? (
                <div className="rounded-md border border-dashed border-border/60 bg-background/45 px-3 py-4 text-xs italic text-muted-foreground">
                    {t('json_diff.missing', 'Missing')}
                </div>
            ) : (
                <pre
                    className={cn(
                        'custom-scrollbar max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md border px-3 py-2 font-mono text-xs leading-relaxed select-text',
                        state === 'added' && 'border-success/20 bg-success/[0.06] text-success',
                        state === 'removed' && 'border-danger/20 bg-danger/[0.06] text-danger'
                    )}
                >
                    {prefixJsonLines(value, sign)}
                </pre>
            )}
        </div>
    )
}

function prefixJsonLines(value: JsonValue, prefix: '+' | '-'): string {
    return stringifyJsonValue(value)
        .split('\n')
        .map((line) => `${prefix} ${line}`)
        .join('\n')
}

function stringifyJsonValue(value: JsonValue): string {
    if (typeof value === 'string') return JSON.stringify(value)
    return JSON.stringify(value, null, 2)
}

function RawSideBySide({ beforeText, afterText }: JsonDiffViewerProps) {
    const { t } = useTranslation()
    return (
        <div className="grid gap-px overflow-hidden rounded-lg border border-border/60 bg-border/50 sm:grid-cols-2">
            <RawPanel title={t('json_diff.before', 'Before')} text={beforeText} />
            <RawPanel title={t('json_diff.after', 'After')} text={afterText} />
        </div>
    )
}

function RawPanel({ title, text }: { title: string; text: string }) {
    return (
        <div className="min-w-0 bg-muted/40 p-3">
            <div className="mb-2 text-xs font-medium text-muted-foreground">
                {title}
            </div>
            <pre className="custom-scrollbar max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs font-mono leading-relaxed text-foreground select-text">
                {text}
            </pre>
        </div>
    )
}

function countByType(changes: JsonDiffChange[]): Record<JsonDiffType, number> {
    return changes.reduce<Record<JsonDiffType, number>>((acc, change) => {
        acc[change.type] += 1
        return acc
    }, { added: 0, removed: 0, changed: 0 })
}
