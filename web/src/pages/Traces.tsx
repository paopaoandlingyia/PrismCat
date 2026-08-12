import { useState, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Network, AlertCircle, Search, X, ChevronLeft, ChevronRight } from 'lucide-react'
import { fetchTraces, fetchUpstreams, type TraceSummary, type TraceFilter, type Upstream } from '@/lib/api'
import { cn, formatLatency } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { toast } from 'sonner'

export function Traces() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const [traces, setTraces] = useState<TraceSummary[]>([])
  const [total, setTotal] = useState(0)
  const [upstreams, setUpstreams] = useState<Upstream[]>([])
  const [filter, setFilter] = useState<TraceFilter>({
    trace_id: searchParams.get('trace_id') || '',
    upstream: '',
    offset: 0,
    limit: 20,
  })
  const [draftFilter, setDraftFilter] = useState<TraceFilter>({ ...filter })

  useEffect(() => {
    let cancelled = false

    async function load() {
      try {
        const res = await fetchTraces(filter)
        if (cancelled) return
        setTraces(res.traces || [])
        setTotal(res.total)
      } catch {
        if (!cancelled) toast.error(t('traces.load_failed'))
      }
    }

    void load()

    return () => {
      cancelled = true
    }
  }, [filter, t])

  useEffect(() => {
    fetchUpstreams().then(setUpstreams).catch(() => {})
  }, [])

  const handleSearch = () => {
    setFilter({ ...draftFilter, offset: 0 })
  }

  const handleReset = () => {
    const reset: TraceFilter = { offset: 0, limit: 20 }
    setDraftFilter(reset)
    setFilter(reset)
  }

  const maxLatency = traces.reduce((max, t) => Math.max(max, t.total_latency_ms), 1)

  return (
    <div className="space-y-6">
      {/* Filter bar */}
      <div className="rounded-md bg-muted/30 p-2 md:p-3 space-y-2">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
          <Input
            placeholder={t('traces.search_placeholder')}
            value={draftFilter.trace_id || ''}
            onChange={e => setDraftFilter(prev => ({ ...prev, trace_id: e.target.value }))}
            onKeyDown={e => e.key === 'Enter' && handleSearch()}
            className="h-9 text-xs"
          />
          <Select
            value={draftFilter.upstream || '_all'}
            onValueChange={v => setDraftFilter(prev => ({ ...prev, upstream: v === '_all' ? '' : v }))}
          >
            <SelectTrigger className="h-9 text-xs">
              <SelectValue placeholder={t('filters.upstream_placeholder')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="_all">{t('common.all')}</SelectItem>
              {upstreams.map(u => (
                <SelectItem key={u.name} value={u.name}>{u.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="flex gap-2">
            <Button size="sm" onClick={handleSearch} className="h-9 gap-1.5 flex-1">
              <Search className="h-3.5 w-3.5" />
              {t('common.search')}
            </Button>
            <Button size="sm" variant="outline" onClick={handleReset} className="h-9 gap-1.5">
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        {/* Pagination */}
        <div className="flex items-center justify-between text-xs text-muted-foreground px-1">
          <span>{t('traces.total_count', { count: total })}</span>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost" size="sm" className="h-7 w-7 p-0"
              disabled={filter.offset === 0}
              onClick={() => setFilter(prev => ({ ...prev, offset: Math.max(0, (prev.offset || 0) - (prev.limit || 20)) }))}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <span>
              {Math.floor((filter.offset || 0) / (filter.limit || 20)) + 1} / {Math.max(1, Math.ceil(total / (filter.limit || 20)))}
            </span>
            <Button
              variant="ghost" size="sm" className="h-7 w-7 p-0"
              disabled={(filter.offset || 0) + (filter.limit || 20) >= total}
              onClick={() => setFilter(prev => ({ ...prev, offset: (prev.offset || 0) + (prev.limit || 20) }))}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      {/* Traces table */}
      {traces.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
          <Network className="h-12 w-12 mb-4 opacity-30" />
          <p className="text-sm font-medium">{t('traces.no_traces')}</p>
          <p className="text-xs mt-1 opacity-60">{t('traces.no_traces_hint')}</p>
        </div>
      ) : (
        <>
          {/* Desktop table */}
          <div className="hidden md:block rounded-lg overflow-hidden bg-card/20 border border-border">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b bg-muted/30">
                  <th className="px-4 py-3 text-left font-medium text-xs text-muted-foreground">{t('traces.trace_id')}</th>
                  <th className="px-3 py-3 text-center font-medium text-xs text-muted-foreground">{t('traces.requests')}</th>
                  <th className="px-3 py-3 text-left font-medium text-xs text-muted-foreground">{t('traces.upstreams')}</th>
                  <th className="px-3 py-3 text-right font-medium text-xs text-muted-foreground">{t('traces.duration')}</th>
                  <th className="px-3 py-3 text-right font-medium text-xs text-muted-foreground">{t('traces.tokens')}</th>
                  <th className="px-3 py-3 text-center font-medium text-xs text-muted-foreground">{t('traces.errors')}</th>
                  <th className="px-3 py-3 text-right font-medium text-xs text-muted-foreground">{t('traces.time')}</th>
                </tr>
              </thead>
              <tbody>
                {traces.map(trace => (
                  <tr
                    key={trace.trace_id}
                    onClick={() => navigate(`/traces/${encodeURIComponent(trace.trace_id)}`)}
                    className="border-b transition-colors hover:bg-muted/40 cursor-pointer"
                  >
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Network className="h-3.5 w-3.5 shrink-0 text-info" />
                        <span className="font-mono text-foreground/90 truncate max-w-[200px]">{trace.trace_id}</span>
                      </div>
                      {/* Latency bar */}
                      <div className="mt-1.5 h-1.5 w-full rounded-full bg-muted/50">
                        <div
                          className={cn(
                            'h-1.5 rounded-full transition-all duration-500',
                            trace.error_count > 0 ? 'bg-danger' :
                            trace.total_latency_ms > 5000 ? 'bg-warning' :
                            'bg-info'
                          )}
                          style={{ width: `${Math.max(4, (trace.total_latency_ms / maxLatency) * 100)}%` }}
                        />
                      </div>
                    </td>
                    <td className="px-3 py-3 text-center">
                      <span className="font-mono font-semibold text-foreground/80">{trace.request_count}</span>
                    </td>
                    <td className="px-3 py-3">
                      <div className="flex flex-wrap gap-1">
                        {(trace.upstreams || []).map(u => (
                          <span key={u} className="inline-flex items-center h-[18px] px-1.5 rounded-md text-xs font-semibold tracking-tight bg-primary/10 text-primary">
                            {u}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="px-3 py-3 text-right font-mono text-muted-foreground">
                      {formatLatency(trace.total_latency_ms)}
                    </td>
                    <td className="px-3 py-3 text-right font-mono text-muted-foreground">
                      {typeof trace.usage_total_tokens === 'number' ? trace.usage_total_tokens.toLocaleString() : '-'}
                    </td>
                    <td className="px-3 py-3 text-center">
                      <span className={cn('font-mono font-semibold', trace.error_count > 0 ? 'text-danger' : 'text-muted-foreground/60')}>
                        {trace.error_count}
                      </span>
                    </td>
                    <td className="px-3 py-3 text-right text-muted-foreground">
                      {new Date(trace.last_time).toLocaleTimeString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Mobile cards */}
          <div className="md:hidden space-y-2">
            {traces.map(trace => (
              <button
                key={trace.trace_id}
                onClick={() => navigate(`/traces/${encodeURIComponent(trace.trace_id)}`)}
                className="w-full rounded-lg p-4 text-left transition-all active:scale-[0.99] bg-card/20 hover:bg-card/40 border border-border/50"
              >
                <div className="flex items-center gap-2 mb-2">
                  <Network className="h-3.5 w-3.5 text-info shrink-0" />
                  <span className="font-mono text-xs text-foreground/90 truncate flex-1">{trace.trace_id}</span>
                  <span className="text-xs font-medium bg-primary/10 text-primary px-1.5 py-0.5 rounded">{trace.request_count} reqs</span>
                  {trace.error_count > 0 && (
                    <span className="text-xs font-medium bg-danger/10 text-danger px-1.5 py-0.5 rounded flex items-center gap-0.5">
                      <AlertCircle className="h-2.5 w-2.5" />{trace.error_count}
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-2 mb-2 flex-wrap">
                  {(trace.upstreams || []).map(u => (
                    <span key={u} className="text-xs font-semibold tracking-tight bg-primary/10 text-primary px-1.5 py-0.5 rounded-md">{u}</span>
                  ))}
                  <span className="text-xs font-mono text-muted-foreground ml-auto">{formatLatency(trace.total_latency_ms)}</span>
                </div>
                <div className="mb-2 text-xs font-mono text-muted-foreground/70">
                  {t('traces.tokens')}: {typeof trace.usage_total_tokens === 'number' ? trace.usage_total_tokens.toLocaleString() : '-'}
                </div>
                <div className="h-1.5 w-full rounded-full bg-muted/50">
                  <div
                    className={cn(
                      'h-1.5 rounded-full',
                      trace.error_count > 0 ? 'bg-danger/60' : 'bg-info/60'
                    )}
                    style={{ width: `${Math.max(4, (trace.total_latency_ms / maxLatency) * 100)}%` }}
                  />
                </div>
                <div className="flex items-center justify-between mt-2 text-xs text-muted-foreground/60">
                  <span>{new Date(trace.first_time).toLocaleString()}</span>
                  <span>{new Date(trace.last_time).toLocaleTimeString()}</span>
                </div>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
