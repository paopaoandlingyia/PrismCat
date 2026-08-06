import { useState, useEffect, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { Send, Plus, Trash2, Loader2, Copy, Check, ChevronDown, Braces } from 'lucide-react'
import { toast } from 'sonner'
import { cn, getStatusColor, formatSize, generateId } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import { fetchUpstreams, sendReplay } from '@/lib/api'
import type { Upstream, ReplayResponse } from '@/lib/api'
import { JsonViewer } from '@/components/JsonViewer'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'] as const

const METHOD_COLORS: Record<string, string> = {
    GET: 'bg-emerald-500/10 text-emerald-600 border-emerald-500/30',
    POST: 'bg-blue-500/10 text-blue-600 border-blue-500/30',
    PUT: 'bg-amber-500/10 text-amber-600 border-amber-500/30',
    PATCH: 'bg-orange-500/10 text-orange-600 border-orange-500/30',
    DELETE: 'bg-red-500/10 text-red-600 border-red-500/30',
    HEAD: 'bg-purple-500/10 text-purple-600 border-purple-500/30',
    OPTIONS: 'bg-gray-500/10 text-gray-600 border-gray-500/30',
}

interface HeaderEntry {
    key: string
    value: string
    id: string
}

interface ReplayLocationState {
    replay?: {
        upstream?: string
        target_url?: string
        targetUrl?: string
        method?: string
        path?: string
        body?: string
        headers?: Record<string, string | string[]>
    }
}

type RequestTab = 'body' | 'headers'
type ResponseViewMode = 'pretty' | 'raw'
type RequestBodyViewMode = 'raw' | 'pretty'
type TargetMode = 'upstream' | 'url'

function getErrorMessage(error: unknown, fallback: string) {
    return error instanceof Error ? error.message : fallback
}

export function Playground() {
    const { t } = useTranslation()
    const location = useLocation()

    // Form state
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [targetMode, setTargetMode] = useState<TargetMode>('upstream')
    const [upstream, setUpstream] = useState('')
    const [targetUrl, setTargetUrl] = useState('')
    const [method, setMethod] = useState('POST')
    const [path, setPath] = useState('')
    const [headers, setHeaders] = useState<HeaderEntry[]>([
        { key: 'Content-Type', value: 'application/json', id: generateId() },
    ])
    const [body, setBody] = useState('')

    // Response state
    const [response, setResponse] = useState<ReplayResponse | null>(null)
    const [sending, setSending] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const [elapsed, setElapsed] = useState<number | null>(null)
    const [copiedField, setCopiedField] = useState<string | null>(null)

    // UI state
    const [methodOpen, setMethodOpen] = useState(false)
    const [upstreamOpen, setUpstreamOpen] = useState(false)
    const [activeTab, setActiveTab] = useState<RequestTab>('body')
    const [requestBodyViewMode, setRequestBodyViewMode] = useState<RequestBodyViewMode>('raw')
    const [responseViewMode, setResponseViewMode] = useState<ResponseViewMode>('pretty')

    // Load upstreams
    useEffect(() => {
        fetchUpstreams().then((data) => {
            setUpstreams(data || [])
            setUpstream((current) => current || data?.[0]?.name || '')
        })
    }, [])

    // Pre-fill from navigation state (replay from LogDetail)
    useEffect(() => {
        const state = location.state as ReplayLocationState | null
        if (state?.replay) {
            const r = state.replay
            const replayTargetUrl = r.target_url || r.targetUrl || ''
            if (replayTargetUrl) setTargetUrl(replayTargetUrl)
            if (r.upstream) {
                setTargetMode('upstream')
                setUpstream(r.upstream)
            } else if (replayTargetUrl) {
                setTargetMode('url')
            }
            if (r.method) setMethod(r.method)
            if (r.path) setPath(r.path)
            if (r.body) setBody(r.body)
            if (r.headers && typeof r.headers === 'object') {
                const entries: HeaderEntry[] = Object.entries(r.headers)
                    .filter(([k]) => {
                        const skip = ['host', 'connection', 'keep-alive', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'proxy-authorization', 'proxy-authenticate', 'proxy-connection']
                        return !skip.includes(k.toLowerCase())
                    })
                    .map(([key, value]) => ({
                        key,
                        value: Array.isArray(value) ? value.join('; ') : value,
                        id: generateId()
                    }))
                if (entries.length > 0) setHeaders(entries)
            }
            window.history.replaceState({}, '')
        }
    }, [location.state])

    const selectedUpstreamExists = useMemo(
        () => upstreams.some((u) => u.name === upstream),
        [upstreams, upstream],
    )
    const selectedUpstreamMissing = targetMode === 'upstream' && upstream !== '' && upstreams.length > 0 && !selectedUpstreamExists

    useEffect(() => {
        if (selectedUpstreamMissing && targetUrl) {
            setTargetMode('url')
        }
    }, [selectedUpstreamMissing, targetUrl])

    const parsedRequestBody = useMemo(() => {
        const text = body.trim()
        if (!text) return null
        try {
            return JSON.parse(text)
        } catch {
            return null
        }
    }, [body])

    const requestBodyJsonError = useMemo(() => {
        const text = body.trim()
        if (!text) return null
        try {
            JSON.parse(text)
            return null
        } catch (err: unknown) {
            return getErrorMessage(err, 'Invalid JSON')
        }
    }, [body])

    // Parsed response body
    const parsedResponseBody = useMemo(() => {
        if (responseViewMode !== 'pretty') return null
        if (!response?.body) return null
        try {
            return JSON.parse(response.body)
        } catch {
            return null
        }
    }, [responseViewMode, response?.body])

    const handleAddHeader = () => {
        setHeaders([...headers, { key: '', value: '', id: generateId() }])
    }

    const handleRemoveHeader = (id: string) => {
        setHeaders(headers.filter((h) => h.id !== id))
    }

    const handleHeaderChange = (id: string, field: 'key' | 'value', val: string) => {
        setHeaders(headers.map((h) => (h.id === id ? { ...h, [field]: val } : h)))
    }

    const copyToClipboard = async (text: string, field: string) => {
        if (!(await copyText(text))) {
            toast.error(t('log_detail.copy_failed'))
            return
        }
        setCopiedField(field)
        setTimeout(() => setCopiedField(null), 2000)
    }

    const formatRequestBody = () => {
        const text = body.trim()
        if (!text) return
        try {
            setBody(JSON.stringify(JSON.parse(text), null, 2))
            setRequestBodyViewMode('raw')
        } catch {
            setRequestBodyViewMode('raw')
        }
    }

    const handleSend = useCallback(async () => {
        if (!method) return
        if (targetMode === 'upstream' && !upstream) return
        if (targetMode === 'url' && !targetUrl.trim()) return

        setError(null)
        setResponse(null)
        setResponseViewMode('pretty')
        setSending(true)

        const headerMap: Record<string, string> = {}
        headers.forEach((h) => {
            if (h.key.trim()) headerMap[h.key.trim()] = h.value
        })

        const startTime = performance.now()
        try {
            const resp = await sendReplay({
                ...(targetMode === 'upstream'
                    ? { upstream, path }
                    : { target_url: targetUrl.trim() }),
                method,
                headers: headerMap,
                body,
            })
            setElapsed(Math.round(performance.now() - startTime))
            setResponse(resp)
        } catch (err: unknown) {
            setElapsed(Math.round(performance.now() - startTime))
            setError(getErrorMessage(err, '请求失败'))
        } finally {
            setSending(false)
        }
    }, [targetMode, upstream, targetUrl, method, path, headers, body])

    const RawBodyViewer = ({ text }: { text: string }) => (
        <pre className="whitespace-pre-wrap break-all text-[11px] font-mono leading-relaxed text-foreground select-text">
            {text}
        </pre>
    )

    const ViewToggle = ({
        value,
        onChange,
    }: {
        value: ResponseViewMode
        onChange: (value: ResponseViewMode) => void
    }) => (
        <div className="flex items-center gap-1 rounded-md border border-border/40 bg-background/70 p-1">
            {([
                { value: 'pretty', label: t('log_detail.view_pretty', 'Pretty') },
                { value: 'raw', label: t('log_detail.view_raw', 'Raw') },
            ] as const).map((option) => (
                <Button
                    key={option.value}
                    type="button"
                    variant={value === option.value ? 'secondary' : 'ghost'}
                    size="sm"
                    onClick={() => onChange(option.value)}
                    className={cn(
                        'h-6 px-2 text-[10px] font-bold uppercase tracking-wider',
                        value === option.value && 'shadow-none'
                    )}
                >
                    {option.label}
                </Button>
            ))}
        </div>
    )

    // Handle Ctrl+Enter to send
    useEffect(() => {
        const handler = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault()
                handleSend()
            }
        }
        window.addEventListener('keydown', handler)
        return () => window.removeEventListener('keydown', handler)
    }, [handleSend])

    return (
        <div className="mx-auto w-full max-w-7xl space-y-5 animate-fade-in">

            {/* Unified Address Bar */}
            <div className="flex flex-wrap items-center gap-2 bg-muted/10 p-1.5 rounded-2xl">
                {/* Method Selector */}
                <div className="relative shrink-0">
                    <button
                        onClick={() => setMethodOpen(!methodOpen)}
                        className={cn(
                            'flex items-center gap-1 px-3 py-2.5 rounded-xl border text-xs font-black uppercase tracking-wider transition-all min-w-[80px] justify-between',
                            METHOD_COLORS[method] || METHOD_COLORS['GET']
                        )}
                    >
                        {method}
                        <ChevronDown className="h-3 w-3 opacity-50" />
                    </button>
                    {methodOpen && (
                        <>
                            <div className="fixed inset-0 z-40" onClick={() => setMethodOpen(false)} />
                            <div className="absolute top-full left-0 mt-2 z-50 bg-popover border border-border shadow-xl py-1 min-w-[120px] rounded-lg">
                                {HTTP_METHODS.map((m) => (
                                    <button
                                        key={m}
                                        onClick={() => { setMethod(m); setMethodOpen(false) }}
                                        className={cn(
                                            'w-full px-3 py-1.5 text-left text-xs font-bold uppercase tracking-wider hover:bg-accent transition-colors',
                                            m === method && 'bg-accent'
                                        )}
                                    >
                                        {m}
                                    </button>
                                ))}
                            </div>
                        </>
                    )}
                </div>

                <div className="flex shrink-0 items-center gap-1 rounded-xl border border-border/40 bg-background/70 p-1 shadow-sm">
                    {([
                        { value: 'upstream', label: t('playground.target_upstream', 'Upstream') },
                        { value: 'url', label: t('playground.target_url', 'URL') },
                    ] as const).map((option) => (
                        <button
                            key={option.value}
                            type="button"
                            onClick={() => setTargetMode(option.value)}
                            className={cn(
                                'h-7 rounded-lg px-2.5 text-[10px] font-black uppercase tracking-wider transition-all',
                                targetMode === option.value
                                    ? 'bg-secondary text-foreground shadow-sm'
                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                            )}
                        >
                            {option.label}
                        </button>
                    ))}
                </div>

                {targetMode === 'upstream' ? (
                    <>
                        {/* Upstream Selector */}
                        <div className="relative shrink-0">
                            <button
                                onClick={() => setUpstreamOpen(!upstreamOpen)}
                                className={cn(
                                    'flex items-center gap-1 px-3 py-2.5 rounded-xl border bg-background/80 text-xs font-bold hover:bg-accent transition-all min-w-[90px] justify-between shadow-sm',
                                    selectedUpstreamMissing ? 'border-amber-500/50 text-amber-600' : 'border-input'
                                )}
                            >
                                <span className="text-foreground/80 truncate max-w-[100px]">{upstream || t('playground.select_upstream')}</span>
                                <ChevronDown className="h-3 w-3 opacity-50" />
                            </button>
                            {upstreamOpen && (
                                <>
                                    <div className="fixed inset-0 z-40" onClick={() => setUpstreamOpen(false)} />
                                    <div className="absolute top-full left-0 mt-2 z-50 bg-popover border border-border shadow-xl py-1 min-w-[180px] rounded-lg">
                                        {upstreams.map((u) => (
                                            <button
                                                key={u.name}
                                                onClick={() => { setUpstream(u.name); setUpstreamOpen(false) }}
                                                className={cn(
                                                    'w-full px-3 py-1.5 text-left text-xs font-bold hover:bg-accent transition-colors',
                                                    u.name === upstream && 'bg-accent'
                                                )}
                                            >
                                                <span className="font-black">{u.name}</span>
                                                <span className="ml-2 text-muted-foreground font-normal truncate">{u.target}</span>
                                            </button>
                                        ))}
                                        {upstreams.length === 0 && (
                                            <div className="px-3 py-2 text-xs text-muted-foreground italic">
                                                {t('playground.no_upstreams')}
                                            </div>
                                        )}
                                    </div>
                                </>
                            )}
                        </div>

                        {/* Path Input */}
                        <input
                            type="text"
                            value={path}
                            onChange={(e) => setPath(e.target.value)}
                            placeholder="/v1/chat/completions"
                            className="flex-1 min-w-[220px] px-3 py-2.5 rounded-xl bg-background border border-input text-sm font-mono placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all shadow-sm"
                        />
                    </>
                ) : (
                    <input
                        type="url"
                        value={targetUrl}
                        onChange={(e) => setTargetUrl(e.target.value)}
                        placeholder={t('playground.custom_url_placeholder', 'https://api.openai.com/v1/chat/completions')}
                        className="flex-1 min-w-[260px] px-3 py-2.5 rounded-xl bg-background border border-input text-sm font-mono placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all shadow-sm"
                    />
                )}

                {/* Send Button */}
                <Button
                    onClick={handleSend}
                    disabled={
                        sending ||
                        (targetMode === 'upstream' ? !upstream || selectedUpstreamMissing : !targetUrl.trim())
                    }
                    className="shrink-0 px-5 py-2.5 h-auto font-black gap-2 bg-primary hover:bg-primary/90 shadow-lg shadow-primary/20 transition-all"
                >
                    {sending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                        <Send className="h-4 w-4" />
                    )}
                    <span className="hidden sm:inline">{t('playground.send')}</span>
                </Button>
            </div>

            {/* Request Config Tabs */}
            <div className="space-y-0">
                {/* Tab Headers */}
                <div className="flex items-center gap-1 border-b border-border/30">
                    <button
                        onClick={() => setActiveTab('body')}
                        className={cn(
                            'px-4 py-2.5 text-xs font-black uppercase tracking-wider transition-all border-b-2 -mb-px',
                            activeTab === 'body'
                                ? 'border-primary text-foreground'
                                : 'border-transparent text-muted-foreground/50 hover:text-muted-foreground/80'
                        )}
                    >
                        {t('playground.body')}
                    </button>
                    <button
                        onClick={() => setActiveTab('headers')}
                        className={cn(
                            'px-4 py-2.5 text-xs font-black uppercase tracking-wider transition-all border-b-2 -mb-px flex items-center gap-1.5',
                            activeTab === 'headers'
                                ? 'border-primary text-foreground'
                                : 'border-transparent text-muted-foreground/50 hover:text-muted-foreground/80'
                        )}
                    >
                        {t('playground.headers')}
                        {headers.length > 0 && (
                            <span className={cn(
                                'text-[9px] font-bold px-1.5 py-0.5 rounded-full',
                                activeTab === 'headers' ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground/50'
                            )}>
                                {headers.length}
                            </span>
                        )}
                    </button>
                </div>

                {/* Tab Content: Body */}
                {activeTab === 'body' && (
                    <div className="space-y-2 pt-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                            <div className="flex items-center gap-2">
                                {body.trim() && (
                                    <Badge
                                        variant="outline"
                                        className={cn(
                                            'h-6 border-none px-2 text-[10px] font-bold',
                                            requestBodyJsonError
                                                ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400'
                                                : 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                                        )}
                                        title={requestBodyJsonError || undefined}
                                    >
                                        {requestBodyJsonError
                                            ? t('playground.json_invalid', 'Invalid JSON')
                                            : t('playground.json_valid', 'JSON')}
                                    </Badge>
                                )}
                                <Button
                                    type="button"
                                    variant="outline"
                                    size="sm"
                                    onClick={formatRequestBody}
                                    disabled={!body.trim() || !!requestBodyJsonError}
                                    className="h-7 gap-1.5 px-2.5 text-[11px] font-bold"
                                >
                                    <Braces className="h-3.5 w-3.5" />
                                    {t('playground.format_json', 'Format')}
                                </Button>
                            </div>
                            <div className="flex items-center gap-1 rounded-md border border-border/40 bg-background/70 p-1">
                                {([
                                    { value: 'raw', label: t('log_detail.view_raw', 'Raw') },
                                    { value: 'pretty', label: t('log_detail.view_pretty', 'Pretty') },
                                ] as const).map((option) => (
                                    <Button
                                        key={option.value}
                                        type="button"
                                        variant={requestBodyViewMode === option.value ? 'secondary' : 'ghost'}
                                        size="sm"
                                        onClick={() => setRequestBodyViewMode(option.value)}
                                        className="h-6 px-2 text-[10px] font-bold uppercase tracking-wider"
                                    >
                                        {option.label}
                                    </Button>
                                ))}
                            </div>
                        </div>
                        {requestBodyViewMode === 'raw' ? (
                            <textarea
                                value={body}
                                onChange={(e) => setBody(e.target.value)}
                                placeholder='{ "model": "gpt-4", "messages": [...] }'
                                className="w-full h-[260px] px-4 py-3 rounded-xl bg-background border border-input text-xs font-mono leading-relaxed placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 resize-none custom-scrollbar transition-all shadow-sm"
                                spellCheck={false}
                            />
                        ) : (
                            <div className="h-[260px] overflow-auto rounded-xl border border-input bg-background px-4 py-3 shadow-sm custom-scrollbar">
                                {body.trim() ? (
                                    <JsonViewer data={parsedRequestBody ?? body} />
                                ) : (
                                    <div className="flex h-full items-center justify-center text-[11px] italic text-muted-foreground/40">
                                        {t('playground.empty_state', 'Configure request parameters and send')}
                                    </div>
                                )}
                            </div>
                        )}
                    </div>
                )}

                {/* Tab Content: Headers */}
                {activeTab === 'headers' && (
                    <div className="pt-3 space-y-2">
                        <div className="space-y-1.5 max-h-[240px] overflow-y-auto custom-scrollbar">
                            {headers.map((h) => (
                                <div key={h.id} className="flex items-center gap-2 group">
                                    <input
                                        type="text"
                                        value={h.key}
                                        onChange={(e) => handleHeaderChange(h.id, 'key', e.target.value)}
                                        placeholder="Header Name"
                                        className="w-[35%] sm:w-[30%] px-3 py-2 rounded-lg bg-background border border-input shadow-sm text-xs font-mono font-bold placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
                                    />
                                    <input
                                        type="text"
                                        value={h.value}
                                        onChange={(e) => handleHeaderChange(h.id, 'value', e.target.value)}
                                        placeholder="Value"
                                        className="flex-1 px-3 py-2 rounded-lg bg-background border border-input shadow-sm text-xs font-mono placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
                                    />
                                    <button
                                        onClick={() => handleRemoveHeader(h.id)}
                                        className="p-1.5 rounded-lg text-muted-foreground/20 hover:text-red-500 hover:bg-red-500/10 transition-all opacity-0 group-hover:opacity-100"
                                    >
                                        <Trash2 className="h-3.5 w-3.5" />
                                    </button>
                                </div>
                            ))}
                        </div>
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={handleAddHeader}
                            className="h-8 px-3 text-[11px] font-bold gap-1.5 text-muted-foreground/50 hover:text-foreground"
                        >
                            <Plus className="h-3 w-3" />
                            {t('playground.add_header')}
                        </Button>
                    </div>
                )}
            </div>

            {/* Response */}
            {(response || error || sending) && (
                <div className="rounded-2xl border border-border bg-card shadow-sm overflow-hidden">
                    {/* Response Header */}
                    <div className="px-4 py-3 border-b border-border/20 flex flex-wrap items-center gap-3">
                        <span className="text-xs font-black uppercase tracking-wider text-muted-foreground/60">
                            {t('playground.response')}
                        </span>
                        {sending && (
                            <div className="flex items-center gap-2 text-[10px] font-black uppercase text-primary animate-pulse">
                                <Loader2 className="h-3 w-3 animate-spin" />
                                {t('common.loading')}
                            </div>
                        )}
                        {response && (
                            <>
                                <Badge
                                    variant="outline"
                                    className={cn(
                                        'font-black text-xs border-none',
                                        getStatusColor(response.status_code)
                                    )}
                                >
                                    {response.status_code}
                                </Badge>
                                {elapsed !== null && (
                                    <span className="text-[10px] font-mono text-muted-foreground/50">
                                        {elapsed}ms
                                    </span>
                                )}
                                {response.body && (
                                    <span className="text-[10px] font-mono text-muted-foreground/50">
                                        {formatSize(response.body.length)}
                                        {response.truncated && (
                                            <span className="ml-1 text-amber-500 font-black">
                                                (TRUNCATED)
                                            </span>
                                        )}
                                    </span>
                                )}
                                {response.body_decoded && (
                                    <Badge
                                        variant="outline"
                                        className="h-5 border-sky-500/30 bg-sky-500/5 px-1.5 text-[10px] font-bold text-sky-600 dark:text-sky-400"
                                    >
                                        {t('playground.body_decoded', {
                                            encoding: (response.body_decoded_from || 'gzip').toUpperCase(),
                                        })}
                                    </Badge>
                                )}
                                {response.body && (
                                    <ViewToggle value={responseViewMode} onChange={setResponseViewMode} />
                                )}
                                <div className="ml-auto">
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-7 w-7"
                                        onClick={() => copyToClipboard(response.body, 'resp')}
                                    >
                                        {copiedField === 'resp' ? (
                                            <Check className="h-3.5 w-3.5 text-green-500" />
                                        ) : (
                                            <Copy className="h-3.5 w-3.5 text-muted-foreground/50" />
                                        )}
                                    </Button>
                                </div>
                            </>
                        )}
                    </div>

                    {/* Error */}
                    {error && (
                        <div className="p-4 bg-red-500/5 border-b border-red-500/10">
                            <pre className="text-xs text-red-500 font-mono whitespace-pre-wrap">{error}</pre>
                        </div>
                    )}

                    {/* Response Headers */}
                    {response?.headers && Object.keys(response.headers).length > 0 && (
                        <details className="group">
                            <summary className="px-4 py-2 cursor-pointer text-[10px] font-black uppercase tracking-wider text-muted-foreground/40 hover:text-muted-foreground transition-colors select-none">
                                {t('playground.response_headers')} ({Object.keys(response.headers).length})
                            </summary>
                            <div className="px-4 pb-3 space-y-1 font-mono text-[11px]">
                                {Object.entries(response.headers).map(([k, vv]) => (
                                    <div key={k} className="flex flex-col sm:flex-row sm:gap-2">
                                        <span className="text-green-500/70 shrink-0 font-bold">{k}:</span>
                                        <div className="flex flex-col">
                                            {vv.map((v, i) => (
                                                <span key={i} className="text-foreground/70 break-all">{v}{i < vv.length - 1 ? ';' : ''}</span>
                                            ))}
                                        </div>
                                    </div>
                                ))}
                            </div>
                            <Separator className="bg-border/20" />
                        </details>
                    )}

                    {/* Response Body */}
                    {response?.body && (
                        <div className="p-4 max-h-[600px] overflow-auto custom-scrollbar">
                            {responseViewMode === 'raw' ? (
                                <RawBodyViewer text={response.body} />
                            ) : (
                                <JsonViewer data={parsedResponseBody ?? response.body} />
                            )}
                        </div>
                    )}

                    {/* Empty response */}
                    {response && !response.body && !error && (
                        <div className="p-8 text-center text-[11px] text-muted-foreground/40 italic">
                            {t('playground.empty_response')}
                        </div>
                    )}
                </div>
            )}


        </div>
    )
}

