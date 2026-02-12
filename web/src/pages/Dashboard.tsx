import { useEffect, useState, useCallback, useRef } from 'react'
import { fetchLogs, fetchLog, fetchStats, fetchUpstreams } from '@/lib/api'
import type { RequestLog, LogStats, Upstream, LogFilter, LogListResponse } from '@/lib/api'
import { StatsCards } from '@/components/StatsCards'
import { LogTable } from '@/components/LogTable'
import { LogDetail } from '@/components/LogDetail'
import { LogFilters } from '@/components/LogFilters'
import { useTranslation } from 'react-i18next'

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
    const [filter, setFilter] = useState<LogFilter>({ limit: 50, offset: 0 })
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
                setSelectedLog(full)
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

    return (
        <>
            {/* 统计卡片 */}
            <section>
                <StatsCards stats={stats} loading={loading && !stats} />
            </section>

            {/* 日志区域 */}
            <section className="space-y-4">
                {/* 过滤器 */}
                <LogFilters
                    filter={filter}
                    onSearch={setFilter}
                    upstreams={upstreams}
                    total={total}
                    loading={loading}
                />

                {/* 日志列表 */}
                <div className="rounded-xl border border-border bg-card/50 backdrop-blur-sm overflow-hidden">
                    <div className="p-4">
                        <LogTable
                            logs={logs}
                            loading={loading}
                            onSelect={handleSelectLog}
                            selectedId={selectedLog?.id}
                        />
                    </div>
                </div>
            </section>

            {/* 日志详情侧边栏 */}
            <LogDetail
                log={selectedLog}
                loading={selectedLogLoading}
                onClose={handleCloseLog}
            />
        </>
    )
}
