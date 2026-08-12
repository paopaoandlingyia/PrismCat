import { cn } from '@/lib/utils'
import type { LogStats } from '@/lib/api'
import { formatLatency } from '@/lib/utils'
import { useTranslation } from 'react-i18next'

interface StatsCardsProps {
    stats: LogStats | null
    loading?: boolean
}

export function StatsCards({ stats, loading }: StatsCardsProps) {
    const { t } = useTranslation()

    // 只有错误数带语义色,其余保持中性 —— 一屏一个焦点
    const cards = [
        { title: t('stats.total_requests'), value: (stats?.total_requests ?? 0).toLocaleString() },
        { title: t('common.success'), value: (stats?.success_count ?? 0).toLocaleString() },
        { title: t('common.error'), value: (stats?.error_count ?? 0).toLocaleString(), tone: 'danger' as const },
        { title: t('log_detail.streaming'), value: (stats?.streaming_count ?? 0).toLocaleString() },
        { title: t('stats.avg_latency'), value: formatLatency(stats?.avg_latency_ms ?? 0) },
        { title: t('log_table.upstream'), value: Object.keys(stats?.by_upstream ?? {}).length.toLocaleString() },
    ]

    return (
        <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3 lg:grid-cols-6">
            {cards.map((card) => (
                <div key={card.title} className={cn('bg-card px-3 py-2', loading && 'animate-pulse')}>
                    <div className="text-xs text-muted-foreground">{card.title}</div>
                    <div
                        className={cn(
                            'mt-0.5 text-xl tabular-nums',
                            card.tone === 'danger' && (stats?.error_count ?? 0) > 0 ? 'text-danger' : 'text-foreground',
                        )}
                    >
                        {card.value}
                    </div>
                </div>
            ))}
        </div>
    )
}
