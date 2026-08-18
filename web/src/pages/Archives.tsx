import { useCallback, useEffect, useRef, useState } from 'react'
import {
    ArchiveRestore,
    ChevronLeft,
    ChevronRight,
    CloudDownload,
    RefreshCw,
    Search,
    Trash2,
    Upload,
    X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
    deleteArchiveImport,
    fetchArchiveImports,
    fetchArchiveJobs,
    fetchArchivePackages,
    fetchArchives,
    importArchiveDate,
    importArchiveObject,
    uploadArchive,
    type ArchiveBatch,
    type ArchiveDateType,
    type ArchiveImport,
    type ArchiveJob,
    type ArchivePage,
    type ArchiveStatus,
} from '@/lib/api'
import { getArchivePagination } from '@/lib/archiveHistory'
import { formatDate, formatSize } from '@/lib/utils'

type ArchiveTab = 'packages' | 'jobs' | 'imports'

const PAGE_SIZES = [20, 50, 100]

export function Archives() {
    const { t, i18n } = useTranslation()
    const [tab, setTab] = useState<ArchiveTab>('packages')
    const [status, setStatus] = useState<ArchiveStatus | null>(null)
    const [packages, setPackages] = useState<ArchivePage<ArchiveBatch> | null>(null)
    const [jobs, setJobs] = useState<ArchivePage<ArchiveJob> | null>(null)
    const [imports, setImports] = useState<ArchivePage<ArchiveImport> | null>(null)
    const [loading, setLoading] = useState(true)
    const [action, setAction] = useState('')
    const [pageSize, setPageSize] = useState(50)
    const [packageOffset, setPackageOffset] = useState(0)
    const [jobOffset, setJobOffset] = useState(0)
    const [importOffset, setImportOffset] = useState(0)
    const [dateType, setDateType] = useState<ArchiveDateType>('completed_at')
    const [date, setDate] = useState('')
    const [jobID, setJobID] = useState('')
    const [s3DialogOpen, setS3DialogOpen] = useState(false)
    const [s3Date, setS3Date] = useState(() => localDateValue(new Date()))
    const [s3Result, setS3Result] = useState<ArchiveStatus | null>(null)
    const [s3Loading, setS3Loading] = useState(false)
    const fileInput = useRef<HTMLInputElement>(null)

    const loadStatus = useCallback(async () => {
        setStatus(await fetchArchives(false))
    }, [])

    const loadPackages = useCallback(async () => {
        setPackages(await fetchArchivePackages({ dateType, date, jobId: jobID, offset: packageOffset, limit: pageSize }))
    }, [date, dateType, jobID, packageOffset, pageSize])

    const loadJobs = useCallback(async () => {
        setJobs(await fetchArchiveJobs(jobOffset, pageSize))
    }, [jobOffset, pageSize])

    const loadImports = useCallback(async () => {
        setImports(await fetchArchiveImports(importOffset, pageSize))
    }, [importOffset, pageSize])

    const loadActive = useCallback(async () => {
        setLoading(true)
        try {
            await Promise.all([
                loadStatus(),
                tab === 'packages' ? loadPackages() : tab === 'jobs' ? loadJobs() : loadImports(),
            ])
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t('common.error'))
        } finally {
            setLoading(false)
        }
    }, [loadImports, loadJobs, loadPackages, loadStatus, t, tab])

    useEffect(() => { void loadActive() }, [loadActive])

    const reloadImports = async () => {
        await Promise.all([loadStatus(), loadImports()])
    }

    const importObject = async (key: string) => {
        setAction(key)
        try {
            await importArchiveObject(key)
            toast.success(t('archives.import_complete'))
            await reloadImports()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t('common.error'))
        } finally {
            setAction('')
        }
    }

    const importDate = async () => {
        setAction('date')
        try {
            const restored = await importArchiveDate(s3Date)
            toast.success(t('archives.import_date_complete', { count: restored.length }))
            await reloadImports()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t('common.error'))
        } finally {
            setAction('')
        }
    }

    const importUpload = async (file: File) => {
        setAction('upload')
        try {
            await uploadArchive(file)
            toast.success(t('archives.import_complete'))
            await reloadImports()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t('common.error'))
        } finally {
            setAction('')
            if (fileInput.current) fileInput.current.value = ''
        }
    }

    const deleteImport = async (id: string) => {
        setAction(id)
        try {
            await deleteArchiveImport(id)
            toast.success(t('archives.import_deleted'))
            await loadImports()
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t('common.error'))
        } finally {
            setAction('')
        }
    }

    const searchS3 = async () => {
        setS3Loading(true)
        try {
            setS3Result(await fetchArchives(true, s3Date))
        } catch (err) {
            toast.error(err instanceof Error ? err.message : t('common.error'))
        } finally {
            setS3Loading(false)
        }
    }

    const showJobPackages = (id: string) => {
        setJobID(id)
        setDate('')
        setPackageOffset(0)
        setTab('packages')
    }

    const changePageSize = (value: string) => {
        setPageSize(Number(value))
        setPackageOffset(0)
        setJobOffset(0)
        setImportOffset(0)
    }

    return (
        <div className="mx-auto max-w-[1500px] space-y-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground">
                    <Badge variant="outline" className={status?.enabled ? 'text-success' : 'text-muted-foreground'}>
                        {status?.enabled ? t('archives.backup_enabled') : t('archives.backup_disabled')}
                    </Badge>
                    <span className="truncate font-mono text-xs">{status?.key_prefix ?? '-'}</span>
                </div>
                <div className="flex flex-wrap gap-2">
                    <Button type="button" variant="outline" size="sm" onClick={() => setS3DialogOpen(true)}>
                        <Search className="h-4 w-4" />{t('archives.find_s3')}
                    </Button>
                    <input ref={fileInput} type="file" accept=".zst,.tar.zst" className="hidden" onChange={event => {
                        const file = event.target.files?.[0]
                        if (file) void importUpload(file)
                    }} />
                    <Button type="button" variant="outline" size="sm" disabled={action !== ''} onClick={() => fileInput.current?.click()}>
                        <Upload className="h-4 w-4" />{t('archives.upload')}
                    </Button>
                    <Button type="button" variant="outline" size="icon-sm" disabled={loading} onClick={() => void loadActive()} title={t('common.refresh')}>
                        <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    </Button>
                </div>
            </div>

            <Tabs value={tab} onValueChange={value => setTab(value as ArchiveTab)}>
                <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
                    <TabsList variant="line">
                        <TabsTrigger value="packages">{t('archives.packages_tab')}</TabsTrigger>
                        <TabsTrigger value="jobs">{t('archives.jobs_tab')}</TabsTrigger>
                        <TabsTrigger value="imports">{t('archives.imports_tab')}</TabsTrigger>
                    </TabsList>
                    <Select value={String(pageSize)} onValueChange={changePageSize}>
                        <SelectTrigger size="sm" aria-label={t('archives.page_size')}><SelectValue /></SelectTrigger>
                        <SelectContent>{PAGE_SIZES.map(size => <SelectItem key={size} value={String(size)}>{t('archives.per_page', { count: size })}</SelectItem>)}</SelectContent>
                    </Select>
                </div>

                <TabsContent value="packages" className="space-y-4">
                    <div className="flex flex-wrap items-end gap-3 py-1">
                        <div className="space-y-1.5">
                            <div className="text-xs text-muted-foreground">{t('archives.date_filter_type')}</div>
                            <div className="inline-flex h-9 rounded-md border border-input bg-background p-0.5">
                                <Button type="button" size="sm" variant={dateType === 'completed_at' ? 'secondary' : 'ghost'} className="h-8" onClick={() => { setDateType('completed_at'); setPackageOffset(0) }}>
                                    {t('archives.backup_completed_date')}
                                </Button>
                                <Button type="button" size="sm" variant={dateType === 'archive_date' ? 'secondary' : 'ghost'} className="h-8" onClick={() => { setDateType('archive_date'); setPackageOffset(0) }}>
                                    {t('archives.log_date')}
                                </Button>
                            </div>
                        </div>
                        <div className="space-y-1.5">
                            <div className="text-xs text-muted-foreground">{t('archives.date_optional')}</div>
                            <div className="flex gap-1">
                                <Input type="date" value={date} onChange={event => { setDate(event.target.value); setPackageOffset(0) }} className="w-44" />
                                {date && <Button type="button" variant="ghost" size="icon" onClick={() => { setDate(''); setPackageOffset(0) }} title={t('archives.clear_date')}><X className="h-4 w-4" /></Button>}
                            </div>
                        </div>
                        {jobID && <Badge variant="secondary" className="mb-1 max-w-full gap-2 font-mono">{t('archives.job_filter')} {jobID}<button type="button" onClick={() => { setJobID(''); setPackageOffset(0) }} title={t('archives.clear_job_filter')}><X className="h-3 w-3" /></button></Badge>}
                    </div>
                    <HistoryTable empty={t('archives.no_packages')} count={packages?.items?.length ?? 0}>
                        <Table>
                            <TableHeader><TableRow>
                                <TableHead>{t('archives.log_date')}</TableHead>
                                <TableHead>{t('archives.backup_completed_at')}</TableHead>
                                <TableHead>{t('archives.trigger')}</TableHead>
                                <TableHead>{t('archives.logs')}</TableHead>
                                <TableHead>{t('archives.size')}</TableHead>
                                <TableHead>{t('archives.status')}</TableHead>
                                <TableHead>{t('archives.object')}</TableHead>
                                <TableHead className="w-16" />
                            </TableRow></TableHeader>
                            <TableBody>{(packages?.items ?? []).map(batch => (
                                <TableRow key={batch.id}>
                                    <TableCell className="whitespace-nowrap">{batch.archive_date}</TableCell>
                                    <TableCell className="whitespace-nowrap">{formatDate(batch.verified_at ?? batch.updated_at, i18n.language)}</TableCell>
                                    <TableCell>{t(`archives.trigger_${batch.trigger || 'unknown'}`)}</TableCell>
                                    <TableCell>{batch.log_count}</TableCell>
                                    <TableCell>{formatSize(batch.compressed_bytes)}</TableCell>
                                    <TableCell><StatusBadge status={batch.status} error={batch.error} /></TableCell>
                                    <TableCell className="max-w-[500px] break-all font-mono text-xs">{batch.object_key || '-'}</TableCell>
                                    <TableCell><Button type="button" variant="ghost" size="icon-sm" disabled={action !== '' || !batch.object_key || batch.status !== 'verified'} onClick={() => void importObject(batch.object_key || '')} title={t('archives.import')}><CloudDownload className="h-4 w-4" /></Button></TableCell>
                                </TableRow>
                            ))}</TableBody>
                        </Table>
                    </HistoryTable>
                    <Paginator page={packages} offset={packageOffset} setOffset={setPackageOffset} />
                </TabsContent>

                <TabsContent value="jobs" className="space-y-4 pt-4">
                    <HistoryTable empty={t('archives.no_jobs')} count={jobs?.items?.length ?? 0}>
                        <Table>
                            <TableHeader><TableRow>
                                <TableHead>{t('archives.started_at')}</TableHead>
                                <TableHead>{t('archives.backup_completed_at')}</TableHead>
                                <TableHead>{t('archives.trigger')}</TableHead>
                                <TableHead>{t('archives.cutoff')}</TableHead>
                                <TableHead>{t('archives.packages')}</TableHead>
                                <TableHead>{t('archives.logs')}</TableHead>
                                <TableHead>{t('archives.status')}</TableHead>
                                <TableHead className="w-28" />
                            </TableRow></TableHeader>
                            <TableBody>{(jobs?.items ?? []).map(job => (
                                <TableRow key={job.id}>
                                    <TableCell className="whitespace-nowrap">{formatDate(job.created_at, i18n.language)}</TableCell>
                                    <TableCell className="whitespace-nowrap">{job.completed_at ? formatDate(job.completed_at, i18n.language) : '-'}</TableCell>
                                    <TableCell>{t(`archives.trigger_${job.trigger}`)}</TableCell>
                                    <TableCell className="whitespace-nowrap">{formatDate(job.cutoff, i18n.language)}</TableCell>
                                    <TableCell>{job.package_count}</TableCell>
                                    <TableCell>{job.log_count}</TableCell>
                                    <TableCell><StatusBadge status={job.status} error={job.error} /></TableCell>
                                    <TableCell><Button type="button" variant="outline" size="xs" onClick={() => showJobPackages(job.id)}>{t('archives.view_packages')}</Button></TableCell>
                                </TableRow>
                            ))}</TableBody>
                        </Table>
                    </HistoryTable>
                    <Paginator page={jobs} offset={jobOffset} setOffset={setJobOffset} />
                </TabsContent>

                <TabsContent value="imports" className="space-y-4 pt-4">
                    <HistoryTable empty={t('archives.no_imports')} count={imports?.items?.length ?? 0}>
                        <Table>
                            <TableHeader><TableRow>
                                <TableHead>{t('archives.source')}</TableHead>
                                <TableHead>{t('archives.imported_at')}</TableHead>
                                <TableHead>{t('archives.status')}</TableHead>
                                <TableHead>{t('archives.logs')}</TableHead>
                                <TableHead>{t('archives.expires')}</TableHead>
                                <TableHead className="w-16" />
                            </TableRow></TableHeader>
                            <TableBody>{(imports?.items ?? []).map(batch => (
                                <TableRow key={batch.id}>
                                    <TableCell className="max-w-[600px] break-all font-mono text-xs">{batch.source_key || batch.id}</TableCell>
                                    <TableCell className="whitespace-nowrap">{formatDate(batch.created_at, i18n.language)}</TableCell>
                                    <TableCell><StatusBadge status={batch.status} error={batch.error} /></TableCell>
                                    <TableCell>{batch.log_count}</TableCell>
                                    <TableCell className="whitespace-nowrap">{batch.expires_at ? formatDate(batch.expires_at, i18n.language) : t('archives.manual')}</TableCell>
                                    <TableCell><Button type="button" variant="ghost" size="icon-sm" disabled={action !== ''} onClick={() => void deleteImport(batch.id)} title={t('common.delete')}><Trash2 className="h-4 w-4" /></Button></TableCell>
                                </TableRow>
                            ))}</TableBody>
                        </Table>
                    </HistoryTable>
                    <Paginator page={imports} offset={importOffset} setOffset={setImportOffset} />
                </TabsContent>
            </Tabs>

            <Dialog open={s3DialogOpen} onOpenChange={setS3DialogOpen}>
                <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-4xl">
                    <DialogHeader>
                        <DialogTitle>{t('archives.find_s3_title')}</DialogTitle>
                        <DialogDescription>{t('archives.find_s3_description')}</DialogDescription>
                    </DialogHeader>
                    <div className="flex flex-wrap items-end gap-2">
                        <div className="space-y-1.5">
                            <div className="text-xs text-muted-foreground">{t('archives.log_date')}</div>
                            <Input type="date" value={s3Date} onChange={event => setS3Date(event.target.value)} className="w-44" />
                        </div>
                        <Button type="button" variant="outline" disabled={!s3Date || s3Loading} onClick={() => void searchS3()}>
                            <Search className={`h-4 w-4 ${s3Loading ? 'animate-pulse' : ''}`} />{t('archives.search')}
                        </Button>
                        <Button type="button" disabled={action !== '' || !(s3Result?.objects?.length)} onClick={() => void importDate()}>
                            <ArchiveRestore className="h-4 w-4" />{t('archives.restore_date')}
                        </Button>
                    </div>
                    {s3Result?.s3_error && <div className="text-sm text-destructive">{s3Result.s3_error}</div>}
                    <HistoryTable empty={t('archives.no_packages')} count={s3Result?.objects?.length ?? 0}>
                        <Table>
                            <TableHeader><TableRow><TableHead>{t('archives.object')}</TableHead><TableHead>{t('archives.size')}</TableHead><TableHead>{t('archives.modified')}</TableHead><TableHead className="w-16" /></TableRow></TableHeader>
                            <TableBody>{(s3Result?.objects ?? []).map(object => (
                                <TableRow key={object.key}>
                                    <TableCell className="max-w-[580px] break-all font-mono text-xs">{object.key}</TableCell>
                                    <TableCell>{formatSize(object.size)}</TableCell>
                                    <TableCell className="whitespace-nowrap">{formatDate(object.last_modified, i18n.language)}</TableCell>
                                    <TableCell><Button type="button" variant="ghost" size="icon-sm" disabled={action !== ''} onClick={() => void importObject(object.key)} title={t('archives.import')}><CloudDownload className="h-4 w-4" /></Button></TableCell>
                                </TableRow>
                            ))}</TableBody>
                        </Table>
                    </HistoryTable>
                </DialogContent>
            </Dialog>
        </div>
    )
}

