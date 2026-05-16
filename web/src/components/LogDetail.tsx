import { cn, formatDate, formatLatency, formatSize, getStatusColor, getMethodColor } from '@/lib/utils'
import { Copy, Check, Zap, AlertTriangle, ChevronDown, ChevronUp, ChevronsDownUp, ChevronsUpDown, FileCode, ListTree, Globe, Layers, RotateCcw, Maximize2, Minimize2, ExternalLink, Terminal, Bookmark, BookmarkCheck, CheckCircle2, CircleDot, Tags, Search, X } from 'lucide-react'
import { fetchBlob, updateLogAnnotation } from '@/lib/api'
import type { LiveLogEvent, RequestLog } from '@/lib/api'
import { startTransition, useCallback, useEffect, useMemo, useRef, useState, type ComponentType, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { JsonViewer, type JsonExpandMode, HighlightText, countJsonSearchMatches } from './JsonViewer'
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
    onLogChange?: (log: RequestLog) => void
}

type BodyViewMode = 'pretty' | 'raw'
type RequestViewMode = BodyViewMode | 'diff'
type ResponseViewMode = BodyViewMode | 'merged'
type PanelWidthMode = 'wide' | 'full'

const logDetailWidthStorageKey = 'prismcat.logDetail.width'
const logDetailExpandedStorageKey = 'prismcat.logDetail.expanded'
const autoLoadFullBodyLimit = 10 * 1024 * 1024

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

function countTextMatches(text: string, term: string): number {
    if (!term || !text) return 0;
    const lower = text.toLowerCase();
    const lowerTerm = term.toLowerCase();
    let count = 0, idx = 0;
    while ((idx = lower.indexOf(lowerTerm, idx)) !== -1) { count++; idx += lowerTerm.length; }
    return count;
}

