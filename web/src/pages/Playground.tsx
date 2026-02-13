import { useState, useEffect, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { Send, Plus, Trash2, Loader2, Copy, Check, ChevronDown } from 'lucide-react'
import { cn, getStatusColor, formatSize } from '@/lib/utils'
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

type RequestTab = 'body' | 'headers'

export function Playground() {
    const { t } = useTranslation()
    const location = useLocation()

    // Form state
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [upstream, setUpstream] = useState('')
    const [method, setMethod] = useState('POST')
    const [path, setPath] = useState('')
    const [headers, setHeaders] = useState<HeaderEntry[]>([
        { key: 'Content-Type', value: 'application/json', id: crypto.randomUUID() },
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

    // Load upstreams
    useEffect(() => {
        fetchUpstreams().then((data) => {
            setUpstreams(data || [])
            if (data?.length > 0 && !upstream) {
                setUpstream(data[0].name)
            }
        })
    }, [])

    // Pre-fill from navigation state (replay from LogDetail)
    useEffect(() => {
        const state = location.state as any
        if (state?.replay) {
            const r = state.replay
            if (r.upstream) setUpstream(r.upstream)
            if (r.method) setMethod(r.method)
            if (r.path) setPath(r.path)
            if (r.body) setBody(r.body)
            if (r.headers && typeof r.headers === 'object') {
                const entries: HeaderEntry[] = Object.entries(r.headers as Record<string, string>)
                    .filter(([k]) => {
                        const skip = ['host', 'connection', 'keep-alive', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'proxy-authorization', 'proxy-authenticate', 'proxy-connection']
                        return !skip.includes(k.toLowerCase())
                    })
                    .map(([key, value]) => ({ key, value, id: crypto.randomUUID() }))
                if (entries.length > 0) setHeaders(entries)
            }
            window.history.replaceState({}, '')
        }
    }, [location.state])

    // Parsed response body
    const parsedResponseBody = useMemo(() => {
        if (!response?.body) return null
        try {
            return JSON.parse(response.body)
        } catch {
            return null
        }
    }, [response?.body])

    const handleAddHeader = () => {
        setHeaders([...headers, { key: '', value: '', id: crypto.randomUUID() }])
    }

    const handleRemoveHeader = (id: string) => {
        setHeaders(headers.filter((h) => h.id !== id))
    }

    const handleHeaderChange = (id: string, field: 'key' | 'value', val: string) => {
        setHeaders(headers.map((h) => (h.id === id ? { ...h, [field]: val } : h)))
    }

    const copyToClipboard = async (text: string, field: string) => {
        await navigator.clipboard.writeText(text)
        setCopiedField(field)
        setTimeout(() => setCopiedField(null), 2000)
    }

    const handleSend = useCallback(async () => {
        if (!upstream || !method) return

        setError(null)
        setResponse(null)
        setSending(true)

        const headerMap: Record<string, string> = {}
        headers.forEach((h) => {
            if (h.key.trim()) headerMap[h.key.trim()] = h.value
        })

        const startTime = performance.now()
        try {
            const resp = await sendReplay({
                upstream,
                method,
                path,
                headers: headerMap,
                body,
            })
            setElapsed(Math.round(performance.now() - startTime))
            setResponse(resp)
        } catch (err: any) {
            setElapsed(Math.round(performance.now() - startTime))
            setError(err?.message || '请求失败')
        } finally {
            setSending(false)
        }
    }, [upstream, method, path, headers, body])

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
        <div className="w-full space-y-5 animate-fade-in">
            {/* Title */}
            <div>
                <h2 className="text-xl font-black tracking-tight">{t('playground.title')}</h2>
                <p className="text-xs text-muted-foreground/60 mt-0.5">{t('playground.description')}</p>
            </div>

            {/* Unified Address Bar */}
            <div className="flex items-center gap-2 bg-muted/10 p-1.5 rounded-2xl">
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

                {/* Upstream Selector */}
                <div className="relative shrink-0">
                    <button
                        onClick={() => setUpstreamOpen(!upstreamOpen)}
                        className="flex items-center gap-1 px-3 py-2.5 rounded-xl border border-border/40 bg-background/40 text-xs font-bold hover:bg-background/80 transition-all min-w-[90px] justify-between"
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
                    className="flex-1 min-w-0 px-3 py-2.5 rounded-xl bg-background/50 border border-border/40 text-sm font-mono placeholder:text-muted-foreground/20 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
                />

                {/* Send Button */}
                <Button
                    onClick={handleSend}
                    disabled={sending || !upstream}
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
                    <div className="pt-3">
                        <textarea
                            value={body}
                            onChange={(e) => setBody(e.target.value)}
                            placeholder='{ "model": "gpt-4", "messages": [...] }'
                            className="w-full h-[240px] px-4 py-3 rounded-xl bg-background border border-border/40 text-xs font-mono leading-relaxed placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 resize-none custom-scrollbar transition-all"
                            spellCheck={false}
                        />
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
                                        className="w-[35%] sm:w-[30%] px-3 py-2 rounded-lg bg-background border border-border/40 text-xs font-mono font-bold placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
                                    />
                                    <input
                                        type="text"
                                        value={h.value}
                                        onChange={(e) => handleHeaderChange(h.id, 'value', e.target.value)}
                                        placeholder="Value"
                                        className="flex-1 px-3 py-2 rounded-lg bg-background border border-border/40 text-xs font-mono placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-primary/20 transition-all"
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
                <div className="rounded-2xl border border-border/30 bg-card/30 backdrop-blur-sm overflow-hidden">
                    {/* Response Header */}
                    <div className="px-4 py-3 border-b border-border/20 flex items-center gap-3">
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
                                    </span>
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
                                {Object.entries(response.headers).map(([k, v]) => (
                                    <div key={k} className="flex">
                                        <span className="text-green-500/70 shrink-0 font-bold">{k}:</span>
                                        <span className="ml-2 text-foreground/70 break-all">{v}</span>
                                    </div>
                                ))}
                            </div>
                            <Separator className="bg-border/20" />
                        </details>
                    )}

                    {/* Response Body */}
                    {response?.body && (
                        <div className="p-4 max-h-[600px] overflow-auto custom-scrollbar">
                            <JsonViewer data={parsedResponseBody ?? response.body} />
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

