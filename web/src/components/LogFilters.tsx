import { cn } from '@/lib/utils'
import { Search, RotateCcw, ChevronDown, ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight, Download, SlidersHorizontal } from 'lucide-react'
import type { Upstream, LogFilter } from '@/lib/api'
import { Suspense, lazy, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { Badge } from "@/components/ui/badge"
import { useArchiveFeatureEnabled } from '@/lib/archiveFeature'
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"

interface LogFiltersProps {
    filter: LogFilter
    onSearch: (filter: LogFilter) => void
    onExport?: (filter: LogFilter) => void
    upstreams: Upstream[]
    total: number
    loading?: boolean
}

const DEFAULT_FILTER: LogFilter = { limit: 20, offset: 0 }

const DateRangePicker = lazy(async () => {
    const module = await import('./DateRangePicker')
    return { default: module.DateRangePicker }
})

function DateRangePickerFallback() {
    return (
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
            <div className="h-8 rounded-md border border-border bg-background sm:min-w-[170px]" />
            <span className="hidden text-muted-foreground/30 text-sm font-medium mx-1 sm:inline">/</span>
            <div className="h-8 rounded-md border border-border bg-background sm:min-w-[170px]" />
        </div>
    )
}

export function LogFilters({
    filter,
    onSearch,
    onExport,
    upstreams,
    total,
    loading,
}: LogFiltersProps) {
    const { t } = useTranslation()
    const archiveEnabled = useArchiveFeatureEnabled()

    // 本地暂存的筛选条件（不触发查询）
    const [draftState, setDraftState] = useState(() => ({
        source: filter,
        draft: { ...filter },
    }))
    let currentDraftState = draftState
    if (draftState.source !== filter) {
        const nextDraftState = {
            source: filter,
            draft: { ...filter },
        }
        setDraftState(nextDraftState)
        currentDraftState = nextDraftState
    }
    const draft = currentDraftState.draft
    const setDraft = (nextDraft: LogFilter) => {
        setDraftState((current) => ({
            ...current,
            draft: nextDraft,
        }))
    }

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

    const handleExport = () => {
        onExport?.({ ...draft, offset: 0 })
    }

    // 分页计算
    const pageSize = filter.limit || 50
    const currentPage = Math.floor((filter.offset || 0) / pageSize) + 1
    const totalPages = Math.max(1, Math.ceil(total / pageSize))
    const [pageDraft, setPageDraft] = useState(String(currentPage))

    useEffect(() => {
        setPageDraft(String(currentPage))
    }, [currentPage])

    const goToPage = (page: number) => {
        const nextPage = Math.min(totalPages, Math.max(1, page))
        onSearch({ ...filter, offset: (nextPage - 1) * pageSize })
    }

    const commitPageDraft = () => {
        const parsed = Number.parseInt(pageDraft, 10)
        if (!Number.isFinite(parsed)) {
            setPageDraft(String(currentPage))
            return
        }
        goToPage(parsed)
    }

    // 检查各个字段是否有未提交的更改
    const isPathChanged = (draft.path || '') !== (filter.path || '')
    const isUpstreamChanged = (draft.upstream || '') !== (filter.upstream || '')
    const isMethodChanged = (draft.method || '') !== (filter.method || '')
    const isStatusCodeChanged = (draft.status_code || 0) !== (filter.status_code || 0)
    const isTraceIdChanged = (draft.trace_id || '') !== (filter.trace_id || '')
    const isTagChanged = (draft.tag || '') !== (filter.tag || '')
    const isSavedChanged = (draft.saved ?? undefined) !== (filter.saved ?? undefined)
    const isAnnotationStatusChanged = (draft.annotation_status || '') !== (filter.annotation_status || '')
    const isAnnotationLabelChanged = (draft.annotation_label || '') !== (filter.annotation_label || '')
    const isBackupStatusChanged = archiveEnabled && (draft.backup_status || '') !== (filter.backup_status || '')
    const isTimeChanged = (draft.start_time || '') !== (filter.start_time || '') ||
        (draft.end_time || '') !== (filter.end_time || '')
    const hasChanges = isPathChanged || isUpstreamChanged || isMethodChanged || isStatusCodeChanged || isTraceIdChanged || isTagChanged ||
        isSavedChanged || isAnnotationStatusChanged || isAnnotationLabelChanged || isBackupStatusChanged || isTimeChanged

    // 次级筛选默认收起,但只要有生效的条件就展开,避免"筛了却看不见"
    const activeAdvancedCount = [
        draft.upstream, draft.method, draft.status_code, draft.tag,
        draft.saved, draft.annotation_status, draft.annotation_label, archiveEnabled ? draft.backup_status : undefined,
    ].filter(value => value !== undefined && value !== '').length
    const [showAdvanced, setShowAdvanced] = useState(activeAdvancedCount > 0)

    return (
        <div className="flex flex-col gap-2 px-0 py-2 sm:px-4 sm:pr-6">
            {/* 工具条：搜索 + 时间 + 筛选开关 + 操作 */}
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <div className="relative flex-1 group">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground/75 transition-colors group-focus-within:text-primary" />
                    <Input
                        placeholder={t('filters.search_path')}
                        value={draft.path || ''}
                        onChange={(e) => setDraft({ ...draft, path: e.target.value })}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') handleSearch()
                        }}
                        className={cn(
                            "h-8 pl-9 border border-input bg-background transition-all hover:bg-accent focus-visible:bg-background",
                            isPathChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}
                    />
                </div>

                <div className={cn(
                    "w-full sm:w-auto rounded-md transition-all",
                    isTimeChanged && "ring-1 ring-primary/20"
                )}>
                    <Suspense fallback={<DateRangePickerFallback />}>
                        <DateRangePicker
                            value={{ startTime: draft.start_time, endTime: draft.end_time }}
                            onChange={({ startTime, endTime }) => {
                                setDraft({ ...draft, start_time: startTime, end_time: endTime })
                            }}
                        />
                    </Suspense>
                </div>

                <button
                    type="button"
                    onClick={() => setShowAdvanced(value => !value)}
                    className={cn(
                        'flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-input px-2.5 text-xs transition-colors hover:bg-accent',
                        activeAdvancedCount > 0 ? 'text-foreground' : 'text-muted-foreground',
                    )}
                >
                    <SlidersHorizontal className="h-3.5 w-3.5" />
                    <span>{t('filters.advanced')}</span>
                    {activeAdvancedCount > 0 && (
                        <span className="rounded-sm bg-primary/10 px-1 font-mono text-primary">{activeAdvancedCount}</span>
                    )}
                    <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', showAdvanced && 'rotate-180')} />
                </button>

                <div className="flex shrink-0 items-center gap-1.5">
                    <TooltipProvider delayDuration={200}>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <Button
                                    variant={hasChanges ? 'default' : 'outline'}
                                    size="icon"
                                    onClick={handleSearch}
                                    disabled={loading}
                                    className={cn(
                                        'h-8 w-8 shrink-0',
                                        // 实心强调色只在有未提交筛选时出现,让这块颜色代表"有东西待应用"
                                        !hasChanges && 'border border-input bg-background text-muted-foreground hover:bg-accent hover:text-foreground',
                                    )}
                                >
                                    <Search className={cn('h-4 w-4', loading && 'animate-spin')} />
                                </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                                <p>{t('filters.search')}</p>
                            </TooltipContent>
                        </Tooltip>

                        {onExport && (
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <Button
                                        variant="outline"
                                        size="icon"
                                        onClick={handleExport}
                                        className="h-8 w-8 shrink-0 border border-input bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                                    >
                                        <Download className="h-4 w-4" />
                                    </Button>
                                </TooltipTrigger>
                                <TooltipContent>
                                    <p>{t('filters.export_jsonl')}</p>
                                </TooltipContent>
                            </Tooltip>
                        )}

                        <Tooltip>
                            <TooltipTrigger asChild>
                                <Button
                                    variant="outline"
                                    size="icon"
                                    onClick={handleReset}
                                    className="h-8 w-8 shrink-0 border border-input bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                                >
                                    <RotateCcw className="h-4 w-4" />
                                </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                                <p>{t('filters.reset')}</p>
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
            </div>

            {showAdvanced && (
                <div className="grid grid-cols-2 gap-2 md:grid-cols-4 xl:grid-cols-8">
                    <Select
                        value={draft.upstream || "all"}
                        onValueChange={(val) => setDraft({ ...draft, upstream: val === "all" ? "" : val })}
                    >
                        <SelectTrigger className={cn(
                            "w-full h-8 bg-background border border-input hover:bg-accent",
                            isUpstreamChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}>
                            <SelectValue placeholder={t('filters.all_upstreams')} />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">{t('filters.all_upstreams')}</SelectItem>
                            {upstreams.map((up) => (
                                <SelectItem key={up.name} value={up.name} className="font-semibold text-xs">
                                    {up.name}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>

                    <Select
                        value={draft.method || "all"}
                        onValueChange={(val) => setDraft({ ...draft, method: val === "all" ? "" : val })}
                    >
                        <SelectTrigger className={cn(
                            "w-full h-8 bg-background border border-input hover:bg-accent",
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

                    <Input
                        type="text"
                        placeholder={t('filters.status_code')}
                        value={draft.status_code || ''}
                        onChange={(e) => {
                            const val = e.target.value.replace(/\D/g, '').slice(0, 3)
                            setDraft({ ...draft, status_code: val ? Number(val) : undefined })
                        }}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') handleSearch()
                        }}
                        className={cn(
                            "w-full h-8 border border-input bg-background transition-all hover:bg-accent focus-visible:bg-background",
                            isStatusCodeChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}
                    />

                    <Input
                        placeholder={t('filters.tag_placeholder')}
                        value={draft.tag || ''}
                        onChange={(e) => setDraft({ ...draft, tag: e.target.value })}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') handleSearch()
                        }}
                        className={cn(
                            "w-full h-8 border border-input bg-background transition-all hover:bg-accent focus-visible:bg-background",
                            isTagChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}
                    />

                    <Select
                        value={draft.saved === true ? 'saved' : draft.saved === false ? 'unsaved' : 'all'}
                        onValueChange={(val) => setDraft({
                            ...draft,
                            saved: val === 'all' ? undefined : val === 'saved',
                        })}
                    >
                        <SelectTrigger className={cn(
                            "w-full h-8 bg-background border border-input hover:bg-accent",
                            isSavedChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}>
                            <SelectValue placeholder={t('filters.saved_all')} />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">{t('filters.saved_all')}</SelectItem>
                            <SelectItem value="saved">{t('filters.saved_only')}</SelectItem>
                            <SelectItem value="unsaved">{t('filters.unsaved_only')}</SelectItem>
                        </SelectContent>
                    </Select>

                    {archiveEnabled && <Select
                        value={draft.backup_status || 'all'}
                        onValueChange={(val) => setDraft({
                            ...draft,
                            backup_status: val === 'all' ? undefined : val as LogFilter['backup_status'],
                        })}
                    >
                        <SelectTrigger className={cn(
                            "w-full h-8 bg-background border border-input hover:bg-accent",
                            isBackupStatusChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}>
                            <SelectValue placeholder={t('filters.backup_all')} />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">{t('filters.backup_all')}</SelectItem>
                            <SelectItem value="pending">{t('filters.backup_pending')}</SelectItem>
                            <SelectItem value="verified">{t('filters.backup_verified')}</SelectItem>
                            <SelectItem value="restored">{t('filters.backup_restored')}</SelectItem>
                        </SelectContent>
                    </Select>}

                    <Select
                        value={draft.annotation_status || 'all'}
                        onValueChange={(val) => setDraft({ ...draft, annotation_status: val === 'all' ? undefined : val as LogFilter['annotation_status'] })}
                    >
                        <SelectTrigger className={cn(
                            "w-full h-8 bg-background border border-input hover:bg-accent",
                            isAnnotationStatusChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}>
                            <SelectValue placeholder={t('filters.annotation_status')} />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="all">{t('filters.annotation_status')}</SelectItem>
                            <SelectItem value="todo">{t('log_annotation.todo')}</SelectItem>
                            <SelectItem value="done">{t('log_annotation.done')}</SelectItem>
                        </SelectContent>
                    </Select>

                    <Input
                        placeholder={t('filters.annotation_label_placeholder')}
                        value={draft.annotation_label || ''}
                        onChange={(e) => setDraft({ ...draft, annotation_label: e.target.value })}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') handleSearch()
                        }}
                        className={cn(
                            "w-full h-8 border border-input bg-background transition-all hover:bg-accent focus-visible:bg-background",
                            isAnnotationLabelChanged && "border-primary/50 ring-1 ring-primary/20"
                        )}
                    />
                </div>
            )}

            {/* 分页 */}
            <div className="flex flex-col gap-3 border-t border-border pt-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-center gap-2">
                    <span className="text-xs font-medium text-muted-foreground/60">
                        {t('filters.total_count', { count: total })}
                    </span>
                    {total > 0 && (
                        <Badge variant="outline" className="text-xs border-border bg-background text-muted-foreground/75">
                            {t('filters.per_page', { count: pageSize })}
                        </Badge>
                    )}
                </div>

                <div className="flex w-full items-center justify-between gap-2 sm:w-auto sm:justify-end">
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md border border-border bg-background hover:bg-accent hover:text-accent-foreground transition-all"
                        onClick={() => goToPage(1)}
                        disabled={currentPage <= 1}
                        aria-label={t('filters.first_page')}
                    >
                        <ChevronsLeft className="h-4 w-4" />
                    </Button>
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md border border-border bg-background hover:bg-accent hover:text-accent-foreground transition-all"
                        onClick={() => goToPage(currentPage - 1)}
                        disabled={currentPage <= 1}
                    >
                        <ChevronLeft className="h-4 w-4" />
                    </Button>

                    <div className="flex items-center h-8 rounded-md border border-border bg-background px-2 font-mono text-xs font-medium text-foreground/80">
                        <Input
                            value={pageDraft}
                            inputMode="numeric"
                            aria-label={t('filters.page_number')}
                            onChange={e => setPageDraft(e.target.value.replace(/\D/g, '').slice(0, 5))}
                            onBlur={commitPageDraft}
                            onKeyDown={e => {
                                if (e.key === 'Enter') {
                                    e.currentTarget.blur()
                                }
                                if (e.key === 'Escape') {
                                    setPageDraft(String(currentPage))
                                    e.currentTarget.blur()
                                }
                            }}
                            className="h-6 w-10 border-0 bg-transparent p-0 text-center font-mono text-xs font-medium text-primary shadow-none focus-visible:ring-0"
                        />
                        <span className="mx-2 text-muted-foreground/30">/</span>
                        <span>{totalPages}</span>
                    </div>

                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md border border-border bg-background hover:bg-accent hover:text-accent-foreground transition-all"
                        onClick={() => goToPage(currentPage + 1)}
                        disabled={currentPage >= totalPages}
                    >
                        <ChevronRight className="h-4 w-4" />
                    </Button>
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 rounded-md border border-border bg-background hover:bg-accent hover:text-accent-foreground transition-all"
                        onClick={() => goToPage(totalPages)}
                        disabled={currentPage >= totalPages}
                        aria-label={t('filters.last_page')}
                    >
                        <ChevronsRight className="h-4 w-4" />
                    </Button>
                </div>
            </div>
        </div>
    )
}

