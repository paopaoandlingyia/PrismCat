import { diffLines, type Change } from 'diff'
import { useMemo } from 'react'

import { cn } from '@/lib/utils'
import type { JsonValue } from '@/lib/jsonDiff'

interface TextDiffViewerProps {
    beforeText: string
    afterText: string
}

type DiffRow = {
    kind: 'context' | 'added' | 'removed'
    oldLine?: number
    newLine?: number
    text: string
}

export function TextDiffViewer({ beforeText, afterText }: TextDiffViewerProps) {
    const rows = useMemo(() => buildRows(prettyJsonText(beforeText), prettyJsonText(afterText)), [beforeText, afterText])

    return (
        <div className="overflow-hidden rounded-xl border border-border/70 bg-background shadow-sm">
            <div className="custom-scrollbar max-h-[72vh] overflow-auto">
                <div className="min-w-[760px] font-mono text-xs leading-5">
                    {rows.map((row, index) => (
                        <DiffLine key={`${index}:${row.kind}:${row.oldLine ?? ''}:${row.newLine ?? ''}`} row={row} />
                    ))}
                </div>
            </div>
        </div>
    )
}

function DiffLine({ row }: { row: DiffRow }) {
    return (
        <div
            className={cn(
                'grid grid-cols-[56px_56px_28px_minmax(0,1fr)] border-l-4',
                row.kind === 'context' && 'border-transparent hover:bg-muted/35',
                row.kind === 'added' && 'border-success bg-success/[0.10] text-success',
                row.kind === 'removed' && 'border-danger bg-danger/[0.10] text-danger'
            )}
        >
            <LineNumber value={row.oldLine} tone={row.kind} />
            <LineNumber value={row.newLine} tone={row.kind} />
            <div
                className={cn(
                    'select-none border-r border-border/40 px-2 text-center',
                    row.kind === 'added' && 'text-success',
                    row.kind === 'removed' && 'text-danger',
                    row.kind === 'context' && 'text-muted-foreground'
                )}
            >
                {row.kind === 'added' ? '+' : row.kind === 'removed' ? '-' : ''}
            </div>
            <pre className="min-w-0 whitespace-pre-wrap break-words px-3 py-0.5 text-current">{row.text || ' '}</pre>
        </div>
    )
}

function LineNumber({ value, tone }: { value?: number; tone: DiffRow['kind'] }) {
    return (
        <div
            className={cn(
                'select-none border-r border-border/40 px-2 py-0.5 text-right text-muted-foreground/70',
                tone === 'added' && 'bg-success/[0.08] text-success/75',
                tone === 'removed' && 'bg-danger/[0.08] text-danger/75'
            )}
        >
            {value ?? ''}
        </div>
    )
}

function buildRows(beforeText: string, afterText: string): DiffRow[] {
    let oldLine = 1
    let newLine = 1
    const rows: DiffRow[] = []

    for (const part of diffLines(beforeText, afterText)) {
        const lines = splitDiffLines(part)
        for (const line of lines) {
            if (part.added) {
                rows.push({ kind: 'added', newLine, text: line })
                newLine += 1
                continue
            }
            if (part.removed) {
                rows.push({ kind: 'removed', oldLine, text: line })
                oldLine += 1
                continue
            }
            rows.push({ kind: 'context', oldLine, newLine, text: line })
            oldLine += 1
            newLine += 1
        }
    }

    return rows
}

function splitDiffLines(part: Change): string[] {
    const lines = part.value.split('\n')
    if (part.value.endsWith('\n')) {
        lines.pop()
    }
    return lines
}

function prettyJsonText(text: string): string {
    try {
        return JSON.stringify(sortJsonValue(JSON.parse(text) as JsonValue), null, 2)
    } catch {
        return text
    }
}

function sortJsonValue(value: JsonValue): JsonValue {
    if (Array.isArray(value)) {
        return value.map(sortJsonValue)
    }
    if (value !== null && typeof value === 'object') {
        return Object.fromEntries(
            Object.keys(value)
                .sort()
                .map((key) => [key, sortJsonValue(value[key])])
        )
    }
    return value
}
