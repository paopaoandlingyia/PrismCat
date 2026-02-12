import { cn } from '@/lib/utils'
import { Activity, Box, Zap, Clock, AlertCircle, CheckCircle } from 'lucide-react'
import type { LogStats } from '@/lib/api'
import { formatLatency } from '@/lib/utils'
import { useTranslation } from 'react-i18next'
import { Card, CardContent } from '@/components/ui/card'

interface StatsCardsProps {
    stats: LogStats | null
    loading?: boolean
}

export function StatsCards({ stats, loading }: StatsCardsProps) {
    const { t } = useTranslation()
    const cards = [
        {
            title: t('stats.total_requests'),
            value: stats?.total_requests ?? 0,
            icon: Activity,
            gradient: 'from-blue-500 to-cyan-500',
            iconColor: 'text-blue-500 dark:text-blue-400',
            bgColor: 'bg-blue-500/10',
        },
        {
            title: t('common.success', '成功'),
            value: stats?.success_count ?? 0,
            icon: CheckCircle,
            gradient: 'from-green-500 to-emerald-500',
            iconColor: 'text-green-500 dark:text-green-400',
            bgColor: 'bg-green-500/10',
        },
        {
            title: t('common.error', '错误'),
            value: stats?.error_count ?? 0,
            icon: AlertCircle,
            gradient: 'from-red-500 to-orange-500',
            iconColor: 'text-red-500 dark:text-red-400',
            bgColor: 'bg-red-500/10',
        },
        {
            title: t('log_detail.streaming', '流式'),
            value: stats?.streaming_count ?? 0,
            icon: Zap,
            gradient: 'from-purple-500 to-pink-500',
            iconColor: 'text-purple-500 dark:text-purple-400',
            bgColor: 'bg-purple-500/10',
        },
        {
            title: t('stats.avg_latency'),
            value: formatLatency(stats?.avg_latency_ms ?? 0),
            icon: Clock,
            gradient: 'from-yellow-500 to-orange-500',
            iconColor: 'text-yellow-600 dark:text-yellow-400',
            bgColor: 'bg-yellow-500/10',
            isText: true,
        },
        {
            title: t('log_table.upstream', '上游数量'),
            value: Object.keys(stats?.by_upstream ?? {}).length,
            icon: Box,
            gradient: 'from-indigo-500 to-violet-500',
            iconColor: 'text-indigo-500 dark:text-indigo-400',
            bgColor: 'bg-indigo-500/10',
        },
    ]

    return (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
            {cards.map((card) => (
                <Card
                    key={card.title}
                    className={cn(
                        'relative overflow-hidden',
                        'border-border/50 bg-card/50 backdrop-blur-sm',
                        loading && 'animate-pulse'
                    )}
                >
                    <CardContent className="p-5">
                        {/* 背景装饰光晕 */}
                        <div className={cn(
                            'absolute -right-4 -top-4 h-16 w-16 rounded-full blur-[32px] opacity-20 dark:opacity-10',
                            card.bgColor
                        )} />

                        <div className="relative z-10">
                            <div className="flex items-center gap-2.5 mb-3">
                                <div className={cn('p-2 rounded-lg bg-black/5 dark:bg-white/5')}>
                                    <card.icon className={cn('h-4 w-4', card.iconColor)} />
                                </div>
                                <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider">{card.title}</span>
                            </div>
                            <div className={cn(
                                'text-2xl font-black tracking-tight',
                                `bg-gradient-to-br ${card.gradient} bg-clip-text text-transparent`
                            )}>
                                {card.isText ? card.value : card.value.toLocaleString()}
                            </div>
                        </div>
                    </CardContent>
                </Card>
            ))}
        </div>
    )
}
