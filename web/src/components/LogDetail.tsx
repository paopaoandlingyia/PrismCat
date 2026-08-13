import { toast } from 'sonner'
import { cn, formatDate, formatLatency, formatSize, getStatusColor, METHOD_CLASS, FOCUS_RING } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import { Copy, Check, Zap, AlertTriangle, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, ChevronsDownUp, ChevronsUpDown, FileCode, ListTree, Layers, RotateCcw, Maximize2, Minimize2, ExternalLink, Terminal, Bookmark, BookmarkCheck, CheckCircle2, CircleDot, Tags, Search, X } from 'lucide-react'
import { fetchBlob, fetchLogBody, updateLogAnnotation } from '@/lib/api'
import type { LiveLogEvent, RequestLog } from '@/lib/api'
import { startTransition, useCallback, useDeferredValue, useEffect, useMemo, useRef, useState, type ComponentType, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { JsonViewer, type JsonExpandMode, HighlightText } from './JsonViewer'
import { JsonDiffViewer } from './JsonDiffViewer'
import { BlobPanel } from './BlobPanel'
import { mergeStreamBody } from '@/lib/streamMerge'
import { collectTextSearchMatches, createJsonSearchPlan, MAX_RENDERED_SEARCH_MATCHES, type JsonSearchPlan } from '@/lib/jsonSearch'
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
    onNavigateLog?: (direction: LogNavigationDirection) => void
    canNavigatePreviousLog?: boolean
    canNavigateNextLog?: boolean
}

type LogNavigationDirection = 'previous' | 'next'
type BodyViewMode = 'pretty' | 'raw'
type RequestViewMode = BodyViewMode | 'diff'
type ResponseViewMode = BodyViewMode | 'merged'
type PanelWidthMode = 'wide' | 'full'

const logDetailWidthStorageKey = 'prismcat.logDetail.width'
const logDetailExpandedStorageKey = 'prismcat.logDetail.expanded'
const autoLoadFullBodyLimit = 10 * 1024 * 1024

const defaultExpandedSections = {
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
            requestHeaders: typeof parsed?.requestHeaders === 'boolean' ? parsed.requestHeaders : defaultExpandedSections.requestHeaders,
            requestBody: typeof parsed?.requestBody === 'boolean' ? parsed.requestBody : defaultExpandedSections.requestBody,
            responseHeaders: typeof parsed?.responseHeaders === 'boolean' ? parsed.responseHeaders : defaultExpandedSections.responseHeaders,
            responseBody: typeof parsed?.responseBody === 'boolean' ? parsed.responseBody : defaultExpandedSections.responseBody,
        }
    } catch {
        return defaultExpandedSections
    }
}

interface BodySearchStats {
    matchCount: number
    truncated: boolean
}

