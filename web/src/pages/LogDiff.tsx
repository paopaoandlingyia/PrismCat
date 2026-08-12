import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, Copy, FileCode } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { TextDiffViewer } from '@/components/TextDiffViewer'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { fetchLog, type RequestLog } from '@/lib/api'
import { cn, formatDate, formatSize, getMethodColor, getStatusColor } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'

interface LogDiffState {
    id?: string
    log: RequestLog | null
    loading: boolean
    error: string | null
}

function createLogDiffState(id: string | undefined): LogDiffState {
    return {
        id,
        log: null,
        loading: Boolean(id),
        error: id ? null : 'missing-id',
    }
}

export function LogDiff() {
    const { id } = useParams()
    const { t, i18n } = useTranslation()
    const [state, setState] = useState(() => createLogDiffState(id))
    let currentState = state
    if (state.id !== id) {
        currentState = createLogDiffState(id)
        setState(currentState)
    }

    useEffect(() => {
        let cancelled = false

        if (!id) {
            return
        }

        fetchLog(id)
            .then((next) => {
                if (!cancelled) setState({ id, log: next, loading: false, error: null })
            })
            .catch((err) => {
                if (!cancelled) {
                    setState({
                        id,
                        log: null,
                        loading: false,
                        error: err instanceof Error ? err.message : t('common.error'),
                    })
                }
            })

        return () => {
            cancelled = true
        }
    }, [id, t])

    const { log, loading } = currentState
    const error = currentState.error === 'missing-id'
        ? t('log_diff.missing_id', 'Missing log id')
        : currentState.error
    const originalBody = log?.request_body_original ?? ''
    const finalBody = log?.request_body_final ?? ''
    const hasDiff = Boolean(originalBody && finalBody && originalBody !== finalBody)
    const targetPath = useMemo(() => {
        if (!log) return ''
        return `${log.path}${log.query ? `?${log.query}` : ''}`
    }, [log])

    const copyFinalBody = async () => {
        if (await copyText(finalBody)) {
            toast.success(t('log_detail.copy_success'))
        } else {
            toast.error(t('log_detail.copy_failed'))
        }
    }

    if (loading) {
        return (
            <div className="flex min-h-[55vh] flex-col items-center justify-center gap-4">
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
                <div className="text-sm font-medium text-muted-foreground">{t('common.loading')}</div>
            </div>
        )
    }

    if (error || !log) {
        return (
            <DiffPageShell>
                <EmptyState
                    icon={<AlertTriangle className="h-5 w-5" />}
                    title={t('common.error')}
                    message={error || t('app.load_log_detail_failed')}
                />
            </DiffPageShell>
        )
    }

    return (
        <DiffPageShell>
            <div className="space-y-5">
                <div className="flex flex-col gap-4 rounded-lg border border-border/60 bg-card px-5 py-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0 space-y-3">
                        <div className="flex flex-wrap items-center gap-2">
                            <Badge variant="outline" className={cn('rounded-md px-2 py-0.5 text-xs font-medium', getMethodColor(log.method))}>
                                {log.method}
                            </Badge>
                            <span className={cn('font-mono text-lg font-semibold tracking-tight', getStatusColor(log.status_code))}>
                                {log.status_code || '---'}
                            </span>
                            {log.request_override_applied && (
                                <Badge variant="outline" className="border-warning/30 bg-warning/10 text-xs font-medium text-warning">
                                    {t('log_detail.modified', 'MODIFIED')}
                                </Badge>
                            )}
                        </div>
                        <div className="min-w-0 space-y-1">
                            <div className="break-all font-mono text-sm font-semibold text-foreground">{targetPath}</div>
                            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs font-medium text-muted-foreground">
                                <span>{log.upstream}{log.upstream_target ? ` / ${log.upstream_target}` : ''}</span>
                                <span>{formatDate(log.created_at, i18n.language)}</span>
                                <span>{formatSize(log.request_body_size)}</span>
                            </div>
                        </div>
                        {log.request_override_rules?.length ? (
                            <div className="flex flex-wrap gap-2">
                                {log.request_override_rules.map((rule) => (
                                    <Badge key={rule} variant="outline" className="border-warning/30 bg-background/60 text-xs font-semibold">
                                        {rule}
                                    </Badge>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    <div className="flex shrink-0 flex-wrap gap-2">
                        <Button asChild variant="outline" size="sm" className="h-8 gap-1.5 text-xs">
                            <Link to="/">
                                <ArrowLeft className="h-3.5 w-3.5" />
                                {t('log_diff.back_to_logs', 'Back to Logs')}
                            </Link>
                        </Button>
                        {hasDiff && (
                            <Button type="button" variant="outline" size="sm" onClick={copyFinalBody} className="h-8 gap-1.5 text-xs">
                                <Copy className="h-3.5 w-3.5" />
                                {t('log_diff.copy_final', 'Copy Final')}
                            </Button>
                        )}
                    </div>
                </div>

                {hasDiff ? (
                    <div className="rounded-lg border border-border/60 bg-card p-4">
                        <div className="mb-4 flex items-center gap-2 text-xs font-medium text-muted-foreground">
                            <FileCode className="h-4 w-4 text-primary" />
                            {t('log_diff.request_diff', 'Request Diff')}
                        </div>
                        <TextDiffViewer beforeText={originalBody} afterText={finalBody} />
                    </div>
                ) : (
                    <EmptyState
                        icon={<FileCode className="h-5 w-5" />}
                        title={t('log_diff.no_diff_title', 'No request diff')}
                        message={t('log_diff.no_diff_message', 'This log does not contain a modified request body.')}
                    />
                )}
            </div>
        </DiffPageShell>
    )
}

function DiffPageShell({ children }: { children: ReactNode }) {
    return (
        <div className="mx-auto flex w-full max-w-[1500px] flex-col gap-5">
            {children}
        </div>
    )
}

function EmptyState({
    icon,
    title,
    message,
}: {
    icon: ReactNode
    title: string
    message: string
}) {
    return (
        <div className="flex min-h-[46vh] flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border/70 bg-card/70 px-6 py-16 text-center">
            <div className="rounded-md bg-muted p-3 text-muted-foreground">{icon}</div>
            <div className="text-sm font-medium text-foreground">{title}</div>
            <div className="max-w-md text-xs leading-6 text-muted-foreground">{message}</div>
        </div>
    )
}
