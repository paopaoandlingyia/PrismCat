import { useState, useEffect, useMemo, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Network, ArrowLeft, Clock, AlertCircle, Layers, Activity,
  ChevronRight,
} from 'lucide-react'
import { fetchTraceDetail, type TraceDetail as TraceDetailType, type RequestLog } from '@/lib/api'
import { cn, formatLatency, formatDate, getStatusColor, getMethodColor } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { LogDetail } from '@/components/LogDetail'
import { toast } from 'sonner'

export function TraceDetail() {
  const { t, i18n } = useTranslation()
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()

  const [detail, setDetail] = useState<TraceDetailType | null>(null)
  const [loading, setLoading] = useState(true)
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null)

  const loadDetail = useCallback(async () => {
    if (!traceId) return
    setLoading(true)
    try {
      const res = await fetchTraceDetail(traceId)
      setDetail(res)
    } catch {
      toast.error(t('traces.load_detail_failed'))
    } finally {
      setLoading(false)
    }
  }, [traceId, t])

  useEffect(() => {
    loadDetail()
  }, [loadDetail])

  const requests = detail?.requests ?? []
  const summary = detail?.summary

  const maxLatency = useMemo(
    () => requests.reduce((max, r) => Math.max(max, r.latency_ms), 1),
    [requests],
  )

  const traceSpanMs = useMemo(() => {
    if (!summary) return 1
    return Math.max(summary.last_time - summary.first_time, 1)
  }, [summary])

  if (loading) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center gap-4">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
        <div className="text-sm font-medium text-muted-foreground">{t('common.loading')}</div>
      </div>
    )
  }

  if (!detail || !summary) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
        <Network className="h-12 w-12 mb-4 opacity-30" />
        <p className="text-sm font-medium">{t('traces.not_found')}</p>
        <Button variant="ghost" size="sm" className="mt-4 gap-1.5" onClick={() => navigate('/traces')}>
          <ArrowLeft className="h-3.5 w-3.5" />
          {t('traces.back_to_list')}
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb + back */}
      <div className="flex items-center gap-2 text-sm">
        <Button variant="ghost" size="sm" className="h-8 gap-1.5 text-muted-foreground" onClick={() => navigate('/traces')}>
          <ArrowLeft className="h-3.5 w-3.5" />
          {t('traces.title')}
        </Button>
        <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/50" />
        <span className="font-mono text-xs text-foreground/80 truncate max-w-[300px]">{traceId}</span>
      </div>

      {/* Summary strip */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        <SummaryCard
          icon={Layers}
          label={t('traces.requests')}
          value={String(summary.request_count)}
          accent="text-cyan-500"
        />
        <SummaryCard
          icon={Clock}
          label={t('traces.duration')}
          value={formatLatency(summary.total_latency_ms)}
          accent="text-cyan-500"
        />
        <SummaryCard
          icon={AlertCircle}
          label={t('traces.errors')}
          value={String(summary.error_count)}
          accent={summary.error_count > 0 ? 'text-red-500' : 'text-muted-foreground'}
        />
        <SummaryCard
          icon={Activity}
          label={t('traces.upstreams')}
          value={String(summary.upstreams?.length ?? 0)}
          accent="text-cyan-500"
        />
        <SummaryCard
          icon={Activity}
          label={t('traces.tokens')}
          value={typeof summary.usage_total_tokens === 'number' ? summary.usage_total_tokens.toLocaleString() : '-'}
          accent="text-emerald-500"
        />
      </div>

      {/* Upstream badges */}
      {summary.upstreams?.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {summary.upstreams.map(u => (
            <span key={u} className="inline-flex items-center h-[22px] px-2 rounded text-[10px] font-black uppercase tracking-tight bg-primary/10 text-primary">
              {u}
            </span>
          ))}
        </div>
      )}

      {/* Timeline */}
      <div className="space-y-0">
        {requests.map((req, idx) => {
          const isLast = idx === requests.length - 1
          const offsetMs = new Date(req.created_at).getTime() - summary.first_time
          const offsetPct = Math.min(100, (offsetMs / traceSpanMs) * 100)

          return (
            <div key={req.id} className="flex gap-3 group">
              {/* Timeline connector */}
              <div className="flex flex-col items-center w-8 shrink-0">
                <div
                  className={cn(
                    'h-3 w-3 rounded-full border-2 mt-4 z-10 transition-colors',
                    req.error
                      ? 'border-red-500 bg-red-500/20'
                      : req.status_code >= 400
                        ? 'border-amber-500 bg-amber-500/20'
                        : 'border-cyan-500 bg-cyan-500/20',
                    'group-hover:scale-125',
                  )}
                />
                {!isLast && (
                  <div className="w-0.5 flex-1 bg-border/60" />
                )}
              </div>

              {/* Request card */}
              <button
                type="button"
                onClick={() => setSelectedLog(req)}
                className={cn(
                  'flex-1 rounded-xl p-3 mb-1 text-left transition-all',
                  'bg-card/20 hover:bg-card/50 border border-border/40 hover:border-border',
                  'active:scale-[0.995]',
                  selectedLog?.id === req.id && 'ring-1 ring-cyan-500/40 bg-card/40',
                )}
              >
                <div className="flex items-center gap-2 flex-wrap">
                  {/* Sequence badge */}
                  <span className="text-[10px] font-mono font-bold text-cyan-500 bg-cyan-500/10 px-1.5 py-0.5 rounded">
                    #{req.trace_seq ?? idx + 1}
                  </span>

                  {/* Method */}
                  <span className={cn(
                    'px-1.5 py-0.5 rounded text-[10px] font-bold uppercase',
                    getMethodColor(req.method),
                  )}>
                    {req.method}
                  </span>

                  {/* Status */}
                  <span className={cn('font-mono text-xs font-bold', getStatusColor(req.status_code))}>
                    {req.status_code || '---'}
                  </span>

                  {/* Upstream */}
                  <span className="text-[10px] font-bold text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">
                    {req.upstream}
                  </span>

                  {req.streaming && (
                    <span className="text-[9px] font-bold text-primary bg-primary/10 px-1 py-0.5 rounded animate-pulse">
                      SSE
                    </span>
                  )}

                  {req.error && (
                    <AlertCircle className="h-3 w-3 text-red-500" />
                  )}

                  <span className="ml-auto text-[10px] font-mono text-muted-foreground">
                    {formatLatency(req.latency_ms)}
                  </span>
                </div>

                {/* Path */}
                <div className="mt-1.5 text-xs font-mono text-foreground/70 truncate">
                  {req.path}{req.query ? `?${req.query}` : ''}
                </div>

                {/* Latency bar */}
                <div className="mt-2 flex items-center gap-2">
                  <div className="flex-1 h-1.5 rounded-full bg-muted/50 overflow-hidden">
                    {/* Offset indicator */}
                    <div className="h-full flex">
                      <div style={{ width: `${offsetPct}%` }} />
                      <div
                        className={cn(
                          'h-full rounded-full transition-all duration-500',
                          req.error ? 'bg-red-500/50' :
                          req.status_code >= 400 ? 'bg-amber-500/50' :
                          'bg-cyan-500/50',
                        )}
                        style={{ width: `${Math.max(3, (req.latency_ms / maxLatency) * (100 - offsetPct))}%` }}
                      />
                    </div>
                  </div>
                  <span className="text-[9px] text-muted-foreground/60 shrink-0">
                    {new Date(req.created_at).toLocaleTimeString()}
                  </span>
                </div>
              </button>
            </div>
          )
        })}
      </div>

      {/* Time range footer */}
      {requests.length > 0 && (
        <div className="flex items-center justify-between text-[10px] text-muted-foreground/60 px-1">
          <span>{formatDate(new Date(summary.first_time).toISOString(), i18n.language)}</span>
          <span className="font-mono">{formatLatency(summary.total_latency_ms)} {t('traces.total_span')}</span>
          <span>{formatDate(new Date(summary.last_time).toISOString(), i18n.language)}</span>
        </div>
      )}

      {/* Log detail Sheet */}
      <LogDetail
        log={selectedLog}
        onClose={() => setSelectedLog(null)}
        onLogChange={(updated) => {
          setSelectedLog(updated)
          setDetail(prev => {
            if (!prev) return prev
            return {
              ...prev,
              requests: prev.requests.map(r => r.id === updated.id ? updated : r),
            }
          })
        }}
      />
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  label,
  value,
  accent,
}: {
  icon: typeof Layers
  label: string
  value: string
  accent: string
}) {
  return (
    <div className="rounded-xl border border-border/50 bg-card/20 p-3 space-y-1">
      <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
        <Icon className={cn('h-3 w-3', accent)} />
        {label}
      </div>
      <div className={cn('text-lg font-mono font-bold', accent)}>{value}</div>
    </div>
  )
}
