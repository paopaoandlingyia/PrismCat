import { Download, ExternalLink, FileArchive, HardDrive, Image, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatSize } from '@/lib/utils'

interface BlobPanelProps {
    blobRef: string
    bodySize?: number
    contentType?: string
    binary?: boolean
    isLoaded: boolean
    loading: boolean
    error: string | null
    onLoad: () => void
}

export function BlobPanel({
    blobRef,
    bodySize,
    contentType,
    binary,
    isLoaded,
    loading,
    error,
    onLoad,
}: BlobPanelProps) {
    const { t } = useTranslation()
    const blobUrl = `/api/blobs/${encodeURIComponent(blobRef)}`
    const downloadUrl = `${blobUrl}?download=1`
    const mediaType = normalizeContentType(contentType)
    const isImage = binary && mediaType.startsWith('image/')
    const hash = shortBlobHash(blobRef)

    return (
        <div className="rounded-lg border border-border bg-muted/40 px-3 py-2">
            <div className="flex flex-wrap items-center gap-2 text-xs">
                {binary ? (
                    isImage ? <Image className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /> : <FileArchive className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                ) : (
                    <HardDrive className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                )}
                <span className="font-medium text-foreground">
                    {binary ? t('log_detail.binary_response', 'Binary response') : isLoaded ? t('log_detail.blob_loaded') : t('log_detail.blob_detached')}
                </span>
                {mediaType && (
                    <span className="rounded-md bg-background px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                        {mediaType}
                    </span>
                )}
                {typeof bodySize === 'number' && bodySize > 0 && (
                    <span className="rounded-md bg-background px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                        {formatSize(bodySize)}
                    </span>
                )}
                {hash && (
                    <span className="rounded-md bg-background px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                        sha256:{hash}
                    </span>
                )}
                <Tooltip>
                    <TooltipTrigger asChild>
                        <button
                            type="button"
                            className="inline-flex shrink-0 items-center rounded-md p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                            aria-label={blobRef}
                        >
                            <Info className="h-3 w-3" />
                        </button>
                    </TooltipTrigger>
                    <TooltipContent side="top" sideOffset={4} className="max-w-none font-mono text-xs break-all">
                        {blobRef}
                    </TooltipContent>
                </Tooltip>
                <div className="ml-auto flex items-center gap-0.5">
                    {!binary && !isLoaded && (
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={onLoad}
                            disabled={loading}
                            className="h-7 px-2 text-xs font-medium text-primary hover:bg-primary/10 hover:text-primary"
                        >
                            {loading ? t('common.loading') : t('log_detail.load_full')}
                        </Button>
                    )}
                    <a
                        href={blobUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    >
                        <ExternalLink className="h-3 w-3" />
                        {t('log_detail.open_raw')}
                    </a>
                    <a
                        href={downloadUrl}
                        className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    >
                        <Download className="h-3 w-3" />
                        {t('log_detail.download', 'Download')}
                    </a>
                </div>
            </div>
            {isImage && (
                <div className="mt-3 overflow-hidden rounded-md border border-border bg-background">
                    <img
                        src={blobUrl}
                        alt={t('log_detail.binary_image_preview', 'Binary image preview')}
                        className="max-h-[420px] w-full object-contain"
                        loading="lazy"
                    />
                </div>
            )}
            {error && (
                <div className="mt-1 font-mono text-xs text-danger">{error}</div>
            )}
        </div>
    )
}

function normalizeContentType(contentType?: string) {
    return (contentType ?? '').split(';')[0].trim().toLowerCase()
}

function shortBlobHash(blobRef: string) {
    const value = blobRef.trim().replace(/^blob:\/\//, '').replace(/^sha256:/, '')
    return value.length >= 12 ? value.slice(0, 12) : value
}