function createTextSearchStats(text: string, term: string): BodySearchStats {
    const result = collectTextSearchMatches(text, term, MAX_RENDERED_SEARCH_MATCHES)
    return { matchCount: result.ranges.length, truncated: result.truncated }
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

function clearActiveSearchMatch(container: HTMLElement | null) {
    container?.querySelector<HTMLElement>('[data-search-match-active]')?.removeAttribute('data-search-match-active');
}

function RawBodyViewer({ text, searchTerm, maxMatches }: { text: string; searchTerm?: string; maxMatches: number }) {
    return (
        <pre className="whitespace-pre-wrap break-all text-xs font-mono leading-relaxed text-foreground select-text">
            {searchTerm ? <HighlightText text={text} searchTerm={searchTerm} maxMatches={maxMatches} /> : text}
        </pre>
    );
}

/** 概览卡的标签列 */
function OverviewLabel({ children }: { children: ReactNode }) {
    return <dt className="whitespace-nowrap font-medium text-muted-foreground">{children}</dt>
}

function UsageMetric({ label, value }: { label: string; value?: number }) {
    return (
        <span className="whitespace-nowrap">
            <span className="text-muted-foreground">{label}</span>{' '}
            <span className="font-mono font-medium text-foreground">
                {typeof value === 'number' ? value.toLocaleString() : '-'}
            </span>
        </span>
    )
}

function BodySearchBar({
    searchTerm,
    onSearchTermChange,
    matchCount,
    truncated,
    pending,
    onNavigate,
    onClose,
}: {
    searchTerm: string;
    onSearchTermChange: (term: string) => void;
    matchCount: number;
    truncated: boolean;
    pending: boolean;
    onNavigate: (dir: 'prev' | 'next') => void;
    onClose: () => void;
}) {
    const { t } = useTranslation();
    const matchLabel = pending
        ? '…'
        : truncated
            ? t('body_search.match_count_truncated', { count: matchCount, defaultValue: '{{count}}+ matches' })
            : matchCount > 0
                ? t('body_search.match_count', { count: matchCount })
                : t('body_search.no_matches', 'No matches')
    return (
        <div className="flex items-center gap-2 border-b border-border px-3 py-1.5 bg-muted/20">
            <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <input
                autoFocus
                value={searchTerm}
                onChange={(e) => onSearchTermChange(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                        e.preventDefault()
                        if (!pending && matchCount > 0) onNavigate(e.shiftKey ? 'prev' : 'next')
                    }
                    if (e.key === 'Escape') { e.preventDefault(); onClose(); }
                }}
                placeholder={t('body_search.placeholder', 'Search in body...')}
                className="flex-1 min-w-0 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
            />
            {searchTerm && (
                <span className="text-xs font-medium text-muted-foreground whitespace-nowrap">
                    {matchLabel}
                </span>
            )}
            <div className="flex items-center">
                <button
                    type="button"
                    onClick={() => onNavigate('prev')}
                    disabled={!searchTerm || pending || matchCount === 0}
                    className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                    <ChevronUp className="h-3 w-3" />
                </button>
                <button
                    type="button"
                    onClick={() => onNavigate('next')}
                    disabled={!searchTerm || pending || matchCount === 0}
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

export function LogDetail({
    log,
    loading,
    onClose,
    onLogChange,
    onNavigateLog,
    canNavigatePreviousLog = false,
    canNavigateNextLog = false,
}: LogDetailProps) {
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
    const [requestSearchOpen, setRequestSearchOpen] = useState(false)
    const [requestSearchTerm, setRequestSearchTerm] = useState('')
    const [responseSearchOpen, setResponseSearchOpen] = useState(false)
    const [responseSearchTerm, setResponseSearchTerm] = useState('')
    const deferredRequestSearchTerm = useDeferredValue(requestSearchTerm)
    const deferredResponseSearchTerm = useDeferredValue(responseSearchTerm)
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

    const capturedRequestBody = fullRequestBody ?? displayLog?.request_body ?? ''
    const effectiveResponseBody = fullResponseBody ?? displayLog?.response_body ?? ''
    const originalRequestBody = displayLog?.request_body_original ?? ''
    const showsOriginalRequestBody = Boolean(
        displayLog?.request_override_error &&
        !displayLog?.request_body_ref &&
        !capturedRequestBody &&
        originalRequestBody,
    )
    const effectiveRequestBody = showsOriginalRequestBody ? originalRequestBody : capturedRequestBody
    const finalRequestBody = displayLog?.request_body_final ?? (originalRequestBody ? capturedRequestBody : '')
    const hasRequestBodyDiff = Boolean(originalRequestBody && finalRequestBody && originalRequestBody !== finalRequestBody)
    const requestBodyDisplaySize = showsOriginalRequestBody
        ? textByteSize(originalRequestBody)
        : displayLog?.request_body_size ?? 0
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
    const hasUsage = displayLog?.usage_input_tokens !== undefined ||
        displayLog?.usage_output_tokens !== undefined ||
        displayLog?.usage_total_tokens !== undefined

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

    const effectiveRequestSearchTerm = requestSearchTerm ? deferredRequestSearchTerm : ''
    const effectiveResponseSearchTerm = responseSearchTerm ? deferredResponseSearchTerm : ''
    const requestSearchPending = requestSearchTerm !== effectiveRequestSearchTerm
    const responseSearchPending = responseSearchTerm !== effectiveResponseSearchTerm
    const requestViewerData = parsedRequestBody ?? effectiveRequestBody
    const responseViewerData = responseViewMode === 'merged' && mergedResponse
        ? mergedResponse.merged
        : parsedResponseBody ?? effectiveResponseBody

    const requestJsonSearchPlan = useMemo<JsonSearchPlan | null>(() => {
        if (!effectiveRequestSearchTerm || requestViewMode !== 'pretty') return null
        return createJsonSearchPlan(requestViewerData, effectiveRequestSearchTerm)
    }, [effectiveRequestSearchTerm, requestViewMode, requestViewerData])

    const responseJsonSearchPlan = useMemo<JsonSearchPlan | null>(() => {
        if (!effectiveResponseSearchTerm || responseViewMode === 'raw' || (responseViewMode === 'merged' && !mergedResponse)) return null
        return createJsonSearchPlan(responseViewerData, effectiveResponseSearchTerm)
    }, [effectiveResponseSearchTerm, responseViewMode, responseViewerData, mergedResponse])

    const requestSearchStats = useMemo<BodySearchStats>(() => {
        if (!effectiveRequestSearchTerm || requestViewMode === 'diff') return { matchCount: 0, truncated: false }
        if (requestViewMode === 'raw') return createTextSearchStats(effectiveRequestBody, effectiveRequestSearchTerm)
        return requestJsonSearchPlan ?? { matchCount: 0, truncated: false }
    }, [effectiveRequestSearchTerm, requestViewMode, effectiveRequestBody, requestJsonSearchPlan])

    const responseSearchStats = useMemo<BodySearchStats>(() => {
        if (!effectiveResponseSearchTerm) return { matchCount: 0, truncated: false }
        if (responseViewMode === 'raw') return createTextSearchStats(effectiveResponseBody, effectiveResponseSearchTerm)
        return responseJsonSearchPlan ?? { matchCount: 0, truncated: false }
    }, [effectiveResponseSearchTerm, responseViewMode, effectiveResponseBody, responseJsonSearchPlan])

    useEffect(() => {
        clearActiveSearchMatch(requestBodyRef.current)
    }, [effectiveRequestSearchTerm, requestViewMode])

    useEffect(() => {
        clearActiveSearchMatch(responseBodyRef.current)
    }, [effectiveResponseSearchTerm, responseViewMode])

    const copyToClipboard = async (text: string, field: string) => {
        if (!(await copyText(text))) {
            toast.error(t('log_detail.copy_failed'))
            return
        }
        setCopiedField(field)
        setTimeout(() => setCopiedField(null), 2000)
    }

    const toggleSection = (section: keyof typeof expandedSections) => {
        setExpandedSections(prev => ({ ...prev, [section]: !prev[section] }))
    }

    const loadBlob = useCallback(async (kind: 'request' | 'response', ref: string) => {
        setBlobError(null)
        setPreviewOnly(prev => ({ ...prev, [kind]: false }))
        setBlobLoading(prev => ({ ...prev, [kind]: true }))
        try {
            const body = displayLog?.id ? (await fetchLogBody(displayLog.id, kind)).body : await fetchBlob(ref)
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
    }, [displayLog?.id])

    const copyCurlCommand = () => {
        const currentLog = displayLog
        if (!currentLog) return

        if (!currentLog.request_body_final && currentLog.request_body_ref && !fullRequestBody) {
            if (!blobLoading.request) {
                void loadBlob('request', currentLog.request_body_ref)
            }
            toast.info(t('log_detail.curl_body_loading'))
            return
        }

        let body = currentLog.request_body_final || fullRequestBody || currentLog.request_body || ''
        if (!body && currentLog.request_override_error) {
            body = currentLog.request_body_original || ''
        }
        void copyToClipboard(buildCurlCommand(currentLog, body), 'curl')
    }

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
        // overflow-hidden 和圆角必须同时存在:header(bg-card)和滚动区(bg-muted/30)都是直角,
        // 不裁剪就会盖在 16px 圆角上,看起来像两层边角对不齐
        "w-full p-0 flex flex-col overflow-hidden bg-background",
        panelWidthMode === 'wide' && "border-l border-border sm:rounded-l-2xl sm:max-w-6xl",
        panelWidthMode === 'full' && "border-0 sm:rounded-none sm:max-w-none"
    )
    // 请求/响应 只作为分组标题浮在背景上,每个可折叠块自己是一张卡。
    // 原来外面还套一层 section 卡(border + bg-card + p-5),导致
    // bg-muted/30 → bg-card → bg-muted/50 三层色差极小的盒子套盒子
    const groupLabelClassName = "px-1 text-xs font-medium text-muted-foreground"
    const blockCardClassName = "rounded-lg border border-border bg-card px-3"
    const contentCardClassName = "rounded-lg bg-muted/50 p-3.5"
    const codeCardClassName = "rounded-lg bg-muted/50"
    const emptyStateClassName = "rounded-lg border border-dashed border-border bg-muted/50 px-4 py-6 text-center"

    const CopyButton = ({ text, field }: { text: string; field: string }) => {
        const label = copiedField === field ? t('common.copied') : t('common.copy')
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
                            <Check className="h-3.5 w-3.5 text-success" />
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
                        "h-6 rounded-md px-2 text-xs font-medium transition-all",
                        value === option.value
                            ? "border border-border bg-background text-foreground hover:bg-background"
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
        const label = willExpand ? t('log_detail.expand_all') : t('log_detail.collapse_all')
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
                <span className="text-xs font-medium text-foreground group-hover:text-primary transition-colors">
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
                <SheetHeader className="gap-2 border-b border-border bg-card px-5 py-3">
                    {/* 第一行只放身份信息。pr-10 是给绝对定位的关闭按钮(right-4)留位,
                        原来靠操作组上的 mr-10 让位,窄窗口下会把按钮挤到换行 */}
                    <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 pr-10">
                        <div
                                className={cn(
                                    "w-14 py-0.5 rounded-md text-xs text-center font-medium border",
                                    METHOD_CLASS
                                )}
                        >
                            {displayLog.method}
                        </div>
                        <SheetTitle className={cn(
                            "font-mono text-xl font-semibold",
                            getStatusColor(displayLog.status_code)
                        )}>
                            {displayLog.status_code || '---'}
                        </SheetTitle>
                        {displayLog.streaming && (
                            <Badge variant="secondary" className="border-none bg-primary/10 text-primary font-medium text-xs">
                                <Zap className="mr-1 h-3 w-3 fill-current" />
                                {t('log_detail.streaming', 'STREAMING')}
                            </Badge>
                        )}
                        {displayLog.error && (
                            <Badge variant="destructive" className="border-none bg-danger/10 text-danger font-medium text-xs">
                                <AlertTriangle className="mr-1 h-3 w-3" />
                                {t('common.error', 'ERROR')}
                            </Badge>
                        )}
                        {/* 与日志表保持一致:"已修改"是属性不是警告 */}
                        {displayLog.request_override_applied && (
                            <Badge variant="outline" className="border-border bg-muted text-muted-foreground font-medium text-xs">
                                {t('log_detail.modified', 'MODIFIED')}
                            </Badge>
                        )}
                        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1 text-xs font-semibold text-muted-foreground">
                            <span className="truncate text-foreground">
                                {displayLog.upstream}
                                {displayLog.upstream_target ? ` / ${displayLog.upstream_target}` : ''}
                            </span>
                            <span className="text-border">/</span>
                            <span className="font-mono">{formatLatency(displayLog.latency_ms)}</span>
                            <span className="text-border">/</span>
                            <span>{formatDate(displayLog.created_at, i18n.language)}</span>
                        </div>
                    </div>

                    {/* 第二行:操作栏。和身份信息分开,窄窗口下两者不再互相挤 */}
                    <div className="flex flex-wrap items-center justify-end gap-2">
                        {loading && (
                            <div className="flex items-center gap-2 text-xs font-medium text-primary">
                                <div className="h-1 w-1 rounded-full bg-current" />
                                {t('common.loading')}
                            </div>
                        )}
                        {!loading && (
                            <>
                                {onNavigateLog && (
                                    <div className="flex items-center gap-1 rounded-lg border border-border bg-background p-0.5">
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <Button
                                                    type="button"
                                                    variant="ghost"
                                                    size="icon"
                                                    disabled={!canNavigatePreviousLog}
                                                    onClick={() => onNavigateLog('previous')}
                                                    className="h-7 w-7 rounded-md text-muted-foreground hover:bg-primary/10 hover:text-primary disabled:opacity-35"
                                                    aria-label={t('log_detail.previous_log', 'Previous log')}
                                                >
                                                    <ChevronLeft className="h-3.5 w-3.5" />
                                                </Button>
                                            </TooltipTrigger>
                                            <TooltipContent side="top" sideOffset={4}>
                                                {t('log_detail.previous_log', 'Previous log')}
                                            </TooltipContent>
                                        </Tooltip>
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <Button
                                                    type="button"
                                                    variant="ghost"
                                                    size="icon"
                                                    disabled={!canNavigateNextLog}
                                                    onClick={() => onNavigateLog('next')}
                                                    className="h-7 w-7 rounded-md text-muted-foreground hover:bg-primary/10 hover:text-primary disabled:opacity-35"
                                                    aria-label={t('log_detail.next_log', 'Next log')}
                                                >
                                                    <ChevronRight className="h-3.5 w-3.5" />
                                                </Button>
                                            </TooltipTrigger>
                                            <TooltipContent side="top" sideOffset={4}>
                                                {t('log_detail.next_log', 'Next log')}
                                            </TooltipContent>
                                        </Tooltip>
                                    </div>
                                )}
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
                                    className="h-7 gap-1.5 border-border bg-background px-2.5 text-xs font-semibold transition-all hover:border-primary/30 hover:bg-primary/10 hover:text-primary"
                                    onClick={copyCurlCommand}
                                >
                                    {copiedField === 'curl' ? (
                                        <Check className="h-3 w-3 text-success" />
                                    ) : (
                                        <Terminal className="h-3 w-3" />
                                    )}
                                    {t('log_detail.copy_as_curl', 'Copy as cURL')}
                                </Button>
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="h-7 gap-1.5 border-primary/20 bg-primary/5 px-2.5 text-xs font-semibold transition-all hover:border-primary/30 hover:bg-primary/10 hover:text-primary"
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
                                                const full = (await fetchLogBody(displayLog.id, 'request')).body
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
                            </>
                        )}
                    </div>
                </SheetHeader>

                {/* 主内容区域 */}
                <div className="custom-scrollbar flex-1 space-y-4 overflow-y-auto bg-background px-5 py-4">
                    <div className="rounded-md border border-border bg-card px-3 py-2.5">
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
                                {annotation.saved ? t('log_annotation.saved') : t('log_annotation.save')}
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
                                    annotation.status === 'todo' && "bg-warning/10 text-warning hover:bg-warning/15 dark:text-warning"
                                )}
                            >
                                <CircleDot className="h-3.5 w-3.5" />
                                {t('log_annotation.todo')}
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
                                    annotation.status === 'done' && "bg-success/10 text-success hover:bg-success/15 dark:text-success"
                                )}
                            >
                                <CheckCircle2 className="h-3.5 w-3.5" />
                                {t('log_annotation.done')}
                            </Button>
                            {annotationSaving && (
                                <span className="text-xs font-medium text-muted-foreground">{t('common.loading')}</span>
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
                                    placeholder={t('log_annotation.note_placeholder')}
                                    className={cn('min-h-20 resize-y rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm leading-relaxed transition-colors placeholder:text-muted-foreground/70 focus-visible:bg-background', FOCUS_RING)}
                                />
                                <div className="space-y-2">
                                    <div className="relative">
                                        <Tags className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                                        <input
                                            value={annotationLabels}
                                            onChange={(event) => setAnnotationLabels(event.target.value)}
                                            placeholder={t('log_annotation.labels_placeholder')}
                                            className={cn('h-10 w-full rounded-lg border border-border bg-muted/40 pl-9 pr-3 text-sm transition-colors placeholder:text-muted-foreground/70 focus-visible:bg-background', FOCUS_RING)}
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
                                        {t('log_annotation.save_note')}
                                    </Button>
                                </div>
                            </div>
                        )}

                        {annotationPanelOpen && annotation.labels?.length ? (
                            <div className="mt-3 flex flex-wrap gap-1.5">
                                {annotation.labels.map((label) => (
                                    <Badge key={label} variant="outline" className="border-primary/20 bg-primary/5 text-xs font-medium text-primary">
                                        {label}
                                    </Badge>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    {/* 错误详情 */}
                    {displayLog.error && (
                        <div className="overflow-hidden rounded-lg border border-danger/20 bg-danger/5 p-4">
                            <div className="mb-3 flex items-center gap-2 text-danger font-medium text-xs">
                                <AlertTriangle className="h-4 w-4" />
                                {t('common.error')}
                            </div>
                            <pre className="text-xs text-danger font-mono whitespace-pre-wrap leading-relaxed">{displayLog.error}</pre>
                        </div>
                    )}

                    {/* 概览:URL / Token / 参数覆盖 合成一张紧凑卡。
                        原来是三张全宽卡,一行 URL 或三个五位数各占满 1152px,
                        把真正要看的请求体挤到首屏之外 */}
                    <div className="rounded-md border border-border bg-card px-3 py-2.5">
                        <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-4 gap-y-2 text-xs">
                            <OverviewLabel>{t('log_detail.url')}</OverviewLabel>
                            <dd className="flex min-w-0 items-baseline gap-1">
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        {/* 不加 flex-1:让复制按钮紧跟 URL 末尾,
                                            否则按钮被推到 1152px 卡片的最右边 */}
                                        <code className="min-w-0 truncate font-mono text-foreground">
                                            {displayLog.target_url}
                                        </code>
                                    </TooltipTrigger>
                                    <TooltipContent side="bottom" className="max-w-[520px] break-all font-mono">
                                        {displayLog.target_url}
                                    </TooltipContent>
                                </Tooltip>
                                <CopyButton text={displayLog.target_url} field="url" />
                            </dd>

                            {/* UUID 固定 36 字符,放在头部身份行会比 8 字符截断宽 179px,
                                窄窗口必然换行;而它的价值只在"能被复制",不在"能被读" */}
                            <OverviewLabel>{t('log_detail.log_id')}</OverviewLabel>
                            <dd className="flex min-w-0 items-baseline gap-1">
                                <code className="min-w-0 truncate font-mono text-foreground">
                                    {displayLog.id}
                                </code>
                                <CopyButton text={displayLog.id} field="id" />
                            </dd>

                            {hasUsage && (
                                <>
                                    <OverviewLabel>{t('log_detail.usage')}</OverviewLabel>
                                    <dd className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
                                        <UsageMetric label={t('log_detail.usage_input')} value={displayLog.usage_input_tokens} />
                                        <UsageMetric label={t('log_detail.usage_output')} value={displayLog.usage_output_tokens} />
                                        <UsageMetric label={t('log_detail.usage_total')} value={displayLog.usage_total_tokens} />
                                        {displayLog.usage_source && (
                                            <span className="font-mono text-muted-foreground/70">{displayLog.usage_source}</span>
                                        )}
                                    </dd>
                                </>
                            )}

                            {(displayLog.request_override_applied || displayLog.request_override_error) && (
                                <>
                                    <OverviewLabel>{t('log_detail.request_override')}</OverviewLabel>
                                    <dd className="min-w-0 space-y-1.5">
                                        {displayLog.request_override_rules?.length ? (
                                            <div className="flex flex-wrap gap-1.5">
                                                {displayLog.request_override_rules.map((rule) => (
                                                    <Badge key={rule} variant="outline" className="border-warning/30 bg-background text-xs font-semibold text-foreground">
                                                        {rule}
                                                    </Badge>
                                                ))}
                                            </div>
                                        ) : null}
                                        {displayLog.request_override_error && (
                                            <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-warning">{displayLog.request_override_error}</pre>
                                        )}
                                        {displayLog.request_header_override_applied && displayLog.request_header_override_changes?.length ? (
                                            <div className="space-y-1 font-mono">
                                                {displayLog.request_header_override_changes.map((change, idx) => (
                                                    <div key={idx} className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 leading-relaxed">
                                                        <Badge variant="outline" className={cn(
                                                            "h-5 shrink-0 rounded px-1.5 text-xs font-medium",
                                                            change.op === 'set'
                                                                ? "border-info/30 bg-info/10 text-info"
                                                                : "border-danger/30 bg-danger/10 text-danger"
                                                        )}>
                                                            {change.op}
                                                        </Badge>
                                                        <span className="font-semibold text-foreground">{change.name}</span>
                                                        {change.old_value && (
                                                            <span className="text-muted-foreground line-through">{change.old_value}</span>
                                                        )}
                                                        {change.op === 'set' && (
                                                            <span className="text-foreground">{change.value}</span>
                                                        )}
                                                    </div>
                                                ))}
                                            </div>
                                        ) : null}
                                    </dd>
                                </>
                            )}
                        </dl>
                    </div>

                    {/* 请求体 & 请求头 */}
                    <div className="space-y-2">
                        <div className={groupLabelClassName}>
                            {t('log_detail.request')}
                        </div>
                        <div className={cn(blockCardClassName, "space-y-2")}>
                            <SectionHeader
                                title={t('log_detail.request') + ' ' + t('log_detail.body')}
                                section="requestBody"
                                icon={FileCode}
                                extra={
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-medium text-muted-foreground">{formatSize(requestBodyDisplaySize)}</span>
                                        {displayLog.truncated && (
                                            <Badge variant="outline" className="h-5 text-xs border-warning/40 text-warning bg-warning/5 px-1.5 font-semibold">
                                                {t('log_detail.truncated_tag', 'TRUNCATED')}
                                            </Badge>
                                        )}
                                    </div>
                                }
                            />
                            {expandedSections.requestBody && (
                                <div className="space-y-3 pb-3">
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

                                    {showsOriginalRequestBody && (
                                        <Badge variant="outline" className="w-fit border-warning/30 bg-warning/5 text-xs font-semibold text-warning">
                                            {t('log_detail.request_body_original_fallback', 'Original body before failed override')}
                                        </Badge>
                                    )}

                                    {effectiveRequestBody && !(requestBodyIsBinary && displayLog.request_body_ref) ? (
                                        <div className={cn(codeCardClassName, "flex max-h-[500px] flex-col")}>
                                            <div className="flex items-center justify-between gap-2 border-b border-border px-2 py-1">
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
                                                    {requestViewMode !== 'diff' && (
                                                        <SearchToggle active={requestSearchOpen} onClick={() => {
                                                            setRequestSearchOpen(v => !v)
                                                            if (requestSearchOpen) setRequestSearchTerm('')
                                                        }} />
                                                    )}
                                                    <CopyButton text={effectiveRequestBody} field="requestBody" />
                                                </div>
                                            </div>
                                            {requestSearchOpen && requestViewMode !== 'diff' && (
                                                <BodySearchBar
                                                    searchTerm={requestSearchTerm}
                                                    onSearchTermChange={setRequestSearchTerm}
                                                    matchCount={requestSearchStats.matchCount}
                                                    truncated={requestSearchStats.truncated}
                                                    pending={requestSearchPending}
                                                    onNavigate={(dir) => navigateSearchMatch(requestBodyRef.current, dir)}
                                                    onClose={() => { setRequestSearchOpen(false); setRequestSearchTerm(''); }}
                                                />
                                            )}
                                            <div ref={requestBodyRef} className="custom-scrollbar flex-1 overflow-x-auto overflow-y-auto p-4">
                                                {requestViewMode === 'raw' ? (
                                                    <RawBodyViewer text={effectiveRequestBody} searchTerm={effectiveRequestSearchTerm || undefined} maxMatches={requestSearchStats.matchCount} />
                                                ) : requestViewMode === 'diff' && hasRequestBodyDiff ? (
                                                    <JsonDiffViewer beforeText={originalRequestBody} afterText={finalRequestBody} />
                                                ) : (
                                                    <JsonViewer data={requestViewerData} expandMode={requestExpandMode} searchTerm={effectiveRequestSearchTerm || undefined} searchPlan={requestJsonSearchPlan} />
                                                )}
                                            </div>
                                        </div>
                                    ) : (
                                        <div className={cn(emptyStateClassName, "text-xs italic text-muted-foreground")}>
                                            {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>

                        <div className={cn(blockCardClassName, "space-y-2")}>
                            <SectionHeader
                                title={t('log_detail.request') + ' ' + t('log_detail.headers')}
                                section="requestHeaders"
                                icon={ListTree}
                                extra={<span className="text-xs font-medium text-muted-foreground">{Object.keys(displayLog.request_headers ?? {}).length} KEYS</span>}
                            />
                            {expandedSections.requestHeaders && displayLog.request_headers && (
                                <div className={cn(contentCardClassName, "mb-3 space-y-2 font-mono text-xs leading-relaxed")}>
                                    {Object.entries(displayLog.request_headers).map(([key, vv]) => (
                                        <div key={key} className="flex flex-col sm:flex-row sm:gap-2 group/line">
                                            <span className="text-primary shrink-0 font-semibold">{key}:</span>
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
                    <div className="space-y-2">
                        <div className={groupLabelClassName}>
                            {t('log_detail.response')}
                        </div>
                        <div className={cn(blockCardClassName, "space-y-2")}>
                            <SectionHeader
                                title={t('log_detail.response') + ' ' + t('log_detail.body')}
                                section="responseBody"
                                icon={FileCode}
                                extra={
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-medium text-muted-foreground">{formatSize(displayLog.response_body_size)}</span>
                                        {displayLog.truncated && (
                                            <Badge variant="outline" className="h-5 text-xs border-warning/40 text-warning bg-warning/5 px-1.5 font-semibold">
                                                {t('log_detail.truncated_tag', 'TRUNCATED')}
                                            </Badge>
                                        )}
                                    </div>
                                }
                            />
                            {expandedSections.responseBody && (
                                <div className="space-y-3 pb-3">
                                    {displayLog.streaming && responseViewMode === 'merged' && mergedResponse && (
                                        <div className="flex items-center gap-1.5 px-1 text-xs font-mono text-muted-foreground">
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
                                            <div className="flex items-center justify-between gap-2 border-b border-border px-2 py-1">
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
                                                    matchCount={responseSearchStats.matchCount}
                                                    truncated={responseSearchStats.truncated}
                                                    pending={responseSearchPending}
                                                    onNavigate={(dir) => navigateSearchMatch(responseBodyRef.current, dir)}
                                                    onClose={() => { setResponseSearchOpen(false); setResponseSearchTerm(''); }}
                                                />
                                            )}
                                            <div ref={responseBodyRef} className="custom-scrollbar flex-1 overflow-x-auto overflow-y-auto p-4">
                                                {responseViewMode === 'raw' ? (
                                                    <RawBodyViewer text={effectiveResponseBody} searchTerm={effectiveResponseSearchTerm || undefined} maxMatches={responseSearchStats.matchCount} />
                                                ) : responseViewMode === 'merged' ? (
                                                    mergedResponse ? (
                                                        <JsonViewer data={mergedResponse.merged} expandMode={responseExpandMode} searchTerm={effectiveResponseSearchTerm || undefined} searchPlan={responseJsonSearchPlan} />
                                                    ) : (
                                                        <div className={cn(emptyStateClassName, "text-xs italic text-muted-foreground")}>
                                                            {t('log_detail.stream_merge_unavailable')}
                                                        </div>
                                                    )
                                                ) : (
                                                    <JsonViewer data={responseViewerData} expandMode={responseExpandMode} searchTerm={effectiveResponseSearchTerm || undefined} searchPlan={responseJsonSearchPlan} />
                                                )}
                                            </div>
                                        </div>
                                    ) : (
                                        <div className={cn(emptyStateClassName, "text-xs italic text-muted-foreground")}>
                                            {loading ? t('common.loading') : t('log_detail.no_body', '--- EMPTY BODY ---')}
                                        </div>
                                    )}
                                </div>
                            )}
                        </div>

                        <div className={cn(blockCardClassName, "space-y-2")}>
                            <SectionHeader
                                title={t('log_detail.response') + ' ' + t('log_detail.headers')}
                                section="responseHeaders"
                                icon={ListTree}
                                extra={<span className="text-xs font-medium text-muted-foreground">{Object.keys(displayLog.response_headers ?? {}).length} KEYS</span>}
                            />
                            {expandedSections.responseHeaders && displayLog.response_headers && (
                                <div className={cn(contentCardClassName, "mb-3 space-y-2 font-mono text-xs leading-relaxed")}>
                                    {Object.entries(displayLog.response_headers).map(([key, vv]) => (
                                        <div key={key} className="flex flex-col sm:flex-row sm:gap-2 group/line">
                                            <span className="text-success shrink-0 font-semibold">{key}:</span>
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

function textByteSize(text: string) {
    return new TextEncoder().encode(text).length
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
