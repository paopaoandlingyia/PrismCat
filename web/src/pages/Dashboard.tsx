import { Suspense, lazy, startTransition, useEffect, useState, useCallback, useRef } from 'react'
import { fetchLogs, fetchLog, fetchStats, fetchUpstreams } from '@/lib/api'
import type { RequestLog, LogStats, Upstream, LogFilter, LogListResponse } from '@/lib/api'
import { StatsCards } from '@/components/StatsCards'
import { LogTable } from '@/components/LogTable'
import { LogFilters } from '@/components/LogFilters'
import { useTranslation } from 'react-i18next'

const LogDetailPanel = lazy(async () => {
    const module = await import('@/components/LogDetail')
    return { default: module.LogDetail }
})

export function Dashboard() {
    const { t } = useTranslation()

    // 状态
    const [logs, setLogs] = useState<RequestLog[]>([])
    const [stats, setStats] = useState<LogStats | null>(null)
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [total, setTotal] = useState(0)
    const [loading, setLoading] = useState(true)
    const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null)
    const [selectedLogLoading, setSelectedLogLoading] = useState(false)
    const [filter, setFilter] = useState<LogFilter>({ limit: 20, offset: 0 })
    const selectSeq = useRef(0)

    // 加载日志
    const loadLogs = useCallback(async () => {
        setLoading(true)
        try {
            const data: LogListResponse = await fetchLogs(filter)
            setLogs(data.logs || [])
            setTotal(data.total)
        } catch (err) {
            console.error('[Dashboard] Failed to load logs:', err)
        } finally {
            setLoading(false)
        }
    }, [filter])

    // 加载统计
    const loadStats = useCallback(async () => {
        try {
            const data = await fetchStats()
            setStats(data)
        } catch (err) {
            console.error('[Dashboard] Failed to load stats:', err)
        }
    }, [])

    // 加载上游配置
    const loadUpstreams = useCallback(async () => {
        try {
            const data = await fetchUpstreams()
            setUpstreams(data || [])
        } catch (err) {
            console.error('[Dashboard] Failed to load upstreams:', err)
        }
    }, [])

    // 初始加载
    useEffect(() => {
        loadUpstreams()
        loadStats()
    }, [loadUpstreams, loadStats])

    // 过滤条件变化时重新加载
    useEffect(() => {
        loadLogs()
    }, [loadLogs])

    const handleSelectLog = useCallback(async (log: RequestLog) => {
        setSelectedLog(log)
        setSelectedLogLoading(true)
        const seq = ++selectSeq.current
        try {
            const full = await fetchLog(log.id)
            if (selectSeq.current === seq) {
                startTransition(() => {
                    setSelectedLog(full)
                })
            }
        } catch (err) {
            console.error(t('app.load_log_detail_failed') + ':', err)
        } finally {
            if (selectSeq.current === seq) {
                setSelectedLogLoading(false)
            }
        }
    }, [t])

    const handleCloseLog = useCallback(() => {
        selectSeq.current++
        setSelectedLog(null)
        setSelectedLogLoading(false)
    }, [])

    const handleLogChange = useCallback((nextLog: RequestLog) => {
        setSelectedLog(nextLog)
        setLogs(current => current
            .map(item => item.id === nextLog.id ? { ...item, annotation: nextLog.annotation } : item)
            .filter(item => item.id !== nextLog.id || matchesAnnotationFilter(nextLog, filter)))
    }, [filter])

    const logDetailFallback = selectedLog ? (
        <div className="fixed inset-y-0 right-0 z-50 w-full border-l border-border/60 bg-background shadow-2xl sm:max-w-2xl">
            <div className="flex h-full flex-col items-center justify-center gap-4">
                <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
                <div className="text-sm font-medium text-muted-foreground">
                    {t('common.loading')}
                </div>
            </div>
        </div>
    ) : null

    return (
        <div className="flex flex-col gap-6 md:gap-8">
            {/* 统计卡片 */}
            <section>
                <StatsCards stats={stats} loading={loading && !stats} />
            </section>

            {/* 日志区域 - 包含过滤器和表格 */}
            <section className="flex flex-col gap-4">
                {/* 过滤器 */}
                <div className="bg-muted/30 rounded-xl p-2 md:p-3">
                    <LogFilters
                        filter={filter}
                        onSearch={setFilter}
                        upstreams={upstreams}
                        total={total}
                        loading={loading}
                    />
                </div>

                {/* 日志列表 */}
                <div className="rounded-2xl overflow-hidden shadow-sm border border-border bg-card">
                    <LogTable
                        logs={logs}
                        loading={loading}
                        onSelect={handleSelectLog}
                        selectedId={selectedLog?.id}
                    />
                </div>
            </section>

            {/* 日志详情侧边栏 */}
            <Suspense fallback={logDetailFallback}>
                <LogDetailPanel
                    log={selectedLog}
                    loading={selectedLogLoading}
                    onClose={handleCloseLog}
                    onLogChange={handleLogChange}
                />
            </Suspense>
        </div>
    )
}

function matchesAnnotationFilter(log: RequestLog, filter: LogFilter) {
    const annotation = log.annotation ?? { saved: false, status: 'none', labels: [] }
    if (typeof filter.saved === 'boolean' && annotation.saved !== filter.saved) return false
    if (filter.annotation_status && annotation.status !== filter.annotation_status) return false
    if (filter.annotation_label && !(annotation.labels ?? []).includes(filter.annotation_label)) return false
    return true
}
