import { cn, formatDate, formatLatency, METHOD_CLASS, getStatusColor } from '@/lib/utils'
import { AlertTriangle, BookmarkCheck, CheckCircle2, ChevronRight, CircleDot, Clock3, Network, Server, Tag as TagIcon, Tags, Zap } from 'lucide-react'
import type { RequestLog } from '@/lib/api'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip"

interface LogTableProps {
    logs: RequestLog[]
    loading?: boolean
    onSelect: (log: RequestLog) => void
    selectedId?: string
}

function getStatusBadgeColor(code: number): string {
    if (code >= 200 && code < 300) return 'bg-success/10 text-success'
    if (code >= 300 && code < 400) return 'bg-warning/10 text-warning'
    if (code >= 400 && code < 500) return 'bg-warning/10 text-warning'
    if (code >= 500) return 'bg-danger/10 text-danger'
    return 'bg-muted text-muted-foreground'
}

function formatTokenCount(value?: number): string {
    if (typeof value !== 'number') return '-'
    if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(value >= 10_000_000 ? 0 : 1)}M`
    if (value >= 10_000) return `${(value / 1_000).toFixed(value >= 100_000 ? 0 : 1)}k`
    return value.toLocaleString()
}

function MobileLogSkeleton() {
    return (
        <div className="space-y-3 md:hidden">
            {Array.from({ length: 6 }).map((_, index) => (
                <div key={index} className="rounded-lg bg-card p-4 space-y-3">
                    <div className="flex items-center gap-2">
                        <Skeleton className="h-6 w-16 rounded-md bg-muted/50" />
                        <Skeleton className="h-6 w-14 rounded-md bg-muted/50" />
                        <Skeleton className="ml-auto h-5 w-12 rounded-md bg-muted/50" />
                    </div>
                    <Skeleton className="h-4 w-full bg-muted/50" />
                    <Skeleton className="h-4 w-3/4 bg-muted/50" />
                    <div className="flex gap-2">
                        <Skeleton className="h-5 w-20 rounded-md bg-muted/50" />
                        <Skeleton className="h-5 w-16 rounded-md bg-muted/50" />
                    </div>
                </div>
            ))}
        </div>
    )
}

function DesktopLogSkeleton({ t }: { t: (key: string) => string }) {
    return (
        <div className="hidden rounded-lg overflow-hidden bg-card md:block">
            <Table>
                <TableHeader className="bg-muted">
                    <TableRow>
                        <TableHead className="w-[80px]">{t('log_table.method')}</TableHead>
                        <TableHead className="w-[70px]">{t('log_table.status')}</TableHead>
                        <TableHead className="w-[100px]">{t('log_table.upstream')}</TableHead>
                        <TableHead>{t('log_table.path')}</TableHead>
                        <TableHead className="w-[80px] text-right">{t('log_table.latency')}</TableHead>
                        <TableHead className="w-[160px] text-right">{t('log_table.time')}</TableHead>
                        <TableHead className="w-10"></TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {Array.from({ length: 8 }).map((_, rowIndex) => (
                        <TableRow key={rowIndex}>
                            {Array.from({ length: 7 }).map((_, cellIndex) => (
                                <TableCell key={cellIndex}>
                                    <Skeleton className="h-5 w-full bg-muted/50" />
                                </TableCell>
                            ))}
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    )
}

function MobileLogCard({
    log,
    selected,
    onSelect,
    dateLabel,
    detailLabel,
    savedLabel,
    todoLabel,
    doneLabel,
    streamingLabel,
    modifiedLabel,
    tokensLabel,
    showUsage,
}: {
    log: RequestLog
    selected: boolean
    onSelect: (log: RequestLog) => void
    dateLabel: string
    detailLabel: string
    savedLabel: string
    todoLabel: string
    doneLabel: string
    streamingLabel: string
    modifiedLabel: string
    tokensLabel: string
    showUsage: boolean
}) {
    return (
        <button
            type="button"
            onClick={() => onSelect(log)}
            className={cn(
                'w-full rounded-lg p-4 text-left transition-all active:scale-[0.99]',
                selected
                    ? 'bg-primary/10'
                    : 'bg-card hover:bg-card/40'
            )}
        >
            <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1 space-y-3">
                    <div className="flex flex-wrap items-center gap-2">
                        <span
                            className={cn(
                                'inline-flex items-center rounded-md px-2.5 py-1 text-xs font-semibold',
                                METHOD_CLASS
                            )}
                        >
                            {log.method}
                        </span>
                        <span
                            className={cn(
                                'inline-flex items-center rounded-md px-2.5 py-1 text-xs font-semibold',
                                getStatusBadgeColor(log.status_code)
                            )}
                        >
                            {log.status_code || '---'}
                        </span>
                        <span className="inline-flex items-center gap-1 rounded-md bg-background px-2.5 py-1 text-xs font-semibold text-muted-foreground">
                            <Server className="h-3 w-3" />
                            <span className="truncate max-w-[160px]">
                                {log.upstream}{log.upstream_target ? ` / ${log.upstream_target}` : ''}
                            </span>
                        </span>
                        {log.streaming && (
                            <span className="inline-flex items-center gap-1 rounded-md bg-primary/10 px-2.5 py-1 text-xs font-medium text-primary">
                                <Zap className="h-3 w-3" />
                                {streamingLabel}
                            </span>
                        )}
                        {log.request_override_applied && (
                            <span className="inline-flex items-center gap-1 rounded-md border border-border bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">
                                <AlertTriangle className="h-3 w-3" />
                                {modifiedLabel}
                            </span>
                        )}
                        {log.trace_id && (
                            <span className="inline-flex items-center gap-1 rounded-md bg-info/10 px-2.5 py-1 text-xs font-medium text-info">
                                <Network className="h-3 w-3" />
                                TRACE
                            </span>
                        )}
                        {log.tag && (
                            <span className="inline-flex items-center gap-1 rounded-md border border-border bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">
                                <TagIcon className="h-3 w-3" />
                                <span className="truncate max-w-[120px]">{log.tag}</span>
                            </span>
                        )}
                        {log.annotation?.saved && (
                            <span className="inline-flex items-center gap-1 rounded-md bg-primary/10 px-2.5 py-1 text-xs font-medium text-primary">
                                <BookmarkCheck className="h-3 w-3" />
                                {savedLabel}
                            </span>
                        )}
                        {log.annotation?.status === 'todo' && (
                            <span className="inline-flex items-center gap-1 rounded-md bg-warning/10 px-2.5 py-1 text-xs font-medium text-warning">
                                <CircleDot className="h-3 w-3" />
                                {todoLabel}
                            </span>
                        )}
                        {log.annotation?.status === 'done' && (
                            <span className="inline-flex items-center gap-1 rounded-md bg-success/10 px-2.5 py-1 text-xs font-medium text-success">
                                <CheckCircle2 className="h-3 w-3" />
                                {doneLabel}
                            </span>
                        )}
                    </div>

                    <div className="space-y-1.5">
                        <div className="font-mono text-xs leading-relaxed text-foreground break-all">
                            {log.path}
                            {log.query && <span className="text-muted-foreground/80">?{log.query}</span>}
                        </div>
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground/70">
                            <span className="inline-flex items-center gap-1">
                                <Clock3 className="h-3.5 w-3.5" />
                                {formatLatency(log.latency_ms)}
                            </span>
                            {showUsage && typeof log.usage_total_tokens === 'number' && (
                                <span>{tokensLabel}: {formatTokenCount(log.usage_total_tokens)}</span>
                            )}
                            <span>{dateLabel}</span>
                            {log.annotation?.labels?.length ? (
                                <span className="inline-flex items-center gap-1">
                                    <Tags className="h-3.5 w-3.5" />
                                    {log.annotation.labels.join(', ')}
                                </span>
                            ) : null}
                        </div>
                    </div>
                </div>

                <div className="shrink-0 inline-flex items-center gap-1 rounded-md bg-background px-2 py-1 text-xs font-semibold text-muted-foreground">
                    <span>{detailLabel}</span>
                    <ChevronRight className="h-3.5 w-3.5" />
                </div>
            </div>
        </button>
    )
}

export function LogTable({ logs, loading, onSelect, selectedId }: LogTableProps) {
    const { t, i18n } = useTranslation()
    const navigate = useNavigate()
    const showUsage = logs.some(log => typeof log.usage_total_tokens === 'number')

    if (loading) {
        return (
            <>
                <MobileLogSkeleton />
                <DesktopLogSkeleton t={t} />
            </>
        )
    }

    if (logs.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-20 sm:py-24 text-muted-foreground bg-card rounded-lg px-6">
                <div className="text-5xl sm:text-6xl mb-6 grayscale opacity-50">📭</div>
                <div className="text-lg sm:text-xl font-semibold tracking-tight text-foreground/70 text-center">{t('log_table.no_logs')}</div>
                <p className="text-sm mt-2 max-w-[280px] text-center leading-relaxed font-medium text-muted-foreground/80">
                    {t('log_table.send_requests_hint', '发送一些请求后这里会显示日志')}
                </p>
            </div>
        )
    }

    return (
        <>
            <div className="space-y-3 md:hidden">
                {logs.map((log) => (
                    <MobileLogCard
                        key={log.id}
                        log={log}
                        selected={selectedId === log.id}
                        onSelect={onSelect}
                        dateLabel={formatDate(log.created_at, i18n.language)}
                        detailLabel={t('common.details')}
                        savedLabel={t('log_annotation.saved', '已保存')}
                        todoLabel={t('log_annotation.todo', '待处理')}
                        doneLabel={t('log_annotation.done', '已处理')}
                        streamingLabel={t('log_detail.streaming', '流式')}
                        modifiedLabel={t('log_detail.modified', 'MODIFIED')}
                        tokensLabel={t('log_table.tokens', 'Tokens')}
                        showUsage={showUsage}
                    />
                ))}
            </div>

            <div className="hidden rounded-lg overflow-hidden bg-card md:block">
                <Table>
                    <TableHeader className="bg-muted">
                        <TableRow className="hover:bg-transparent">
                            <TableHead className="w-[80px] font-medium text-xs">{t('log_table.method')}</TableHead>
                            <TableHead className="w-[70px] font-medium text-xs text-center">{t('log_table.status')}</TableHead>
                            <TableHead className="w-[100px] font-medium text-xs">{t('log_table.upstream')}</TableHead>
                            <TableHead className="font-medium text-xs">{t('log_table.path')}</TableHead>
                            {showUsage && (
                                <TableHead className="w-[90px] font-medium text-xs text-right">{t('log_table.tokens', 'Tokens')}</TableHead>
                            )}
                            <TableHead className="w-[100px] font-medium text-xs text-right">{t('log_table.latency')}</TableHead>
                            <TableHead className="w-[180px] font-medium text-xs text-right">{t('log_table.time')}</TableHead>
                            <TableHead className="w-10"></TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {logs.map((log) => (
                            <TableRow
                                key={log.id}
                                onClick={() => onSelect(log)}
                                aria-label={t('common.details')}
                                className={cn(
                                    'group cursor-pointer border-b transition-colors',
                                    selectedId === log.id ? 'bg-accent hover:bg-accent' : 'hover:bg-muted/60'
                                )}
                            >
                                <TableCell>
                                    <div
                                        className={cn(
                                            'inline-flex items-center justify-center min-w-[56px] h-6 px-2 rounded-md text-xs font-semibold',
                                            METHOD_CLASS
                                        )}
                                    >
                                        {log.method}
                                    </div>
                                </TableCell>
                                <TableCell className="text-center">
                                    <span
                                        className={cn(
                                            'font-mono text-xs font-medium',
                                            getStatusColor(log.status_code)
                                        )}
                                    >
                                        {log.status_code || '---'}
                                    </span>
                                </TableCell>
                                <TableCell>
                                    <span className="block max-w-[110px] truncate text-xs font-semibold text-muted-foreground/85">
                                        {log.upstream}{log.upstream_target ? ` / ${log.upstream_target}` : ''}
                                    </span>
                                </TableCell>
                                <TableCell className="max-w-0">
                                    <div className="flex items-center gap-2">
                                        <span className="truncate font-mono text-xs text-foreground/90 select-text">
                                            {log.path}
                                            {log.query && <span className="text-muted-foreground/75">?{log.query}</span>}
                                        </span>
                                        {log.trace_id && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <span
                                                        role="link"
                                                        onClick={(e) => { e.stopPropagation(); navigate(`/traces/${encodeURIComponent(log.trace_id!)}`) }}
                                                        className="shrink-0 inline-flex items-center gap-0.5 h-[18px] px-1.5 rounded-md text-xs font-semibold tracking-tight bg-info/10 text-info cursor-pointer hover:bg-info/20 transition-colors"
                                                    >
                                                        <Network className="h-2.5 w-2.5" />
                                                        TRACE
                                                    </span>
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">Trace: {log.trace_id}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.tag && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <span className="shrink-0 inline-flex items-center h-[18px] px-1.5 rounded-md border border-border text-xs font-medium bg-muted text-muted-foreground">
                                                        {log.tag}
                                                    </span>
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_table.tag')}: {log.tag}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.streaming && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <div className="shrink-0">
                                                        <Zap className="h-3 w-3 text-primary fill-primary/20" />
                                                    </div>
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_detail.streaming', '流式响应')}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.request_override_applied && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <span className="shrink-0 inline-flex items-center h-[18px] px-1.5 rounded-md border border-border text-xs font-medium bg-muted text-muted-foreground">
                                                        {t('log_detail.modified', 'MODIFIED')}
                                                    </span>
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_detail.request_override', 'Request Override')}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.annotation?.saved && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <BookmarkCheck className="h-3.5 w-3.5 shrink-0 text-primary" />
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_annotation.saved', 'Saved')}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.annotation?.status === 'todo' && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <CircleDot className="h-3.5 w-3.5 shrink-0 text-warning" />
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_annotation.todo', 'Todo')}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.annotation?.status === 'done' && (
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_annotation.done', 'Done')}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        )}
                                        {log.annotation?.labels?.map((label) => (
                                            <Tooltip key={label}>
                                                <TooltipTrigger asChild>
                                                    <span className="shrink-0 inline-flex items-center h-[18px] px-1.5 rounded-md text-xs font-semibold tracking-tight bg-primary/10 text-primary">
                                                        {label}
                                                    </span>
                                                </TooltipTrigger>
                                                <TooltipContent side="right">
                                                    <p className="text-xs font-medium">{t('log_annotation.label', 'Label')}: {label}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        ))}
                                    </div>
                                </TableCell>
                                {showUsage && (
                                    <TableCell className="text-right">
                                        <span className="font-mono text-xs font-semibold text-muted-foreground">
                                            {formatTokenCount(log.usage_total_tokens)}
                                        </span>
                                    </TableCell>
                                )}
                                <TableCell className="text-right">
                                    <span className="text-xs text-muted-foreground font-mono font-medium">
                                        {formatLatency(log.latency_ms)}
                                    </span>
                                </TableCell>
                                <TableCell className="text-right">
                                    <span className="text-xs text-muted-foreground/85 font-medium">
                                        {formatDate(log.created_at, i18n.language)}
                                    </span>
                                </TableCell>
                                <TableCell className="text-right">
                                    <ChevronRight
                                        className={cn(
                                            'ml-auto h-4 w-4 text-muted-foreground transition-opacity',
                                            selectedId === log.id ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
                                        )}
                                        aria-hidden="true"
                                    />
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        </>
    )
}
