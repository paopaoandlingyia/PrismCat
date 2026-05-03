import { cn, formatDate, formatLatency, formatSize, getStatusColor, getMethodColor } from '@/lib/utils'
import { Copy, Check, Zap, AlertTriangle, ChevronDown, ChevronUp, ChevronsDownUp, ChevronsUpDown, FileCode, ListTree, Globe, Layers, RotateCcw, Maximize2, Minimize2, ExternalLink, Terminal } from 'lucide-react'
import { fetchBlob } from '@/lib/api'
import type { LiveLogEvent, RequestLog } from '@/lib/api'
import { startTransition, useEffect, useMemo, useState, type ComponentType, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { JsonViewer, type JsonExpandMode } from './JsonViewer'
import { JsonDiffViewer } from './JsonDiffViewer'
import { BlobPanel } from './BlobPanel'
import { mergeStreamBody } from '@/lib/streamMerge'
import { logRequestDiffPath } from '@/lib/routes'
import { buildCurlCommand } from '@/lib/curlExport'
import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

interface LogDetailProps {
    log: RequestLog | null
    loading?: boolean
    onClose: () => void
}

type BodyViewMode = 'pretty' | 'raw'
type RequestViewMode = BodyViewMode | 'diff'
type ResponseViewMode = BodyViewMode | 'merged'
type PanelWidthMode = 'wide' | 'full'

const logDetailWidthStorageKey = 'prismcat.logDetail.width'
const logDetailExpandedStorageKey = 'prismcat.logDetail.expanded'

const defaultExpandedSections = {
    url: true,
    requestHeaders: false,
    requestBody: false,
    responseHeaders: false,
    responseBody: false,
}

function getInitialPanelWidthMode(): PanelWidthMode {
    if (typeof window === 'undefined') return 'wide'

    const stored = window.localStorage.getItem(logDetailWidthStorageKey)
    if (stored === 'full') {
        return stored
    }

    return 'wide'
}

function getInitialExpandedSections(): typeof defaultExpandedSections {
    if (typeof window === 'undefined') return defaultExpandedSections
    try {
        const raw = window.localStorage.getItem(logDetailExpandedStorageKey)
        if (!raw) return defaultExpandedSections
        const parsed = JSON.parse(raw) as Partial<typeof defaultExpandedSections>
        return {
            url: typeof parsed?.url === 'boolean' ? parsed.url : defaultExpandedSections.url,
            requestHeaders: typeof parsed?.requestHeaders === 'boolean' ? parsed.requestHeaders : defaultExpandedSections.requestHeaders,
            requestBody: typeof parsed?.requestBody === 'boolean' ? parsed.requestBody : defaultExpandedSections.requestBody,
            responseHeaders: typeof parsed?.responseHeaders === 'boolean' ? parsed.responseHeaders : defaultExpandedSections.responseHeaders,
            responseBody: typeof parsed?.responseBody === 'boolean' ? parsed.responseBody : defaultExpandedSections.responseBody,
        }
    } catch {
        return defaultExpandedSections
    }
}

export function LogDetail({ log, loading, onClose }: LogDetailProps) {
    const { t, i18n } = useTranslation()
    const navigate = useNavigate()
    const [liveLog, setLiveLog] = useState<RequestLog | null>(null)
    const [copiedField, setCopiedField] = useState<string | null>(null)
    const [fullRequestBody, setFullRequestBody] = useState<string | null>(null)
    const [fullResponseBody, setFullResponseBody] = useState<string | null>(null)
    const [blobLoading, setBlobLoading] = useState<{ request: boolean; response: boolean }>({
        request: false,
        response: false,
    })
    const [blobError, setBlobError] = useState<string | null>(null)
    const [expandedSections, setExpandedSections] = useState(getInitialExpandedSections)
    const [requestViewMode, setRequestViewMode] = useState<RequestViewMode>('pretty')
    const [responseViewMode, setResponseViewMode] = useState<ResponseViewMode>('pretty')
    const [requestExpandMode, setRequestExpandMode] = useState<JsonExpandMode>('default')
    const [responseExpandMode, setResponseExpandMode] = useState<JsonExpandMode>('default')
    const [panelWidthMode, setPanelWidthMode] = useState<PanelWidthMode>(() => getInitialPanelWidthMode())
    const displayLog = liveLog ?? log

    useEffect(() => {
        setLiveLog(null)
        setFullRequestBody(null)
        setFullResponseBody(null)
        setBlobError(null)
        setBlobLoading({ request: false, response: false })
        setRequestViewMode('pretty')
        setResponseViewMode(log?.streaming ? 'raw' : 'pretty')
        setRequestExpandMode('default')
        setResponseExpandMode('default')
    }, [log?.id])

    useEffect(() => {
        if (typeof window === 'undefined') return
        window.localStorage.setItem(logDetailWidthStorageKey, panelWidthMode)
    }, [panelWidthMode])

    useEffect(() => {
        if (typeof window === 'undefined') return
        window.localStorage.setItem(logDetailExpandedStorageKey, JSON.stringify(expandedSections))
    }, [expandedSections])

    useEffect(() => {
        if (!log || !shouldSubscribeLive(log)) {
            setLiveLog(null)
            return
        }

        const source = new EventSource(`/api/logs/${log.id}/live`)
        const handleEvent = (event: MessageEvent<string>) => {
            const next = parseLiveEvent(event)
            if (!next) return

            if (next.type === 'snapshot' && next.log) {
                startTransition(() => {
                    setLiveLog(next.log ?? null)
                })
                return
            }

            if (next.type === 'response_chunk') {
                startTransition(() => {
                    setLiveLog((current) => {
                        const base = current ?? log
                        if (!base) return current

                        return {
                            ...base,
                            response_body: `${base.response_body ?? ''}${next.chunk ?? ''}`,
                            response_body_size: base.response_body_size + (next.size_delta ?? 0),
                        }
                    })
                })
                return
            }

            if (next.type === 'completed' && next.log) {
                startTransition(() => {
                    setLiveLog(next.log ?? null)
                })
                source.close()
            }
        }

        source.addEventListener('snapshot', handleEvent as EventListener)
        source.addEventListener('response_chunk', handleEvent as EventListener)
        source.addEventListener('completed', handleEvent as EventListener)
        source.onerror = () => {
            source.close()
        }

        return () => {
            source.close()
        }
    }, [log])

    useEffect(() => {
        if (displayLog?.streaming && responseViewMode === 'pretty') {
            setResponseViewMode('raw')
        }
    }, [displayLog?.streaming, responseViewMode])

    const effectiveRequestBody = fullRequestBody ?? displayLog?.request_body ?? ''
    const effectiveResponseBody = fullResponseBody ?? displayLog?.response_body ?? ''
    const originalRequestBody = displayLog?.request_body_original ?? ''
    const finalRequestBody = displayLog?.request_body_final ?? (originalRequestBody ? effectiveRequestBody : '')
    const hasRequestBodyDiff = Boolean(originalRequestBody && finalRequestBody && originalRequestBody !== finalRequestBody)
    const requestContentType = firstHeaderValue(displayLog?.request_headers, 'Content-Type')
    const responseContentType = firstHeaderValue(displayLog?.response_headers, 'Content-Type')
    const requestBodyIsBinary = isBinaryPlaceholder(effectiveRequestBody)
    const responseBodyIsBinary = isBinaryPlaceholder(effectiveResponseBody)
    const shouldInspectRequestBody = expandedSections.requestBody && requestViewMode === 'pretty' && Boolean(effectiveRequestBody)
    const shouldInspectResponseBody = expandedSections.responseBody && Boolean(effectiveResponseBody)

    useEffect(() => {
        if (!hasRequestBodyDiff && requestViewMode === 'diff') {
            setRequestViewMode('pretty')
        }
    }, [hasRequestBodyDiff, requestViewMode])

    const parsedRequestBody = useMemo(() => {
        if (!shouldInspectRequestBody) return null
        try {
            return JSON.parse(effectiveRequestBody)
        } catch {
            return null
        }
    }, [shouldInspectRequestBody, effectiveRequestBody])

    const parsedResponseBody = useMemo(() => {
        if (!shouldInspectResponseBody || responseViewMode !== 'pretty') return null
        try {
            return JSON.parse(effectiveResponseBody)
        } catch {
            return null
        }
    }, [shouldInspectResponseBody, responseViewMode, effectiveResponseBody])

    const mergedResponse = useMemo(() => {
        if (!shouldInspectResponseBody || !displayLog?.streaming || responseViewMode !== 'merged') return null
        return mergeStreamBody(effectiveResponseBody)
    }, [shouldInspectResponseBody, displayLog?.streaming, responseViewMode, effectiveResponseBody])


    const copyToClipboard = async (text: string, field: string) => {
        await navigator.clipboard.writeText(text)
        setCopiedField(field)
        setTimeout(() => setCopiedField(null), 2000)
    }

    const copyCurlCommand = async () => {
        const currentLog = displayLog
        if (!currentLog) return

        let body = currentLog.request_body_final || fullRequestBody || currentLog.request_body || ''
        if (!currentLog.request_body_final && currentLog.request_body_ref && !fullRequestBody) {
            body = await fetchBlob(currentLog.request_body_ref)
            startTransition(() => setFullRequestBody(body))
        }
        await copyToClipboard(buildCurlCommand(currentLog, body), 'curl')
    }

    const toggleSection = (section: keyof typeof expandedSections) => {
        setExpandedSections(prev => ({ ...prev, [section]: !prev[section] }))
    }

    const loadBlob = async (kind: 'request' | 'response', ref: string) => {
        setBlobError(null)
        setBlobLoading(prev => ({ ...prev, [kind]: true }))
        try {
            const body = await fetchBlob(ref)
            startTransition(() => {
                if (kind === 'request') setFullRequestBody(body)
                else setFullResponseBody(body)
            })
        } catch (err: any) {
            setBlobError(err?.message || 'Failed to load blob')
        } finally {
            setBlobLoading(prev => ({ ...prev, [kind]: false }))
        }
    }

    if (!displayLog) return null

    const sheetWidthClassName = cn(
        "w-full p-0 flex flex-col bg-background shadow-2xl",
        panelWidthMode === 'wide' && "border-l border-border/60 sm:rounded-l-2xl sm:max-w-6xl",
        panelWidthMode === 'full' && "border-0 sm:rounded-none sm:max-w-none"
    )
    const sectionCardClassName = "rounded-2xl border border-border/60 bg-card p-5"
    const contentCardClassName = "rounded-lg bg-muted/50 p-3.5"
    const codeCardClassName = "rounded-lg bg-muted/50"
    const emptyStateClassName = "rounded-lg border border-dashed border-border/50 bg-muted/50 px-4 py-6 text-center"

    const CopyButton = ({ text, field }: { text: string; field: string }) => {
        const label = copiedField === field ? t('common.copied', '已复制') : t('common.copy', '复制')
        return (
            <Tooltip>
                <TooltipTrigger asChild>
                    <Button
                        variant="ghost"
                        size="icon"
                        onClick={(e) => {
                            e.stopPropagation()
                            copyToClipboard(text, field)
                        }}
                        className="h-7 w-7 rounded-md hover:bg-primary/10 hover:text-primary transition-all"
                        aria-label={label}
                    >
                        {copiedField === field ? (
                            <Check className="h-3.5 w-3.5 text-green-500" />
                        ) : (
                            <Copy className="h-3.5 w-3.5 text-muted-foreground" />
                        )}
                    </Button>
                </TooltipTrigger>
                <TooltipContent side="top" sideOffset={4}>{label}</TooltipContent>
            </Tooltip>
        )
    }

    const RawBodyViewer = ({ text }: { text: string }) => (
        <pre className="whitespace-pre-wrap break-all text-[11px] font-mono leading-relaxed text-foreground select-text">
            {text}
        </pre>
    )

    const ViewToggle = ({
        value,
        options,
        onChange,
    }: {
        value: string
        options: Array<{ value: string; label: string }>
        onChange: (value: string) => void
    }) => (
        <div className="flex items-center gap-1 rounded-lg bg-muted p-1">
            {options.map((option) => (
                <Button
                    key={option.value}
                    type="button"
                    variant={value === option.value ? 'secondary' : 'ghost'}
                    size="sm"
                    onClick={() => onChange(option.value)}
                    className={cn(
                        "h-6 rounded-md px-2 text-[10px] font-bold uppercase tracking-wider transition-all",
                        value === option.value
                            ? "border border-border/70 bg-background text-foreground shadow-sm hover:bg-background"
                            : "text-muted-foreground hover:bg-background/70 hover:text-foreground"
                    )}
                >
                    {option.label}
                </Button>
            ))}
        </div>
    )

    const ExpandToggle = ({
        mode,
        onChange,
    }: {
        mode: JsonExpandMode
        onChange: (mode: JsonExpandMode) => void
    }) => {
        const willExpand = mode !== 'all'
        const label = willExpand ? t('log_detail.expand_all', '展开全部') : t('log_detail.collapse_all', '折叠全部')
        return (
            <Tooltip>
                <TooltipTrigger asChild>
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        onClick={() => onChange(willExpand ? 'all' : 'none')}
                        className="h-7 w-7 rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                        aria-label={label}
                    >
                        {willExpand ? <ChevronsUpDown className="h-3.5 w-3.5" /> : <ChevronsDownUp className="h-3.5 w-3.5" />}
                    </Button>
                </TooltipTrigger>
                <TooltipContent side="top" sideOffset={4}>{label}</TooltipContent>
            </Tooltip>
        )
    }

    const SectionHeader = ({
        title,
        section,
        icon: Icon,
        extra,
    }: {
        title: string
        section: keyof typeof defaultExpandedSections
        icon: ComponentType<{ className?: string }>
        extra?: ReactNode
    }) => (
        <div className="flex items-center justify-between gap-3 py-2.5">
            <button
                type="button"
                onClick={() => toggleSection(section)}
                className="group -mx-1 flex min-w-0 flex-1 items-center gap-2 rounded-lg px-1 py-0.5 text-left transition-colors"
            >
                <div className={cn(
                    "rounded-md p-1.5 transition-colors",
                    expandedSections[section]
                        ? "bg-primary/10 text-primary ring-1 ring-primary/20"
                        : "bg-muted text-muted-foreground group-hover:bg-secondary"
                )}>
                    <Icon className="h-3.5 w-3.5" />
                </div>
                <span className="text-xs font-bold uppercase tracking-wider text-foreground group-hover:text-primary transition-colors">
                    {title}
                </span>
                {expandedSections[section] ? (
                    <ChevronUp className="h-3.5 w-3.5 text-muted-foreground" />
                ) : (
                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                )}
            </button>
            {extra ? <div className="shrink-0">{extra}</div> : null}
        </div>
    )

    return (
        <Sheet open={!!log} onOpenChange={(open) => !open && onClose()}>
            <SheetContent className={sheetWidthClassName}>
                {/* 头部固定区域 */}
                <SheetHeader className="border-b border-border/60 bg-card px-6 py-5">
                    <div className="flex flex-wrap items-center gap-3">
                        <div
                                className={cn(
                                    "w-14 py-0.5 rounded-[3px] text-[10px] text-center uppercase font-bold border",
                                    getMethodColor(displayLog.method)
                                )}
                        >
                            {displayLog.method}
                        </div>
                        <SheetTitle className={cn(
                            "font-mono text-xl font-black tracking-tighter",
                            getStatusColor(displayLog.status_code)
                        )}>
                            {displayLog.status_code || '---'}
                        </SheetTitle>
                        {displayLog.streaming && (
                            <Badge variant="secondary" className="border-none bg-primary/10 text-primary font-bold text-[10px] animate-pulse">
                                <Zap className="mr-1 h-3 w-3 fill-current" />
                                {t('log_detail.streaming', 'STREAMING')}
                            </Badge>
                        )}
                        {displayLog.error && (
                            <Badge variant="destructive" className="border-none bg-red-500/10 text-red-500 font-bold text-[10px]">
                                <AlertTriangle className="mr-1 h-3 w-3" />
                                {t('common.error', 'ERROR')}
                            </Badge>
                        )}
                        {displayLog.request_override_applied && (
                            <Badge variant="outline" className="border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400 font-bold text-[10px]">
                                {t('log_detail.modified', 'MODIFIED')}
                            </Badge>
                        )}
                        {loading && (
                            <div className="ml-auto flex items-center gap-2 text-[11px] font-bold text-primary animate-pulse">
                                <div className="h-1 w-1 rounded-full bg-current" />
                                {t('common.loading')}
                            </div>
                        )}
                        {!loading && (
                            <div className="ml-auto mr-10 flex flex-wrap items-center justify-end gap-2">
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="icon"
                                            onClick={() => setPanelWidthMode(current => current === 'full' ? 'wide' : 'full')}
                                            className="h-8 w-8 rounded-lg text-muted-foreground hover:bg-primary/10 hover:text-primary"
                                            aria-label={panelWidthMode === 'full'
                                                ? t('log_detail.exit_fullscreen', 'Exit fullscreen')
                                                : t('log_detail.enter_fullscreen', 'Fullscreen')}
                                        >
                                            {panelWidthMode === 'full'
                                                ? <Minimize2 className="h-4 w-4" />
                                                : <Maximize2 className="h-4 w-4" />}
                                        </Button>
                                    </TooltipTrigger>
                                    <TooltipContent side="top" sideOffset={4}>
                                        {panelWidthMode === 'full'
                                            ? t('log_detail.exit_fullscreen', 'Exit fullscreen')
                                            : t('log_detail.enter_fullscreen', 'Fullscreen')}
                                    </TooltipContent>
                                </Tooltip>
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="h-7 gap-1.5 border-border/60 bg-background/60 px-2.5 text-[11px] font-semibold shadow-sm transition-all hover:border-primary/30 hover:bg-primary/10 hover:text-primary"
                                    onClick={copyCurlCommand}
                                >
                                    {copiedField === 'curl' ? (
                                        <Check className="h-3 w-3 text-green-500" />
                                    ) : (
                                        <Terminal className="h-3 w-3" />
                                    )}
                                    {t('log_detail.copy_as_curl', 'Copy as cURL')}
                                </Button>
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="h-7 gap-1.5 border-primary/20 bg-primary/5 px-2.5 text-[11px] font-semibold shadow-sm transition-all hover:border-primary/30 hover:bg-primary/10 hover:text-primary"
                                    onClick={async () => {
                                        const navigateToPlayground = (body: string) => {
                                            onClose()
                                            navigate('/playground', {
                                                state: {
                                                    replay: {
                                                        upstream: displayLog.upstream,
                                                        method: displayLog.method,
                                                        path: displayLog.path + (displayLog.query ? '?' + displayLog.query : ''),
                                                        headers: displayLog.request_headers,
                                                        body,
                                                    },
                                                },
                                            })
                                        }

                                        // If blob ref exists and not yet loaded, fetch full body first
                                        if (displayLog.request_body_ref && !fullRequestBody) {
                                            try {
                                                const full = await fetchBlob(displayLog.request_body_ref)
                                                navigateToPlayground(full)
                                            } catch {
                                                // Fallback to preview if blob fetch fails
                                                navigateToPlayground(effectiveRequestBody)
                                            }
                                        } else {
                                            navigateToPlayground(effectiveRequestBody)
                                        }
                                    }}
                                >
                                    <RotateCcw className="h-3 w-3" />
                                    {t('playground.replay')}
                                </Button>
                            </div>
                        )}
                    </div>
                </SheetHeader>

                {/* 主内容区域 */}
                <div className="custom-scrollbar flex-1 space-y-6 overflow-y-auto bg-muted/30 px-6 py-6">
                    {/* 基本信息网格 */}
                    <div className="grid grid-cols-2 gap-6 rounded-2xl border border-border/60 bg-card p-5 sm:grid-cols-4">
                        {[
                            { label: t('log_table.upstream'), value: displayLog.upstream, mono: false },
                            { label: t('log_table.latency'), value: formatLatency(displayLog.latency_ms), mono: true },
                            { label: t('log_table.time'), value: formatDate(displayLog.created_at, i18n.language), mono: false },
                            { label: 'ID', value: displayLog.id.substring(0, 8) + '...', mono: true, full: displayLog.id }
                        ].map((item, idx) => (
                            <div key={idx} className="space-y-1">
                                <div className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider">{item.label}</div>
                                <div className={cn(
                                    "text-sm font-bold truncate text-foreground",
                                    item.mono ? "font-mono" : ""
                                )} title={item.full}>
                                    {item.value}
                                </div>
                            </div>
                        ))}
                    </div>

                    {/* URL 地址 */}
                    <div className={sectionCardClassName}>
                        <SectionHeader title={t('log_detail.url')} section="url" icon={Globe} />
                        {expandedSections.url && (
                            <div className={cn(contentCardClassName, "group flex items-center gap-2 transition-colors hover:bg-muted")}>
                                <code className="flex-1 text-xs font-mono break-all leading-relaxed text-foreground">{displayLog.target_url}</code>
                                <CopyButton text={displayLog.target_url} field="url" />
                            </div>
                        )}
                    </div>

                    {/* 错误详情 */}
                    {displayLog.error && (
                        <div className="overflow-hidden rounded-2xl border border-red-500/20 bg-red-500/5 p-4">
                            <div className="mb-3 flex items-center gap-2 text-red-500 font-bold text-xs uppercase tracking-wider">
                                <AlertTriangle className="h-4 w-4" />
                                {t('common.error')}
                            </div>
                            <pre className="text-xs text-red-600 dark:text-red-400 font-mono whitespace-pre-wrap leading-relaxed">{displayLog.error}</pre>
                        </div>
                    )}

                    {(displayLog.request_override_applied || displayLog.request_override_error) && (
                        <div className="overflow-hidden rounded-2xl border border-amber-500/20 bg-amber-500/5 p-4">
                            <div className="mb-3 flex items-center gap-2 text-amber-600 dark:text-amber-400 font-bold text-xs uppercase tracking-wider">
                                <AlertTriangle className="h-4 w-4" />
                                {t('log_detail.request_override', 'Request Override')}
                            </div>
                            {displayLog.request_override_rules?.length ? (
                                <div className="mb-3 flex flex-wrap gap-2">
                                    {displayLog.request_override_rules.map((rule) => (
                                        <Badge key={rule} variant="outline" className="border-amber-500/30 bg-background/60 text-[11px] font-semibold text-foreground">
                                            {rule}
                                        </Badge>
                                    ))}
                                </div>
                            ) : null}
                            {displayLog.request_override_error && (
                                <pre className="text-xs text-amber-700 dark:text-amber-300 font-mono whitespace-pre-wrap leading-relaxed">{displayLog.request_override_error}</pre>
                            )}
                        </div>
                    )}

                    {/* 请求体 & 请求头 */}
                    <div className={cn(sectionCardClassName, "space-y-4")}>
                        <div className="text-[11px] font-bold tracking-widest text-muted-foreground">
                            {t('log_detail.request')}
                        </div>
                        <div className="space-y-2">
                            <SectionHeader
                                title={t('log_detail.request') + ' ' + t('log_detail.body')}
                                section="requestBody"
                                icon={FileCode}
                                extra={
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-bold text-muted-foreground">{formatSize(displayLog.request_body_size)}</span>
                                        {displayLog.truncated && (
                                            <Badge variant="outline" className="h-5 text-[11px] border-yellow-500/40 text-yellow-600 dark:text-yellow-500 bg-yellow-500/5 px-1.5 font-semibold">
                                                {t('log_detail.truncated_tag', 'TRUNCATED')}
                                            </Badge>
                                        )}
                                    </div>
                                }
                            />
                            {expandedSections.requestBody && (
                                <div className="space-y-3">
                                    {displayLog.request_body_ref && (
                                        <BlobPanel
                                            blobRef={displayLog.request_body_ref}
                                            bodySize={displayLog.request_body_size}
                                            contentType={requestContentType}
                                            binary={requestBodyIsBinary}
                                            isLoaded={!!fullRequestBody}
                                            loading={blobLoading.request}
                                            error={blobError}
                                            onLoad={() => loadBlob('request', displayLog.request_body_ref!)}
                                            onUsePreview={() => setFullRequestBody(null)}
                                        />
                                    )}

                                    {effectiveRequestBody && !(requestBodyIsBinary && displayLog.request_body_ref) ? (
                                        <div className={cn(codeCardClassName, "flex max-h-[500px] flex-col")}>
                                            <div className="flex items-center justify-between gap-2 border-b border-border/60 px-2 py-1">
                                                <ViewToggle
                                                    value={requestViewMode}
                                                    options={[
                                                        { value: 'pretty', label: t('log_detail.view_pretty', 'Pretty') },
                                                        { value: 'raw', label: t('log_detail.view_raw', 'Raw') },
                                                        ...(hasRequestBodyDiff
                                                            ? [{ value: 'diff', label: t('log_detail.view_diff', 'Diff') }]
                                                            : []),
                                                    ]}
                                                    onChange={(value) => setRequestViewMode(value as RequestViewMode)}
                                                />
                                                <div className="flex items-center gap-0.5">
                                                    {requestViewMode === 'pretty' && (
                                                        <ExpandToggle mode={requestExpandMode} onChange={setRequestExpandMode} />
                                                    )}
                                                    {hasRequestBodyDiff && (
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    onClick={() => window.open(logRequestDiffPath(displayLog.id), '_blank', 'noopener,noreferrer')}
                                                                    className="h-7 w-7 rounded-md text-muted-foreground hover:bg-primary/10 hover:text-primary"
                                                                    aria-label={t('log_detail.open_diff', 'Open Diff')}
                                                                >
                                                                    <ExternalLink className="h-3.5 w-3.5" />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent side="top" sideOffset={4}>
                                                                {t('log_detail.open_diff', 'Open Diff')}
                                                            </TooltipContent>
                                                        </Tooltip>
                                                    )}
                                                    <CopyButton text={effectiveRequestBody} field="requestBody" />
                                                </div>
                                            </div>
                                            <div className="custom-scrollbar flex-1 overflow-x-auto overflow-y-auto p-4">
                                                {requestViewMode === 'raw' ? (
                                                    <RawBodyViewer text={effectiveRequestBody} />
                                                ) : requestViewMode === 'diff' && hasRequestBodyDiff ? (
                                                    <JsonDiffViewer beforeText={originalRequestBody} afterText={finalRequestBody} />
                                                ) : (
                                                    <JsonViewer data={parsedRequestBody ?? effectiveRequestBody} expandMode={requestExpandMode} />
                                                )}
                                            </div>
                                        </div>
                                    ) : (
                                        <div className={cn(emptyStateClassName, "text-[11px] italic text-muted-foreground")}>
                                            {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>

                        <div className="space-y-2">
                            <SectionHeader
                                title={t('log_detail.request') + ' ' + t('log_detail.headers')}
                                section="requestHeaders"
                                icon={ListTree}
                                extra={<span className="text-xs font-bold text-muted-foreground">{Object.keys(displayLog.request_headers ?? {}).length} KEYS</span>}
                            />
                            {expandedSections.requestHeaders && displayLog.request_headers && (
                                <div className={cn(contentCardClassName, "space-y-2 font-mono text-[11px] leading-relaxed")}>
                                    {Object.entries(displayLog.request_headers).map(([key, vv]) => (
                                        <div key={key} className="flex flex-col sm:flex-row sm:gap-2 group/line">
                                            <span className="text-primary shrink-0 font-bold">{key}:</span>
                                            <div className="flex flex-col">
                                                {vv.map((v, i) => (
                                                    <span key={i} className="text-foreground break-all select-text">{v}{i < vv.length - 1 ? ';' : ''}</span>
                                                ))}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    </div>

                    {/* 响应体 & 响应头 */}
                    <div className={cn(sectionCardClassName, "space-y-4")}>
                        <div className="text-[11px] font-bold tracking-widest text-muted-foreground">
                            {t('log_detail.response')}
                        </div>
                        <div className="space-y-2">
                            <SectionHeader
                                title={t('log_detail.response') + ' ' + t('log_detail.body')}
                                section="responseBody"
                                icon={FileCode}
                                extra={
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-bold text-muted-foreground">{formatSize(displayLog.response_body_size)}</span>
                                        {displayLog.truncated && (
                                            <Badge variant="outline" className="h-5 text-[11px] border-yellow-500/40 text-yellow-600 dark:text-yellow-500 bg-yellow-500/5 px-1.5 font-semibold">
                                                {t('log_detail.truncated_tag', 'TRUNCATED')}
                                            </Badge>
                                        )}
                                    </div>
                                }
                            />
                            {expandedSections.responseBody && (
                                <div className="space-y-3">
                                    {displayLog.streaming && responseViewMode === 'merged' && mergedResponse && (
                                        <div className="flex items-center gap-1.5 px-1 text-[11px] font-mono text-muted-foreground">
                                            <Layers className="h-3 w-3 shrink-0" />
                                            <span>
                                                {t('log_detail.stream_merge_info', { count: mergedResponse.chunks })}
                                                {' · '}
                                                {t('log_detail.stream_merge_format', { format: mergedResponse.format.toUpperCase() })}
                                                {' · '}
                                                {t('log_detail.stream_merge_protocol', { protocol: mergedResponse.protocol.toUpperCase() })}
                                            </span>
                                        </div>
                                    )}

                                    {displayLog.response_body_ref && (
                                        <BlobPanel
                                            blobRef={displayLog.response_body_ref}
                                            bodySize={displayLog.response_body_size}
                                            contentType={responseContentType}
                                            binary={responseBodyIsBinary}
                                            isLoaded={!!fullResponseBody}
                                            loading={blobLoading.response}
                                            error={blobError}
                                            onLoad={() => loadBlob('response', displayLog.response_body_ref!)}
                                            onUsePreview={() => setFullResponseBody(null)}
                                        />
                                    )}

                                    {effectiveResponseBody && !(responseBodyIsBinary && displayLog.response_body_ref) ? (
                                        <div className={cn(codeCardClassName, "flex max-h-[500px] flex-col")}>
                                            <div className="flex items-center justify-between gap-2 border-b border-border/60 px-2 py-1">
                                                <ViewToggle
                                                    value={responseViewMode}
                                                    options={displayLog.streaming
                                                        ? [
                                                            { value: 'raw', label: t('log_detail.view_raw', 'Raw') },
                                                            { value: 'merged', label: t('log_detail.stream_merged', 'Merged') },
                                                        ]
                                                        : [
                                                            { value: 'pretty', label: t('log_detail.view_pretty', 'Pretty') },
                                                            { value: 'raw', label: t('log_detail.view_raw', 'Raw') },
                                                        ]}
                                                    onChange={(value) => setResponseViewMode(value as ResponseViewMode)}
                                                />
                                                <div className="flex items-center gap-0.5">
                                                    {(responseViewMode === 'pretty' || (responseViewMode === 'merged' && mergedResponse)) && (
                                                        <ExpandToggle mode={responseExpandMode} onChange={setResponseExpandMode} />
                                                    )}
                                                    <CopyButton
                                                        text={
                                                            responseViewMode === 'merged' && mergedResponse
                                                                ? JSON.stringify(mergedResponse.merged, null, 2)
                                                                : effectiveResponseBody
                                                        }
                                                        field="responseBody"
                                                    />
                                                </div>
                                            </div>
                                            <div className="custom-scrollbar flex-1 overflow-x-auto overflow-y-auto p-4">
                                                {responseViewMode === 'raw' ? (
                                                    <RawBodyViewer text={effectiveResponseBody} />
                                                ) : responseViewMode === 'merged' ? (
                                                    mergedResponse ? (
                                                        <JsonViewer data={mergedResponse.merged} expandMode={responseExpandMode} />
                                                    ) : (
                                                        <div className={cn(emptyStateClassName, "text-[11px] italic text-muted-foreground")}>
                                                            {t('log_detail.stream_merge_unavailable', '当前无法生成合并视图，请切换到 Raw 查看原始内容。')}
                                                        </div>
                                                    )
                                                ) : (
                                                    <JsonViewer data={parsedResponseBody ?? effectiveResponseBody} expandMode={responseExpandMode} />
                                                )}
                                            </div>
                                        </div>
                                    ) : (
                                        <div className={cn(emptyStateClassName, "text-[11px] italic text-muted-foreground")}>
                                            {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>

                        <div className="space-y-2">
                            <SectionHeader
                                title={t('log_detail.response') + ' ' + t('log_detail.headers')}
                                section="responseHeaders"
                                icon={ListTree}
                                extra={<span className="text-xs font-bold text-muted-foreground">{Object.keys(displayLog.response_headers ?? {}).length} KEYS</span>}
                            />
                            {expandedSections.responseHeaders && displayLog.response_headers && (
                                <div className={cn(contentCardClassName, "space-y-2 font-mono text-[11px] leading-relaxed")}>
                                    {Object.entries(displayLog.response_headers).map(([key, vv]) => (
                                        <div key={key} className="flex flex-col sm:flex-row sm:gap-2 group/line">
                                            <span className="text-green-600 dark:text-green-400 shrink-0 font-bold">{key}:</span>
                                            <div className="flex flex-col">
                                                {vv.map((v, i) => (
                                                    <span key={i} className="text-foreground break-all select-text">{v}{i < vv.length - 1 ? ';' : ''}</span>
                                                ))}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            </SheetContent>
        </Sheet>
    )
}

function shouldSubscribeLive(log: RequestLog): boolean {
    return log.latency_ms === 0 && !log.error
}

function parseLiveEvent(event: MessageEvent<string>): LiveLogEvent | null {
    try {
        const payload = JSON.parse(event.data) as LiveLogEvent
        if (!payload || typeof payload !== 'object' || !('type' in payload)) {
            return null
        }
        return payload
    } catch {
        return null
    }
}

function isBinaryPlaceholder(body: string) {
    return body.trimStart().startsWith('[binary content omitted;')
}

function firstHeaderValue(headers: Record<string, string[]> | undefined, name: string) {
    if (!headers) return ''
    const direct = headers[name]
    if (direct?.length) return direct[0]

    const lowerName = name.toLowerCase()
    for (const [key, values] of Object.entries(headers)) {
        if (key.toLowerCase() === lowerName && values.length) {
            return values[0]
        }
    }
    return ''
}
