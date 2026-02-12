import { cn, formatDate, formatLatency, getStatusColor, getMethodColor } from '@/lib/utils'
import { Zap } from 'lucide-react'
import type { RequestLog } from '@/lib/api'
import { useTranslation } from 'react-i18next'
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip"

interface LogTableProps {
    logs: RequestLog[]
    loading?: boolean
    onSelect: (log: RequestLog) => void
    selectedId?: string
}

export function LogTable({ logs, loading, onSelect, selectedId }: LogTableProps) {
    const { t, i18n } = useTranslation()

    if (loading) {
        return (
            <div className="rounded-xl border border-border/50 overflow-hidden bg-card/30">
                <Table>
                    <TableHeader className="bg-muted/50">
                        <TableRow>
                            <TableHead className="w-[80px]">{t('log_table.method')}</TableHead>
                            <TableHead className="w-[70px]">{t('log_table.status')}</TableHead>
                            <TableHead className="w-[100px]">{t('log_table.upstream')}</TableHead>
                            <TableHead>{t('log_table.path')}</TableHead>
                            <TableHead className="w-[80px] text-right">{t('log_table.latency')}</TableHead>
                            <TableHead className="w-[160px] text-right">{t('log_table.time')}</TableHead>
                            <TableHead className="w-[100px]"></TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {Array.from({ length: 8 }).map((_, i) => (
                            <TableRow key={i}>
                                {Array.from({ length: 7 }).map((_, j) => (
                                    <TableCell key={j}>
                                        <Skeleton className="h-5 w-full bg-muted/50" />
                                    </TableCell>
                                ))}
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>
        )
    }

    if (logs.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-24 text-muted-foreground bg-card/20 rounded-2xl border border-dashed border-border/50">
                <div className="text-6xl mb-6 grayscale opacity-50">📭</div>
                <div className="text-xl font-semibold tracking-tight text-foreground/70">{t('log_table.no_logs')}</div>
                <p className="text-sm mt-2 max-w-[280px] text-center leading-relaxed font-medium opacity-60">
                    {t('log_table.send_requests_hint', '发送一些请求后这里会显示日志')}
                </p>
            </div>
        )
    }

    return (
        <div className="rounded-xl border border-border/40 overflow-hidden bg-card/30 backdrop-blur-sm">
            <Table>
                <TableHeader className="bg-muted/30">
                    <TableRow className="hover:bg-transparent">
                        <TableHead className="w-[80px] font-bold text-[11px] uppercase tracking-tighter">{t('log_table.method')}</TableHead>
                        <TableHead className="w-[70px] font-bold text-[11px] uppercase tracking-tighter text-center">{t('log_table.status')}</TableHead>
                        <TableHead className="w-[100px] font-bold text-[11px] uppercase tracking-tighter">{t('log_table.upstream')}</TableHead>
                        <TableHead className="font-bold text-[11px] uppercase tracking-tighter">{t('log_table.path')}</TableHead>
                        <TableHead className="w-[100px] font-bold text-[11px] uppercase tracking-tighter text-right">{t('log_table.latency')}</TableHead>
                        <TableHead className="w-[180px] font-bold text-[11px] uppercase tracking-tighter text-right">{t('log_table.time')}</TableHead>
                        <TableHead className="w-[100px]"></TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {logs.map((log) => (
                        <TableRow
                            key={log.id}
                            className={cn(
                                "transition-colors border-border/20",
                                selectedId === log.id ? "bg-primary/10 hover:bg-primary/15" : "hover:bg-muted/40"
                            )}
                        >
                            <TableCell>
                                <div
                                    className={cn(
                                        "w-[50px] py-0.5 rounded-[3px] text-[10px] text-center uppercase font-bold border",
                                        getMethodColor(log.method)
                                    )}
                                >
                                    {log.method}
                                </div>
                            </TableCell>
                            <TableCell className="text-center">
                                <span className={cn(
                                    "font-mono text-xs font-bold",
                                    getStatusColor(log.status_code)
                                )}>
                                    {log.status_code || '---'}
                                </span>
                            </TableCell>
                            <TableCell>
                                <span className="text-[10px] font-black uppercase tracking-tighter text-muted-foreground/60 truncate block max-w-[90px]">
                                    {log.upstream}
                                </span>
                            </TableCell>
                            <TableCell className="max-w-0">
                                <div className="flex items-center gap-2">
                                    <span className="truncate font-mono text-xs text-foreground/90 select-text">
                                        {log.path}
                                        {log.query && <span className="text-muted-foreground/50">?{log.query}</span>}
                                    </span>
                                    {log.streaming && (
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <div className="shrink-0 animate-pulse">
                                                    <Zap className="h-3 w-3 text-purple-500 fill-purple-500/20" />
                                                </div>
                                            </TooltipTrigger>
                                            <TooltipContent side="right">
                                                <p className="text-[10px] font-bold uppercase">{t('log_detail.streaming', '流式响应')}</p>
                                            </TooltipContent>
                                        </Tooltip>
                                    )}
                                </div>
                            </TableCell>
                            <TableCell className="text-right">
                                <span className="text-xs text-muted-foreground font-mono font-medium">
                                    {formatLatency(log.latency_ms)}
                                </span>
                            </TableCell>
                            <TableCell className="text-right">
                                <span className="text-[11px] text-muted-foreground/60 font-medium">
                                    {formatDate(log.created_at, i18n.language)}
                                </span>
                            </TableCell>
                            <TableCell>
                                <div className="flex justify-end transition-opacity">
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className={cn(
                                            "h-7 text-[11px] font-black px-6 min-w-[80px] rounded-md transition-all active:scale-95",
                                            selectedId === log.id
                                                ? "bg-primary text-primary-foreground"
                                                : "text-muted-foreground hover:bg-primary hover:text-primary-foreground dark:hover:bg-primary dark:hover:text-primary-foreground"
                                        )}
                                        onClick={() => onSelect(log)}
                                    >
                                        {t('common.details')}
                                    </Button>
                                </div>
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    )
}
