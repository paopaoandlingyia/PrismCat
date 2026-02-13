import { cn } from '@/lib/utils'
import { Search, RotateCcw, ChevronLeft, ChevronRight } from 'lucide-react'
import type { Upstream, LogFilter } from '@/lib/api'
import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { DateRangePicker } from './DateRangePicker'
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Badge } from "@/components/ui/badge"

interface LogFiltersProps {
    filter: LogFilter
    onSearch: (filter: LogFilter) => void
    upstreams: Upstream[]
    total: number
    loading?: boolean
}

const DEFAULT_FILTER: LogFilter = { limit: 50, offset: 0 }

export function LogFilters({
    filter,
    onSearch,
    upstreams,
    total,
    loading,
}: LogFiltersProps) {
    const { t } = useTranslation()

    // 本地暂存的筛选条件（不触发查询）
    const [draft, setDraft] = useState<LogFilter>(() => ({ ...filter }))

    // 当外部 filter 变化时同步到 draft（例如分页或重置后）
    useEffect(() => {
        setDraft({ ...filter })
    }, [filter])

    // 提交查询
    const handleSearch = () => {
        onSearch({ ...draft, offset: 0 })
    }

    // 重置所有条件并立即触发查询
    const handleReset = () => {
        const resetFilter = { ...DEFAULT_FILTER }
        setDraft(resetFilter)
        onSearch(resetFilter)
    }

    // 分页计算
    const pageSize = filter.limit || 50
    const currentPage = Math.floor((filter.offset || 0) / pageSize) + 1
    const totalPages = Math.ceil(total / pageSize)

    const goToPage = (page: number) => {
        onSearch({ ...filter, offset: (page - 1) * pageSize })
    }

    // 检查各个字段是否有未提交的更改
    const isPathChanged = (draft.path || '') !== (filter.path || '')
    const isUpstreamChanged = (draft.upstream || '') !== (filter.upstream || '')
    const isMethodChanged = (draft.method || '') !== (filter.method || '')
    const isTimeChanged = (draft.start_time || '') !== (filter.start_time || '') ||
        (draft.end_time || '') !== (filter.end_time || '')
    const hasChanges = isPathChanged || isUpstreamChanged || isMethodChanged || isTimeChanged

    return (
        <div className="flex flex-col gap-4 px-4 pr-6 py-2">
            {/* 第一行：筛选条件 */}
            <div className="flex flex-wrap items-center gap-4">
                {/* 搜索框 */}
                <div className="relative flex-1 min-w-[240px] max-w-sm">
                    <div className="relative">
                        <Input
                            placeholder={t('filters.search_path')}
                            value={draft.path || ''}
                            onChange={(e) => setDraft({ ...draft, path: e.target.value })}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') {
                                    handleSearch()
                                }
                            }}
                            className={cn(
                                "h-10 border-border/50 bg-background/50 transition-all",
                                isPathChanged && "border-primary/50 ring-1 ring-primary/20"
                            )}
                        />
                        {isPathChanged && (
                            <Badge className="absolute right-2 top-2 h-6 px-1.5 text-[9px] font-black uppercase bg-primary/20 text-primary border-none">
                                Edited
                            </Badge>
                        )}
                    </div>
                </div>

                <Separator orientation="vertical" className="h-6 bg-border/40 hidden md:block" />

                {/* 上游筛选 */}
                <Select
                    value={draft.upstream || "all"}
                    onValueChange={(val) => setDraft({ ...draft, upstream: val === "all" ? "" : val })}
                >
                    <SelectTrigger className={cn(
                        "w-[160px] h-10 bg-background/50 border-border/50",
                        isUpstreamChanged && "border-primary/50 ring-1 ring-primary/20"
                    )}>
                        <SelectValue placeholder={t('filters.all_upstreams')} />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">{t('filters.all_upstreams')}</SelectItem>
                        {upstreams.map((up) => (
                            <SelectItem key={up.name} value={up.name} className="uppercase font-bold text-xs tracking-tight">
                                {up.name}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                {/* 方法筛选 */}
                <Select
                    value={draft.method || "all"}
                    onValueChange={(val) => setDraft({ ...draft, method: val === "all" ? "" : val })}
                >
                    <SelectTrigger className={cn(
                        "w-[120px] h-10 bg-background/50 border-border/50",
                        isMethodChanged && "border-primary/50 ring-1 ring-primary/20"
                    )}>
                        <SelectValue placeholder={t('filters.all_methods')} />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">{t('filters.all_methods')}</SelectItem>
                        {["GET", "POST", "PUT", "DELETE", "PATCH"].map((m) => (
                            <SelectItem key={m} value={m}>{m}</SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </div>

            {/* 第二行：时间范围 + 操作按钮 */}
            <div className="flex flex-wrap items-center justify-between gap-4">
                {/* 时间范围选择器 */}
                <div className={cn(
                    "rounded-lg transition-all",
                    isTimeChanged && "ring-2 ring-primary/20 border-primary/40"
                )}>
                    <DateRangePicker
                        value={{ startTime: draft.start_time, endTime: draft.end_time }}
                        onChange={({ startTime, endTime }) => {
                            setDraft({ ...draft, start_time: startTime, end_time: endTime })
                        }}
                    />
                </div>

                <div className="flex items-center gap-2 ml-auto">
                    {/* 查询按钮 */}
                    <Button
                        variant="default"
                        size="sm"
                        onClick={handleSearch}
                        disabled={loading}
                        className={cn(
                            "h-10 px-6 font-bold transition-all shadow-lg",
                            hasChanges
                                ? "bg-primary hover:bg-primary/90 shadow-primary/20 scale-105"
                                : "bg-primary/80 hover:bg-primary shadow-primary/10"
                        )}
                    >
                        <Search className={cn("h-4 w-4 mr-2", loading && "animate-spin")} />
                        <span>{t('filters.search')}</span>
                    </Button>

                    {/* 重置按钮 */}
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={handleReset}
                        className="text-muted-foreground hover:text-foreground h-10 px-4 border-border/50 bg-background/50"
                    >
                        <RotateCcw className="h-4 w-4 mr-2" />
                        <span>{t('filters.reset')}</span>
                    </Button>
                </div>
            </div>

            {/* 分页 */}
            <div className="flex items-center justify-between border-t border-border/40 pt-4">
                <div className="flex items-center gap-2">
                    <span className="text-[11px] font-bold uppercase tracking-widest text-muted-foreground/60">
                        {t('filters.total_count', { count: total })}
                    </span>
                    {total > 0 && (
                        <Badge variant="outline" className="text-[9px] border-border/50 text-muted-foreground/50">
                            {pageSize} / PAGE
                        </Badge>
                    )}
                </div>

                <div className="flex items-center gap-3">
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md border-border/40 hover:bg-primary hover:text-primary-foreground transition-all"
                        onClick={() => goToPage(currentPage - 1)}
                        disabled={currentPage <= 1}
                    >
                        <ChevronLeft className="h-4 w-4" />
                    </Button>

                    <div className="flex items-center h-8 px-4 rounded-md border border-border/40 bg-background/50 font-mono text-xs font-bold text-foreground/80">
                        <span className="text-primary">{currentPage}</span>
                        <span className="mx-2 text-muted-foreground/30">/</span>
                        <span>{totalPages || 1}</span>
                    </div>

                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md border-border/40 hover:bg-primary hover:text-primary-foreground transition-all"
                        onClick={() => goToPage(currentPage + 1)}
                        disabled={currentPage >= totalPages}
                    >
                        <ChevronRight className="h-4 w-4" />
                    </Button>
                </div>
            </div>
        </div>
    )
}
