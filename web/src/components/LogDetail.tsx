import { cn, formatDate, formatLatency, formatSize, getStatusColor, getMethodColor } from '@/lib/utils'
import { Copy, Check, Zap, AlertTriangle, ChevronDown, ChevronUp, FileCode, ListTree, Globe, Layers, RotateCcw } from 'lucide-react'
import { fetchBlob } from '@/lib/api'
import type { RequestLog } from '@/lib/api'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { JsonViewer } from './JsonViewer'
import { mergeStreamBody } from '@/lib/streamMerge'
import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"

interface LogDetailProps {
    log: RequestLog | null
    loading?: boolean
    onClose: () => void
}

export function LogDetail({ log, loading, onClose }: LogDetailProps) {
    const { t, i18n } = useTranslation()
    const navigate = useNavigate()
    const [copiedField, setCopiedField] = useState<string | null>(null)
    const [fullRequestBody, setFullRequestBody] = useState<string | null>(null)
    const [fullResponseBody, setFullResponseBody] = useState<string | null>(null)
    const [blobLoading, setBlobLoading] = useState<{ request: boolean; response: boolean }>({
        request: false,
        response: false,
    })
    const [blobError, setBlobError] = useState<string | null>(null)
    const [streamMerged, setStreamMerged] = useState(false)
    const [expandedSections, setExpandedSections] = useState({
        requestHeaders: false,
        requestBody: true,
        responseHeaders: false,
        responseBody: true,
    })

    useEffect(() => {
        setFullRequestBody(null)
        setFullResponseBody(null)
        setBlobError(null)
        setBlobLoading({ request: false, response: false })
        setStreamMerged(false)
    }, [log?.id])

    const effectiveRequestBody = fullRequestBody ?? log?.request_body ?? ''
    const effectiveResponseBody = fullResponseBody ?? log?.response_body ?? ''

    const parsedRequestBody = useMemo(() => {
        if (!effectiveRequestBody) return null
        try {
            return JSON.parse(effectiveRequestBody)
        } catch {
            return null
        }
    }, [effectiveRequestBody])

    const parsedResponseBody = useMemo(() => {
        if (!effectiveResponseBody) return null
        try {
            return JSON.parse(effectiveResponseBody)
        } catch {
            return null
        }
    }, [effectiveResponseBody])

    const mergedResponse = useMemo(() => {
        if (!log?.streaming || !effectiveResponseBody) return null
        return mergeStreamBody(effectiveResponseBody)
    }, [log?.streaming, effectiveResponseBody])


    const copyToClipboard = async (text: string, field: string) => {
        await navigator.clipboard.writeText(text)
        setCopiedField(field)
        setTimeout(() => setCopiedField(null), 2000)
    }

    const toggleSection = (section: keyof typeof expandedSections) => {
        setExpandedSections(prev => ({ ...prev, [section]: !prev[section] }))
    }

    const loadBlob = async (kind: 'request' | 'response', ref: string) => {
        setBlobError(null)
        setBlobLoading(prev => ({ ...prev, [kind]: true }))
        try {
            const body = await fetchBlob(ref)
            if (kind === 'request') setFullRequestBody(body)
            else setFullResponseBody(body)
        } catch (err: any) {
            setBlobError(err?.message || 'Failed to load blob')
        } finally {
            setBlobLoading(prev => ({ ...prev, [kind]: false }))
        }
    }

    if (!log) return null

    const CopyButton = ({ text, field }: { text: string; field: string }) => (
        <Button
            variant="ghost"
            size="icon"
            onClick={(e) => {
                e.stopPropagation()
                copyToClipboard(text, field)
            }}
            className="h-7 w-7 rounded-md hover:bg-primary/10 hover:text-primary transition-all"
            title={t('common.copy', '复制')}
        >
            {copiedField === field ? (
                <Check className="h-3.5 w-3.5 text-green-500" />
            ) : (
                <Copy className="h-3.5 w-3.5 text-muted-foreground/50" />
            )}
        </Button>
    )

    const SectionHeader = ({
        title,
        section,
        icon: Icon,
        extra,
    }: {
        title: string
        section: keyof typeof expandedSections
        icon: any
        extra?: React.ReactNode
    }) => (
        <button
            onClick={() => toggleSection(section)}
            className="flex items-center justify-between w-full py-2.5 group transition-colors"
        >
            <div className="flex items-center gap-2">
                <div className={cn(
                    "p-1.5 rounded-md transition-colors",
                    expandedSections[section] ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground group-hover:bg-muted/80"
                )}>
                    <Icon className="h-3.5 w-3.5" />
                </div>
                <span className="text-xs font-bold uppercase tracking-wider text-foreground/80 group-hover:text-foreground">
                    {title}
                </span>
                {expandedSections[section] ? (
                    <ChevronUp className="h-3.5 w-3.5 text-muted-foreground/40" />
                ) : (
                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground/40" />
                )}
            </div>
            {extra}
        </button>
    )

    return (
        <Sheet open={!!log} onOpenChange={(open) => !open && onClose()}>
            <SheetContent className="sm:max-w-2xl w-full p-0 flex flex-col border-l border-border/40 sm:rounded-l-2xl shadow-2xl backdrop-blur-xl bg-white dark:bg-card/95">
                {/* 头部固定区域 */}
                <SheetHeader className="px-6 py-5 border-b border-border/40 bg-muted/20">
                    <div className="flex items-center gap-3">
                        <div
                            className={cn(
                                "w-14 py-0.5 rounded-[3px] text-[10px] text-center uppercase font-bold border",
                                getMethodColor(log.method)
                            )}
                        >
                            {log.method}
                        </div>
                        <SheetTitle className={cn(
                            "font-mono text-xl font-black tracking-tighter",
                            getStatusColor(log.status_code)
                        )}>
                            {log.status_code || '---'}
                        </SheetTitle>
                        {log.streaming && (
                            <Badge variant="secondary" className="bg-purple-500/10 text-purple-500 border-none font-bold text-[10px] animate-pulse">
                                <Zap className="h-3 w-3 mr-1 fill-current" />
                                {t('log_detail.streaming', 'STREAMING')}
                            </Badge>
                        )}
                        {log.error && (
                            <Badge variant="destructive" className="bg-red-500/10 text-red-500 border-none font-bold text-[10px]">
                                <AlertTriangle className="h-3 w-3 mr-1" />
                                {t('common.error', 'ERROR')}
                            </Badge>
                        )}
                        {loading && (
                            <div className="ml-auto flex items-center gap-2 text-[10px] font-black uppercase text-primary animate-pulse">
                                <div className="h-1 w-1 rounded-full bg-current" />
                                {t('common.loading')}
                            </div>
                        )}
                        {!loading && (
                            <Button
                                variant="outline"
                                size="sm"
                                className="ml-auto h-7 px-2.5 text-[11px] font-semibold gap-1.5 border-primary/20 bg-primary/5 hover:bg-primary/10 hover:text-primary hover:border-primary/30 transition-all shadow-sm"
                                onClick={async () => {
                                    const navigateToPlayground = (body: string) => {
                                        onClose()
                                        navigate('/playground', {
                                            state: {
                                                replay: {
                                                    upstream: log.upstream,
                                                    method: log.method,
                                                    path: log.path + (log.query ? '?' + log.query : ''),
                                                    headers: log.request_headers,
                                                    body,
                                                },
                                            },
                                        })
                                    }

                                    // If blob ref exists and not yet loaded, fetch full body first
                                    if (log.request_body_ref && !fullRequestBody) {
                                        try {
                                            const full = await fetchBlob(log.request_body_ref)
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
                        )}
                    </div>
                </SheetHeader>

                {/* 主内容区域 */}
                <div className="flex-1 overflow-y-auto px-6 py-6 space-y-8 custom-scrollbar">
                    {/* 基本信息网格 */}
                    <div className="grid grid-cols-2 sm:grid-cols-4 gap-6 bg-muted/30 p-4 rounded-lg border border-border/30">
                        {[
                            { label: t('log_table.upstream'), value: log.upstream, mono: false },
                            { label: t('log_table.latency'), value: formatLatency(log.latency_ms), mono: true },
                            { label: t('log_table.time'), value: formatDate(log.created_at, i18n.language), mono: false },
                            { label: 'ID', value: log.id.substring(0, 8) + '...', mono: true, full: log.id }
                        ].map((item, idx) => (
                            <div key={idx} className="space-y-1">
                                <div className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider">{item.label}</div>
                                <div className={cn(
                                    "text-sm font-bold truncate",
                                    item.mono ? "font-mono" : ""
                                )} title={item.full}>
                                    {item.value}
                                </div>
                            </div>
                        ))}
                    </div>

                    {/* URL 地址 */}
                    <div className="space-y-3">
                        <SectionHeader title={t('log_detail.url')} section="requestHeaders" icon={Globe} />
                        <div className="flex items-center gap-2 p-3.5 rounded-lg bg-slate-50 dark:bg-background/50 border border-border/40 group hover:border-primary/30 transition-all">
                            <code className="flex-1 text-xs font-mono break-all leading-relaxed text-foreground/80">{log.target_url}</code>
                            <CopyButton text={log.target_url} field="url" />
                        </div>
                    </div>

                    {/* 错误详情 */}
                    {log.error && (
                        <div className="p-4 rounded-xl bg-red-500/5 border border-red-500/20 overflow-hidden">
                            <div className="flex items-center gap-2 text-red-500 font-bold text-xs uppercase tracking-wider mb-3">
                                <AlertTriangle className="h-4 w-4" />
                                {t('common.error')}
                            </div>
                            <pre className="text-xs text-red-600 dark:text-red-400 font-mono whitespace-pre-wrap leading-relaxed">{log.error}</pre>
                        </div>
                    )}

                    {/* 请求头 & 请求体 */}
                    <div className="space-y-4">
                        <div className="space-y-1">
                            <SectionHeader
                                title={t('log_detail.request') + ' ' + t('log_detail.headers')}
                                section="requestHeaders"
                                icon={ListTree}
                                extra={<span className="text-xs font-bold text-muted-foreground/70">{Object.keys(log.request_headers ?? {}).length} KEYS</span>}
                            />
                            {expandedSections.requestHeaders && log.request_headers && (
                                <div className="p-4 rounded-lg bg-slate-50 dark:bg-background/50 border border-border/40 space-y-2 font-mono text-[11px] leading-relaxed">
                                    {Object.entries(log.request_headers).map(([key, value]) => (
                                        <div key={key} className="flex group/line">
                                            <span className="text-primary/70 shrink-0 font-bold">{key}:</span>
                                            <span className="ml-2 text-foreground/70 break-all select-text">{value}</span>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>

                        <div className="space-y-1">
                            <SectionHeader
                                title={t('log_detail.request') + ' ' + t('log_detail.body')}
                                section="requestBody"
                                icon={FileCode}
                                extra={
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-bold text-muted-foreground/70">{formatSize(log.request_body_size)}</span>
                                        {log.request_body_ref && (
                                            <Badge variant="outline" className="h-5 text-[10px] border-indigo-500/40 text-indigo-600 dark:text-indigo-400 bg-indigo-500/5 px-1.5 font-bold">
                                                {t('log_detail.detached_tag', 'DETACHED')}
                                            </Badge>
                                        )}
                                        {log.truncated && (
                                            <Badge variant="outline" className="h-5 text-[10px] border-yellow-500/40 text-yellow-600 dark:text-yellow-500 bg-yellow-500/5 px-1.5 font-bold">
                                                {t('log_detail.truncated_tag', 'TRUNCATED')}
                                            </Badge>
                                        )}
                                    </div>
                                }
                            />
                            {expandedSections.requestBody && (
                                <div className="space-y-2">
                                    {log.request_body_ref && (
                                        <div className="p-3 rounded-lg bg-indigo-500/5 border border-indigo-500/20">
                                            <div className="flex items-center justify-between gap-3">
                                                <div className="text-xs font-bold text-indigo-600 dark:text-indigo-400">
                                                    {fullRequestBody ? t('log_detail.blob_loaded') : t('log_detail.blob_detached')}
                                                </div>
                                                <div className="flex items-center gap-3">
                                                    {fullRequestBody ? (
                                                        <Button
                                                            variant="outline"
                                                            size="sm"
                                                            onClick={() => setFullRequestBody(null)}
                                                            className="h-7 px-2 text-[11px] font-bold"
                                                        >
                                                            {t('log_detail.use_preview')}
                                                        </Button>
                                                    ) : (
                                                        <Button
                                                            variant="outline"
                                                            size="sm"
                                                            onClick={() => loadBlob('request', log.request_body_ref!)}
                                                            disabled={blobLoading.request}
                                                            className="h-7 px-2 text-[11px] font-bold border-indigo-500/30 text-indigo-600 hover:bg-indigo-50"
                                                        >
                                                            {blobLoading.request ? t('common.loading') : t('log_detail.load_full')}
                                                        </Button>
                                                    )}
                                                    <a
                                                        href={`/api/blobs/${encodeURIComponent(log.request_body_ref)}`}
                                                        target="_blank"
                                                        rel="noreferrer"
                                                        className="text-[11px] font-bold text-indigo-600 dark:text-indigo-400 hover:text-indigo-500 underline decoration-indigo-500/30 underline-offset-4"
                                                    >
                                                        {t('log_detail.open_raw')}
                                                    </a>
                                                </div>
                                            </div>
                                            <code className="block mt-2 text-[11px] font-mono break-all text-muted-foreground">
                                                {log.request_body_ref}
                                            </code>
                                            {blobError && (
                                                <div className="mt-2 text-[11px] text-red-500/80 font-mono">
                                                    {blobError}
                                                </div>
                                            )}
                                        </div>
                                    )}

                                    <div className="relative group">
                                        {effectiveRequestBody ? (
                                            <>
                                                {effectiveRequestBody ? (
                                                    <div className="p-4 rounded-lg bg-slate-50 dark:bg-background/50 border border-border/40 overflow-x-auto max-h-[500px] overflow-y-auto custom-scrollbar relative group">
                                                        <JsonViewer data={parsedRequestBody ?? effectiveRequestBody} />
                                                        <div className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity z-10">
                                                            <CopyButton text={effectiveRequestBody} field="requestBody" />
                                                        </div>
                                                    </div>
                                                ) : (
                                                    <div className="text-[11px] text-muted-foreground/40 italic p-4 border border-dashed border-border/30 rounded-xl text-center">
                                                        {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                                    </div>
                                                )}
                                            </>
                                        ) : (
                                            <div className="text-[11px] text-muted-foreground/40 italic p-4 border border-dashed border-border/30 rounded-xl text-center">
                                                {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>

                    <Separator className="bg-border/20" />

                    {/* 响应头 & 响应体 */}
                    <div className="space-y-4">
                        <div className="space-y-1">
                            <SectionHeader
                                title={t('log_detail.response') + ' ' + t('log_detail.headers')}
                                section="responseHeaders"
                                icon={ListTree}
                                extra={<span className="text-xs font-bold text-muted-foreground/70">{Object.keys(log.response_headers ?? {}).length} KEYS</span>}
                            />
                            {expandedSections.responseHeaders && log.response_headers && (
                                <div className="p-4 rounded-xl bg-background/50 border border-border/40 space-y-2 font-mono text-[11px] leading-relaxed">
                                    {Object.entries(log.response_headers).map(([key, value]) => (
                                        <div key={key} className="flex group/line">
                                            <span className="text-green-500/70 shrink-0 font-bold">{key}:</span>
                                            <span className="ml-2 text-foreground/70 break-all select-text">{value}</span>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>

                        <div className="space-y-1">
                            <SectionHeader
                                title={t('log_detail.response') + ' ' + t('log_detail.body')}
                                section="responseBody"
                                icon={FileCode}
                                extra={
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-bold text-muted-foreground/70">{formatSize(log.response_body_size)}</span>
                                        {log.response_body_ref && (
                                            <Badge variant="outline" className="h-5 text-[10px] border-indigo-500/40 text-indigo-600 dark:text-indigo-400 bg-indigo-500/5 px-1.5 font-bold">
                                                {t('log_detail.detached_tag', 'DETACHED')}
                                            </Badge>
                                        )}
                                        {log.truncated && (
                                            <Badge variant="outline" className="h-5 text-[10px] border-yellow-500/40 text-yellow-600 dark:text-yellow-500 bg-yellow-500/5 px-1.5 font-bold">
                                                {t('log_detail.truncated_tag', 'TRUNCATED')}
                                            </Badge>
                                        )}
                                    </div>
                                }
                            />
                            {expandedSections.responseBody && (
                                <div className="space-y-2">
                                    {/* 流式合并切换 */}
                                    {log.streaming && mergedResponse && (
                                        <div className="flex items-center gap-2">
                                            <Button
                                                variant={streamMerged ? 'default' : 'outline'}
                                                size="sm"
                                                onClick={() => setStreamMerged(true)}
                                                className={cn(
                                                    "h-7 px-2.5 text-[11px] font-bold gap-1.5 transition-all",
                                                    streamMerged && "bg-purple-600 hover:bg-purple-700 text-white shadow-sm"
                                                )}
                                            >
                                                <Layers className="h-3 w-3" />
                                                {t('log_detail.stream_merged')}
                                            </Button>
                                            <Button
                                                variant={!streamMerged ? 'default' : 'outline'}
                                                size="sm"
                                                onClick={() => setStreamMerged(false)}
                                                className={cn(
                                                    "h-7 px-2.5 text-[11px] font-bold gap-1.5 transition-all",
                                                    !streamMerged && "bg-slate-700 hover:bg-slate-800 text-white shadow-sm"
                                                )}
                                            >
                                                <FileCode className="h-3 w-3" />
                                                {t('log_detail.stream_raw')}
                                            </Button>
                                            {streamMerged && (
                                                <span className="text-[10px] font-mono text-muted-foreground/60 ml-1">
                                                    {t('log_detail.stream_merge_info', { count: mergedResponse.chunks })}
                                                    {' · '}
                                                    {t('log_detail.stream_merge_format', { format: mergedResponse.format.toUpperCase() })}
                                                </span>
                                            )}
                                        </div>
                                    )}

                                    {log.response_body_ref && (
                                        <div className="p-3 rounded-lg bg-indigo-500/5 border border-indigo-500/20">
                                            <div className="flex items-center justify-between gap-3">
                                                <div className="text-xs font-bold text-indigo-600 dark:text-indigo-400">
                                                    {fullResponseBody ? t('log_detail.blob_loaded') : t('log_detail.blob_detached')}
                                                </div>
                                                <div className="flex items-center gap-3">
                                                    {fullResponseBody ? (
                                                        <Button
                                                            variant="outline"
                                                            size="sm"
                                                            onClick={() => setFullResponseBody(null)}
                                                            className="h-7 px-2 text-[11px] font-bold"
                                                        >
                                                            {t('log_detail.use_preview')}
                                                        </Button>
                                                    ) : (
                                                        <Button
                                                            variant="outline"
                                                            size="sm"
                                                            onClick={() => loadBlob('response', log.response_body_ref!)}
                                                            disabled={blobLoading.response}
                                                            className="h-7 px-2 text-[11px] font-bold border-indigo-500/30 text-indigo-600 hover:bg-indigo-50"
                                                        >
                                                            {blobLoading.response ? t('common.loading') : t('log_detail.load_full')}
                                                        </Button>
                                                    )}
                                                    <a
                                                        href={`/api/blobs/${encodeURIComponent(log.response_body_ref)}`}
                                                        target="_blank"
                                                        rel="noreferrer"
                                                        className="text-[11px] font-bold text-indigo-600 dark:text-indigo-400 hover:text-indigo-500 underline decoration-indigo-500/30 underline-offset-4"
                                                    >
                                                        {t('log_detail.open_raw')}
                                                    </a>
                                                </div>
                                            </div>
                                            <code className="block mt-2 text-[11px] font-mono break-all text-muted-foreground">
                                                {log.response_body_ref}
                                            </code>
                                            {blobError && (
                                                <div className="mt-2 text-[11px] text-red-500/80 font-mono">
                                                    {blobError}
                                                </div>
                                            )}
                                        </div>
                                    )}

                                    <div className="relative group">
                                        {effectiveResponseBody ? (
                                            <div className="p-4 rounded-lg bg-slate-50 dark:bg-background/50 border border-border/40 overflow-x-auto max-h-[500px] overflow-y-auto custom-scrollbar relative group">
                                                <JsonViewer data={
                                                    streamMerged && mergedResponse
                                                        ? mergedResponse.merged
                                                        : (parsedResponseBody ?? effectiveResponseBody)
                                                } />
                                                <div className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity z-10">
                                                    <CopyButton
                                                        text={
                                                            streamMerged && mergedResponse
                                                                ? JSON.stringify(mergedResponse.merged, null, 2)
                                                                : effectiveResponseBody
                                                        }
                                                        field="responseBody"
                                                    />
                                                </div>
                                            </div>
                                        ) : (
                                            <div className="text-[11px] text-muted-foreground/40 italic p-4 border border-dashed border-border/30 rounded-xl text-center">
                                                {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            </SheetContent>
        </Sheet>
    )
}