function StatusBadge({ status, error }: { status: string; error?: string }) {
    const { t } = useTranslation()
    return <div><Badge variant="outline">{t(`archives.status_${status}`)}</Badge>{error && <div className="mt-1 max-w-64 text-xs text-destructive">{error}</div>}</div>
}

function HistoryTable({ empty, count, children }: { empty: string; count: number; children: React.ReactNode }) {
    return <div className="overflow-x-auto border-y border-border">{count > 0 ? children : <div className="px-2 py-10 text-sm text-muted-foreground">{empty}</div>}</div>
}

function Paginator<T>({ page, offset, setOffset }: { page: ArchivePage<T> | null; offset: number; setOffset: (value: number) => void }) {
    const { t } = useTranslation()
    const limit = page?.limit ?? 50
    const total = page?.total ?? 0
    const pagination = getArchivePagination(offset, limit, total)
    return (
        <div className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
            <span>{t('archives.pagination', { current: pagination.current, pages: pagination.pages, total })}</span>
            <div className="flex gap-1">
                <Button type="button" variant="outline" size="icon-sm" disabled={!pagination.hasPrevious} onClick={() => setOffset(pagination.previousOffset)} title={t('archives.previous_page')}><ChevronLeft className="h-4 w-4" /></Button>
                <Button type="button" variant="outline" size="icon-sm" disabled={!pagination.hasNext} onClick={() => setOffset(pagination.nextOffset)} title={t('archives.next_page')}><ChevronRight className="h-4 w-4" /></Button>
            </div>
        </div>
    )
}

function localDateValue(value: Date): string {
    return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`
}