function navigateSearchMatch(container: HTMLElement | null, direction: 'prev' | 'next') {
    if (!container) return;
    const matches = Array.from(container.querySelectorAll<HTMLElement>('[data-search-match]'));
    if (!matches.length) return;
    const prev = container.querySelector<HTMLElement>('[data-search-match-active]');
    if (prev) prev.removeAttribute('data-search-match-active');
    const currentIdx = prev ? matches.indexOf(prev) : -1;
    let nextIdx: number;
    if (direction === 'next') {
        nextIdx = currentIdx + 1 >= matches.length ? 0 : currentIdx + 1;
    } else {
        nextIdx = currentIdx - 1 < 0 ? matches.length - 1 : currentIdx - 1;
    }
    matches[nextIdx].setAttribute('data-search-match-active', '');
    matches[nextIdx].scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

function RawBodyViewer({ text, searchTerm }: { text: string; searchTerm?: string }) {
    return (
        <pre className="whitespace-pre-wrap break-all text-[11px] font-mono leading-relaxed text-foreground select-text">
            {searchTerm ? <HighlightText text={text} searchTerm={searchTerm} /> : text}
        </pre>
    );
}

function BodySearchBar({
    searchTerm,
    onSearchTermChange,
    matchCount,
    onNavigate,
    onClose,
}: {
    searchTerm: string;
    onSearchTermChange: (term: string) => void;
    matchCount: number;
    onNavigate: (dir: 'prev' | 'next') => void;
    onClose: () => void;
}) {
    const { t } = useTranslation();
    return (
        <div className="flex items-center gap-2 border-b border-border/60 px-3 py-1.5 bg-muted/20">
            <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <input
                autoFocus
                value={searchTerm}
                onChange={(e) => onSearchTermChange(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Enter') { e.preventDefault(); onNavigate(e.shiftKey ? 'prev' : 'next'); }
                    if (e.key === 'Escape') { e.preventDefault(); onClose(); }
                }}
                placeholder={t('body_search.placeholder', 'Search in body...')}
                className="flex-1 min-w-0 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
            />
            {searchTerm && (
                <span className="text-[10px] font-bold text-muted-foreground whitespace-nowrap">
                    {matchCount > 0
                        ? t('body_search.match_count', { count: matchCount })
                        : t('body_search.no_matches', 'No matches')
                    }
                </span>
            )}
            <div className="flex items-center">
                <button
                    type="button"
                    onClick={() => onNavigate('prev')}
                    disabled={!searchTerm || matchCount === 0}
                    className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                    <ChevronUp className="h-3 w-3" />
                </button>
                <button
                    type="button"
                    onClick={() => onNavigate('next')}
                    disabled={!searchTerm || matchCount === 0}
                    className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                    <ChevronDown className="h-3 w-3" />
                </button>
            </div>
            <button
                type="button"
                onClick={onClose}
                className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
                <X className="h-3 w-3" />
            </button>
        </div>
    );
}

export function LogDetail({ log, loading, onClose, onLogChange }: LogDetailProps) {
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
    const [previewOnly, setPreviewOnly] = useState<{ request: boolean; response: boolean }>({
        request: false,
        response: false,
    })
    const [expandedSections, setExpandedSections] = useState(getInitialExpandedSections)
    const [requestViewMode, setRequestViewMode] = useState<RequestViewMode>('pretty')
    const [responseViewMode, setResponseViewMode] = useState<ResponseViewMode>('pretty')
    const [requestExpandMode, setRequestExpandMode] = useState<JsonExpandMode>('default')
    const [responseExpandMode, setResponseExpandMode] = useState<JsonExpandMode>('default')
    const [panelWidthMode, setPanelWidthMode] = useState<PanelWidthMode>(() => getInitialPanelWidthMode())
    const [annotationSaving, setAnnotationSaving] = useState(false)
    const [annotationNote, setAnnotationNote] = useState('')
    const [annotationLabels, setAnnotationLabels] = useState('')
    const [annotationPanelOpen, setAnnotationPanelOpen] = useState(false)
    const [idTooltipOpen, setIdTooltipOpen] = useState(false)
    const [requestSearchOpen, setRequestSearchOpen] = useState(false)
    const [requestSearchTerm, setRequestSearchTerm] = useState('')
    const [responseSearchOpen, setResponseSearchOpen] = useState(false)
    const [responseSearchTerm, setResponseSearchTerm] = useState('')
    const requestBodyRef = useRef<HTMLDivElement>(null)
    const responseBodyRef = useRef<HTMLDivElement>(null)
    const displayLog = liveLog ?? log

    useEffect(() => {
        setLiveLog(null)
        setFullRequestBody(null)
        setFullResponseBody(null)
        setBlobError(null)
        setBlobLoading({ request: false, response: false })
        setPreviewOnly({ request: false, response: false })
        setRequestViewMode('pretty')
        setResponseViewMode('pretty')
        setRequestExpandMode('default')
        setResponseExpandMode('default')
        setAnnotationPanelOpen(false)
        setIdTooltipOpen(false)
        setRequestSearchOpen(false)
        setRequestSearchTerm('')
        setResponseSearchOpen(false)
        setResponseSearchTerm('')
    }, [log?.id])

    useEffect(() => {
        setAnnotationNote(displayLog?.annotation?.note ?? '')
        setAnnotationLabels((displayLog?.annotation?.labels ?? []).join(', '))
    }, [displayLog?.id, displayLog?.annotation?.note, displayLog?.annotation?.labels])

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
    const annotation = normalizedAnnotation(displayLog?.annotation)
    const draftLabels = parseLabelDraft(annotationLabels)
    const annotationChanged = annotationNote.trim() !== (annotation.note ?? '') ||
        draftLabels.join('\n') !== (annotation.labels ?? []).join('\n')
    const annotationSummary = annotation.note || annotation.labels.length
        ? [annotation.note, ...annotation.labels.map(label => `#${label}`)].filter(Boolean).join(' · ')
        : t('log_annotation.empty_summary', 'No note or labels')

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

    const requestSearchMatchCount = useMemo(() => {
        if (!requestSearchTerm) return 0
        if (requestViewMode === 'raw' || requestViewMode === 'diff') return countTextMatches(effectiveRequestBody, requestSearchTerm)
        return parsedRequestBody
            ? countJsonSearchMatches(parsedRequestBody, requestSearchTerm)
            : countTextMatches(effectiveRequestBody, requestSearchTerm)
    }, [requestSearchTerm, requestViewMode, effectiveRequestBody, parsedRequestBody])

    const responseSearchMatchCount = useMemo(() => {
        if (!responseSearchTerm) return 0
        if (responseViewMode === 'raw') return countTextMatches(effectiveResponseBody, responseSearchTerm)
        if (responseViewMode === 'merged' && mergedResponse) return countJsonSearchMatches(mergedResponse.merged, responseSearchTerm)
        return parsedResponseBody
            ? countJsonSearchMatches(parsedResponseBody, responseSearchTerm)
            : countTextMatches(effectiveResponseBody, responseSearchTerm)
    }, [responseSearchTerm, responseViewMode, effectiveResponseBody, parsedResponseBody, mergedResponse])

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

    const loadBlob = useCallback(async (kind: 'request' | 'response', ref: string) => {
        setBlobError(null)
        setPreviewOnly(prev => ({ ...prev, [kind]: false }))
        setBlobLoading(prev => ({ ...prev, [kind]: true }))
        try {
            const body = await fetchBlob(ref)
            startTransition(() => {
                if (kind === 'request') setFullRequestBody(body)
                else setFullResponseBody(body)
            })
        } catch (err: unknown) {
            setBlobError(err instanceof Error ? err.message : 'Failed to load blob')
            setPreviewOnly(prev => ({ ...prev, [kind]: true }))
        } finally {
            setBlobLoading(prev => ({ ...prev, [kind]: false }))
        }
    }, [])

    const saveAnnotation = useCallback(async (update: Parameters<typeof updateLogAnnotation>[1]) => {
        if (!displayLog || annotationSaving) return
        setAnnotationSaving(true)
        try {
            const nextAnnotation = await updateLogAnnotation(displayLog.id, update)
            const nextLog = { ...displayLog, annotation: nextAnnotation }
            startTransition(() => {
                setLiveLog(current => current?.id === nextLog.id ? { ...current, annotation: nextAnnotation } : current)
                onLogChange?.(nextLog)
            })
        } catch (err) {
            console.error('Failed to save log annotation:', err)
        } finally {
            setAnnotationSaving(false)
        }
    }, [annotationSaving, displayLog, onLogChange])

    useEffect(() => {
        if (!displayLog) return

        if (shouldAutoLoadBody({
            blobRef: displayLog.request_body_ref,
            bodySize: displayLog.request_body_size,
            contentType: requestContentType,
            previewBody: displayLog.request_body ?? '',
            fullBody: fullRequestBody,
            loading: blobLoading.request,
            previewOnly: previewOnly.request,
        })) {
            loadBlob('request', displayLog.request_body_ref!)
        }

        if (shouldAutoLoadBody({
            blobRef: displayLog.response_body_ref,
            bodySize: displayLog.response_body_size,
            contentType: responseContentType,
            previewBody: displayLog.response_body ?? '',
            fullBody: fullResponseBody,
            loading: blobLoading.response,
            previewOnly: previewOnly.response,
        })) {
            loadBlob('response', displayLog.response_body_ref!)
        }
    }, [
        displayLog,
        requestContentType,
        responseContentType,
        fullRequestBody,
        fullResponseBody,
        blobLoading.request,
        blobLoading.response,
        previewOnly.request,
        previewOnly.response,
        loadBlob,
    ])

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

    const SearchToggle = ({ active, onClick }: { active: boolean; onClick: () => void }) => (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={onClick}
                    className={cn(
                        "h-7 w-7 rounded-md transition-all",
                        active ? "bg-primary/10 text-primary hover:bg-primary/15" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    )}
                    aria-label={t('body_search.search', 'Search')}
                >
                    <Search className="h-3.5 w-3.5" />
                </Button>
            </TooltipTrigger>
            <TooltipContent side="top" sideOffset={4}>{t('body_search.search', 'Search')}</TooltipContent>
        </Tooltip>
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
                <SheetHeader className="border-b border-border/60 bg-card px-5 py-3.5">
                    <div className="flex flex-wrap items-center gap-2.5">
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
                        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1 text-xs font-semibold text-muted-foreground">
                            <span className="truncate text-foreground">{displayLog.upstream}</span>
                            <span className="text-border">/</span>
                            <span className="font-mono">{formatLatency(displayLog.latency_ms)}</span>
                            <span className="text-border">/</span>
                            <span>{formatDate(displayLog.created_at, i18n.language)}</span>
                            <span className="text-border">/</span>
                            <Tooltip open={idTooltipOpen}>
                                <TooltipTrigger asChild>
                                    <button
                                        type="button"
                                        onClick={() => copyToClipboard(displayLog.id, 'id')}
                                        onMouseEnter={() => setIdTooltipOpen(true)}
                                        onMouseLeave={() => setIdTooltipOpen(false)}
                                        onBlur={() => setIdTooltipOpen(false)}
                                        className="rounded px-1 py-0.5 font-mono transition-colors hover:bg-muted hover:text-foreground"
                                        aria-label={t('log_detail.copy_id', 'Copy log ID')}
                                    >
                                        {copiedField === 'id' ? (
                                            <span className="text-green-500">{t('common.copied', 'Copied')}</span>
                                        ) : (
                                            `${displayLog.id.substring(0, 8)}...`
                                        )}
                                    </button>
                                </TooltipTrigger>
                                <TooltipContent side="bottom" sideOffset={6} className="max-w-[420px] break-all font-mono">
                                    {copiedField === 'id' ? t('common.copied', 'Copied') : displayLog.id}
                                </TooltipContent>
                            </Tooltip>
                        </div>
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
                                                        target_url: displayLog.target_url,
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
                <div className="custom-scrollbar flex-1 space-y-4 overflow-y-auto bg-muted/30 px-5 py-4">
                    <div className="rounded-xl border border-border/60 bg-card px-3 py-2.5">
                        <div className="flex flex-wrap items-center gap-2">
                            <Button
                                type="button"
                                variant={annotation.saved ? 'default' : 'outline'}
                                size="sm"
                                disabled={annotationSaving}
                                onClick={() => saveAnnotation({
                                    saved: !annotation.saved,
                                    status: annotation.saved ? 'none' : annotation.status,
                                })}
                                className="h-8 gap-1.5 rounded-lg px-3 text-xs font-semibold"
                            >
                                {annotation.saved ? <BookmarkCheck className="h-3.5 w-3.5" /> : <Bookmark className="h-3.5 w-3.5" />}
                                {annotation.saved ? t('log_annotation.saved', '已保存') : t('log_annotation.save', '保存')}
                            </Button>
                            <Button
                                type="button"
                                variant={annotation.status === 'todo' ? 'secondary' : 'outline'}
                                size="sm"
                                disabled={annotationSaving}
                                onClick={() => saveAnnotation({
                                    saved: true,
                                    status: annotation.status === 'todo' ? 'none' : 'todo',
                                })}
                                className={cn(
                                    "h-8 gap-1.5 rounded-lg px-3 text-xs font-semibold",
                                    annotation.status === 'todo' && "bg-amber-500/10 text-amber-700 hover:bg-amber-500/15 dark:text-amber-300"
                                )}
                            >
                                <CircleDot className="h-3.5 w-3.5" />
                                {t('log_annotation.todo', '待处理')}
                            </Button>
                            <Button
                                type="button"
                                variant={annotation.status === 'done' ? 'secondary' : 'outline'}
                                size="sm"
                                disabled={annotationSaving}
                                onClick={() => saveAnnotation({
                                    saved: true,
                                    status: annotation.status === 'done' ? 'none' : 'done',
                                })}
                                className={cn(
                                    "h-8 gap-1.5 rounded-lg px-3 text-xs font-semibold",
                                    annotation.status === 'done' && "bg-green-500/10 text-green-700 hover:bg-green-500/15 dark:text-green-300"
                                )}
                            >
                                <CheckCircle2 className="h-3.5 w-3.5" />
                                {t('log_annotation.done', '已处理')}
                            </Button>
                            {annotationSaving && (
                                <span className="text-[11px] font-medium text-muted-foreground">{t('common.loading')}</span>
                            )}
                            <button
                                type="button"
                                onClick={() => setAnnotationPanelOpen(current => !current)}
                                className="ml-auto flex min-w-0 items-center gap-2 rounded-lg px-2 py-1 text-left text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                            >
                                <Tags className="h-3.5 w-3.5 shrink-0" />
                                <span className="max-w-[360px] truncate">{annotationSummary}</span>
                                {annotationPanelOpen ? (
                                    <ChevronUp className="h-3.5 w-3.5 shrink-0" />
                                ) : (
                                    <ChevronDown className="h-3.5 w-3.5 shrink-0" />
                                )}
                            </button>
                        </div>

                        {annotationPanelOpen && (
                            <div className="mt-3 grid gap-3 lg:grid-cols-[1fr_280px]">
                                <textarea
                                    value={annotationNote}
                                    onChange={(event) => setAnnotationNote(event.target.value)}
                                    placeholder={t('log_annotation.note_placeholder', '写下为什么保存这条日志，或后续要检查什么...')}
                                    className="min-h-20 resize-y rounded-lg border border-border/60 bg-muted/40 px-3 py-2 text-sm leading-relaxed outline-none transition-colors placeholder:text-muted-foreground/70 focus:border-primary/40 focus:bg-background"
                                />
                                <div className="space-y-2">
                                    <div className="relative">
                                        <Tags className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                                        <input
                                            value={annotationLabels}
                                            onChange={(event) => setAnnotationLabels(event.target.value)}
                                            placeholder={t('log_annotation.labels_placeholder', '标签，用逗号分隔')}
                                            className="h-10 w-full rounded-lg border border-border/60 bg-muted/40 pl-9 pr-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/70 focus:border-primary/40 focus:bg-background"
                                        />
                                    </div>
                                    <Button
                                        type="button"
                                        size="sm"
                                        disabled={annotationSaving || !annotationChanged}
                                        onClick={() => saveAnnotation({ note: annotationNote, labels: draftLabels })}
                                        className="h-8 w-full rounded-lg text-xs font-semibold"
                                    >
                                        <Check className="mr-1.5 h-3.5 w-3.5" />
                                        {t('log_annotation.save_note', '保存备注')}
                                    </Button>
                                </div>
                            </div>
                        )}

                        {annotationPanelOpen && annotation.labels?.length ? (
                            <div className="mt-3 flex flex-wrap gap-1.5">
                                {annotation.labels.map((label) => (
                                    <Badge key={label} variant="outline" className="border-primary/20 bg-primary/5 text-[11px] font-medium text-primary">
                                        {label}
                                    </Badge>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    {/* URL 地址 */}
                    <div className="rounded-xl border border-border/60 bg-card px-3 py-2">
                        <div className="flex items-center gap-2">
                            <button
                                type="button"
                                onClick={() => toggleSection('url')}
                                className="group flex min-w-0 flex-1 items-center gap-2 rounded-lg px-1 py-1 text-left transition-colors hover:bg-muted"
                            >
                                <div className="rounded-md bg-muted p-1.5 text-muted-foreground group-hover:text-primary">
                                    <Globe className="h-3.5 w-3.5" />
                                </div>
                                <span className="shrink-0 text-xs font-bold text-foreground">{t('log_detail.url')}</span>
                                <span className="min-w-0 flex-1" />
                                {expandedSections.url ? (
                                    <ChevronUp className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                                ) : (
                                    <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                                )}
                            </button>
                            <CopyButton text={displayLog.target_url} field="url" />
                        </div>
                        {expandedSections.url && (
                            <code className="mt-2 block rounded-lg bg-muted/50 px-3 py-2 text-xs font-mono leading-relaxed break-all text-foreground">
                                {displayLog.target_url}
                            </code>
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
                                            onUsePreview={() => {
                                                setPreviewOnly(prev => ({ ...prev, request: true }))
                                                setFullRequestBody(null)
                                            }}
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
                                                    <SearchToggle active={requestSearchOpen} onClick={() => {
                                                        setRequestSearchOpen(v => !v)
                                                        if (requestSearchOpen) setRequestSearchTerm('')
                                                    }} />
                                                    <CopyButton text={effectiveRequestBody} field="requestBody" />
                                                </div>
                                            </div>
                                            {requestSearchOpen && (
                                                <BodySearchBar
                                                    searchTerm={requestSearchTerm}
                                                    onSearchTermChange={setRequestSearchTerm}
                                                    matchCount={requestSearchMatchCount}
                                                    onNavigate={(dir) => navigateSearchMatch(requestBodyRef.current, dir)}
                                                    onClose={() => { setRequestSearchOpen(false); setRequestSearchTerm(''); }}
                                                />
                                            )}
                                            <div ref={requestBodyRef} className="custom-scrollbar flex-1 overflow-x-auto overflow-y-auto p-4">
                                                {requestViewMode === 'raw' ? (
                                                    <RawBodyViewer text={effectiveRequestBody} searchTerm={requestSearchTerm || undefined} />
                                                ) : requestViewMode === 'diff' && hasRequestBodyDiff ? (
                                                    <JsonDiffViewer beforeText={originalRequestBody} afterText={finalRequestBody} />
                                                ) : (
                                                    <JsonViewer data={parsedRequestBody ?? effectiveRequestBody} expandMode={requestExpandMode} searchTerm={requestSearchTerm || undefined} />
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
                                            onUsePreview={() => {
                                                setPreviewOnly(prev => ({ ...prev, response: true }))
                                                setFullResponseBody(null)
                                            }}
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
                                                    <SearchToggle active={responseSearchOpen} onClick={() => {
                                                        setResponseSearchOpen(v => !v)
                                                        if (responseSearchOpen) setResponseSearchTerm('')
                                                    }} />
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
                                            {responseSearchOpen && (
                                                <BodySearchBar
                                                    searchTerm={responseSearchTerm}
                                                    onSearchTermChange={setResponseSearchTerm}
                                                    matchCount={responseSearchMatchCount}
                                                    onNavigate={(dir) => navigateSearchMatch(responseBodyRef.current, dir)}
                                                    onClose={() => { setResponseSearchOpen(false); setResponseSearchTerm(''); }}
                                                />
                                            )}
                                            <div ref={responseBodyRef} className="custom-scrollbar flex-1 overflow-x-auto overflow-y-auto p-4">
                                                {responseViewMode === 'raw' ? (
                                                    <RawBodyViewer text={effectiveResponseBody} searchTerm={responseSearchTerm || undefined} />
                                                ) : responseViewMode === 'merged' ? (
                                                    mergedResponse ? (
                                                        <JsonViewer data={mergedResponse.merged} expandMode={responseExpandMode} searchTerm={responseSearchTerm || undefined} />
                                                    ) : (
                                                        <div className={cn(emptyStateClassName, "text-[11px] italic text-muted-foreground")}>
                                                            {t('log_detail.stream_merge_unavailable', '当前无法生成合并视图，请切换到 Raw 查看原始内容。')}
                                                        </div>
                                                    )
                                                ) : (
                                                    <JsonViewer data={parsedResponseBody ?? effectiveResponseBody} expandMode={responseExpandMode} searchTerm={responseSearchTerm || undefined} />
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

function shouldAutoLoadBody({
    blobRef,
    bodySize,
    contentType,
    previewBody,
    fullBody,
    loading,
    previewOnly,
}: {
    blobRef?: string
    bodySize?: number
    contentType: string
    previewBody: string
    fullBody: string | null
    loading: boolean
    previewOnly: boolean
}) {
    if (!blobRef || fullBody !== null || loading || previewOnly) return false
    if (!bodySize || bodySize > autoLoadFullBodyLimit) return false
    if (isBinaryPlaceholder(previewBody)) return false
    return isAutoLoadableTextContent(contentType, previewBody)
}

function isAutoLoadableTextContent(contentType: string, previewBody: string) {
    const mediaType = contentType.split(';')[0]?.trim().toLowerCase() ?? ''
    if (!mediaType) return Boolean(previewBody.trim())
    if (mediaType.startsWith('text/')) return true
    if (
        mediaType === 'application/json' ||
        mediaType === 'application/xml' ||
        mediaType === 'application/x-ndjson' ||
        mediaType === 'application/x-www-form-urlencoded' ||
        mediaType === 'application/graphql' ||
        mediaType === 'application/javascript'
    ) {
        return true
    }
    return mediaType.endsWith('+json') || mediaType.endsWith('+xml')
}

function normalizedAnnotation(annotation: RequestLog['annotation'] | undefined) {
    return {
        saved: annotation?.saved ?? false,
        status: annotation?.status ?? 'none',
        note: annotation?.note ?? '',
        labels: annotation?.labels ?? [],
    }
}

function parseLabelDraft(value: string) {
    const seen = new Set<string>()
    const labels: string[] = []
    value
        .split(/[,，\n]/)
        .map(item => item.trim())
        .filter(Boolean)
        .forEach((label) => {
            if (seen.has(label)) return
            seen.add(label)
            labels.push(label)
        })
    return labels
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
