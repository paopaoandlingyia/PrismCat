import { useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Copy, Eye, Image as ImageIcon, FileCode } from 'lucide-react';
import { Button } from './ui/button';
import { formatSize } from '@/lib/utils';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";

// ─── Base64 Detection (magic-number based) ───────────────────────────

interface Base64Detection {
    isBase64: boolean;
    fileType: 'jpeg' | 'png' | 'gif' | 'webp' | 'pdf' | 'unknown' | null;
    isImage: boolean;
    mimeType?: string;
    label: string;
}

const NO_B64: Base64Detection = { isBase64: false, fileType: null, isImage: false, label: '' };

/**
 * Detect whether a string is base64-encoded binary data.
 * Decodes only the first 16 chars (~12 bytes) to check magic numbers.
 */
function detectBase64(value: string): Base64Detection {
    if (value.length < 200) return NO_B64;
    if (!/^[A-Za-z0-9+/]+$/.test(value.substring(0, 200))) return NO_B64;
    try {
        const decoded = atob(value.substring(0, 16));
        const b = new Uint8Array(decoded.length);
        for (let i = 0; i < decoded.length; i++) b[i] = decoded.charCodeAt(i);

        if (b[0] === 0xFF && b[1] === 0xD8 && b[2] === 0xFF)
            return { isBase64: true, fileType: 'jpeg', isImage: true, mimeType: 'image/jpeg', label: 'JPEG' };
        if (b[0] === 0x89 && b[1] === 0x50 && b[2] === 0x4E && b[3] === 0x47)
            return { isBase64: true, fileType: 'png', isImage: true, mimeType: 'image/png', label: 'PNG' };
        if (b[0] === 0x47 && b[1] === 0x49 && b[2] === 0x46)
            return { isBase64: true, fileType: 'gif', isImage: true, mimeType: 'image/gif', label: 'GIF' };
        if (b[0] === 0x52 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x46)
            return { isBase64: true, fileType: 'webp', isImage: true, mimeType: 'image/webp', label: 'WebP' };
        if (b[0] === 0x25 && b[1] === 0x50 && b[2] === 0x44 && b[3] === 0x46)
            return { isBase64: true, fileType: 'pdf', isImage: false, mimeType: 'application/pdf', label: 'PDF' };
        return { isBase64: true, fileType: 'unknown', isImage: false, label: 'Base64' };
    } catch {
        return NO_B64;
    }
}

/** Parse data URI: "data:image/jpeg;base64,xxx" → { prefix, base64Data, mimeType } */
function parseDataUri(value: string) {
    if (!value.startsWith('data:')) return null;
    const marker = ';base64,';
    const idx = value.indexOf(marker);
    if (idx < 0) return null;
    const base64Data = value.substring(idx + marker.length);
    if (!base64Data || base64Data.length < 100) return null;
    return {
        prefix: value.substring(0, idx + marker.length),
        base64Data,
        mimeType: value.substring(5, idx).split(';')[0],
    };
}

function detectionFromMime(mimeType: string): Base64Detection {
    return { isBase64: true, fileType: 'unknown', isImage: mimeType.startsWith('image/'), mimeType, label: mimeType.split('/')[1]?.toUpperCase() || 'Base64' };
}

// ─── Components ──────────────────────────────────────────────────────

interface JsonViewerProps {
    data: any;
    initialExpanded?: boolean;
}

export function JsonViewer({ data, initialExpanded = true }: JsonViewerProps) {
    if (typeof data === 'string') return <SmartText text={data} />;
    if (typeof data !== 'object' || data === null) return <ValueNode value={data} />;
    return (
        <div className="font-mono text-[11px] leading-relaxed select-text">
            <CollapsibleNode data={data} label="" isRoot initialExpanded={initialExpanded} depth={0} />
        </div>
    );
}

// ─── SmartText: raw text with base64 detection ───────────────────────

export function SmartText({ text }: { text: string }) {
    type Seg = { type: 'text'; content: string } | { type: 'b64'; content: string; detection: Base64Detection; prefix?: string };
    const segments = useMemo(() => {
        if (!text || text.length < 200) return null;
        const parts: Seg[] = [];
        let lastIndex = 0, found = false;
        const regex = /(data:[^\s]+?;base64,)?([A-Za-z0-9+/]{200,}[=]{0,2})/g;
        let match;
        while ((match = regex.exec(text)) !== null) {
            const prefix = match[1] || undefined;
            const b64 = match[2];
            const detection = detectBase64(b64);
            if (!detection.isBase64) continue;
            found = true;
            if (match.index > lastIndex) parts.push({ type: 'text', content: text.substring(lastIndex, match.index) });
            if (prefix) parts.push({ type: 'text', content: prefix });
            parts.push({ type: 'b64', content: b64, detection, prefix });
            lastIndex = regex.lastIndex;
        }
        if (!found) return null;
        if (lastIndex < text.length) parts.push({ type: 'text', content: text.substring(lastIndex) });
        return parts;
    }, [text]);

    if (!segments) return <pre className="whitespace-pre-wrap break-all text-[11px] font-mono">{text}</pre>;
    return (
        <div className="whitespace-pre-wrap break-all leading-relaxed text-[11px] font-mono">
            {segments.map((seg, i) =>
                seg.type === 'text'
                    ? <span key={i}>{seg.content}</span>
                    : <Base64Placeholder key={i} value={seg.content} detection={seg.detection} dataUriPrefix={seg.prefix} />
            )}
        </div>
    );
}

// ─── CollapsibleNode: renders objects and arrays as a tree ────────────

/** Produce a CSS indent string for the given depth (2 spaces per level). */
function indent(depth: number): string {
    return '\u00A0\u00A0'.repeat(depth); // Non-breaking spaces × 2 per level
}

function CollapsibleNode({ data, label, isRoot = false, isArrayItem = false, initialExpanded = true, suffix = null, depth = 0 }: {
    data: any;
    label: string;
    isRoot?: boolean;
    isArrayItem?: boolean;
    initialExpanded?: boolean;
    suffix?: ReactNode;
    depth?: number;
}) {
    const { t } = useTranslation();
    const [expanded, setExpanded] = useState(initialExpanded);
    const isArray = Array.isArray(data);
    const entries = Object.entries(data);
    const isEmpty = entries.length === 0;
    const [open, close] = isArray ? ['[', ']'] : ['{', '}'];
    const showLabel = !isRoot && !isArrayItem;
    const pad = indent(depth);

    if (isEmpty) {
        return (
            <div>
                <span className="text-muted-foreground/30 select-none">{pad}</span>
                {showLabel && <span className="text-sky-600 dark:text-sky-400 font-semibold mr-1">"{label}": </span>}
                <span className="text-muted-foreground/60">{open}{close}</span>{suffix}
            </div>
        );
    }

    return (
        <>
            {/* Header: { or [ */}
            <div
                className="cursor-pointer hover:bg-muted/30 rounded-sm transition-colors inline-flex items-center"
                onClick={() => setExpanded(!expanded)}
            >
                <span className="text-muted-foreground/30 select-none">{pad}</span>
                {showLabel && <span className="text-sky-600 dark:text-sky-400 font-semibold mr-1">"{label}": </span>}
                <span className="text-muted-foreground/60">{open}</span>
                {!expanded && (
                    <>
                        <span className="mx-1 px-1 py-0.5 rounded bg-muted/50 text-[9px] text-muted-foreground font-bold">
                            {isArray ? t('json_viewer.items', { count: data.length }) : t('json_viewer.keys', { count: entries.length })}
                        </span>
                        <span className="text-muted-foreground/60">{close}</span>{suffix}
                    </>
                )}
            </div>

            {/* Children - each indented one level deeper */}
            {expanded && entries.map(([key, value], idx) => {
                const comma = idx < entries.length - 1 ? <span className="text-muted-foreground/40">,</span> : null;
                if (typeof value === 'object' && value !== null) {
                    return <CollapsibleNode key={key} data={value} label={key} isArrayItem={isArray} initialExpanded={idx < 10} suffix={comma} depth={depth + 1} />;
                }
                return (
                    <div key={key} className="flex items-start">
                        <span className="text-muted-foreground/30 select-none shrink-0">{indent(depth + 1)}</span>
                        {!isArray && <span className="text-sky-600 dark:text-sky-400 font-semibold mr-1 shrink-0">"{key}": </span>}
                        <span className="flex-1 min-w-0 break-all">
                            <ValueNode value={value} />{comma}
                        </span>
                    </div>
                );
            })}

            {/* Footer: } or ] */}
            {expanded && (
                <div>
                    <span className="text-muted-foreground/30 select-none">{pad}</span>
                    <span className="text-muted-foreground/60">{close}</span>{suffix}
                </div>
            )}
        </>
    );
}

// ─── ValueNode: renders leaf values ──────────────────────────────────

function ValueNode({ value }: { value: any }) {
    if (value === null) return <span className="text-rose-600 dark:text-rose-400 font-semibold">null</span>;
    if (typeof value === 'boolean') return <span className="text-indigo-600 dark:text-indigo-400 font-semibold">{value.toString()}</span>;
    if (typeof value === 'number') return <span className="text-orange-600 dark:text-orange-400">{value}</span>;

    if (typeof value === 'string') {
        // Case 1: data URI → show prefix visibly, replace only base64 part
        const dataUri = parseDataUri(value);
        if (dataUri) {
            const det = detectBase64(dataUri.base64Data);
            const detection = det.isBase64 ? det : detectionFromMime(dataUri.mimeType);
            return (
                <span className="text-emerald-600 dark:text-emerald-400 break-all leading-relaxed">
                    "{dataUri.prefix}
                    <Base64Placeholder value={dataUri.base64Data} detection={detection} dataUriPrefix={dataUri.prefix} />
                    "
                </span>
            );
        }

        // Case 2: pure base64 → detect via magic number
        const detection = detectBase64(value);
        if (detection.isBase64) {
            return (
                <span className="text-emerald-600 dark:text-emerald-400">
                    "<Base64Placeholder value={value} detection={detection} />"
                </span>
            );
        }

        // Case 3: normal string
        return <span className="text-emerald-600 dark:text-emerald-400 break-all leading-relaxed">"{value}"</span>;
    }

    return <span>{String(value)}</span>;
}

// ─── Base64Placeholder ───────────────────────────────────────────────

function Base64Placeholder({ value, detection, dataUriPrefix }: {
    value: string;
    detection: Base64Detection;
    dataUriPrefix?: string;
}) {
    const { t } = useTranslation();
    const [showFull, setShowFull] = useState(false);
    const [previewOpen, setPreviewOpen] = useState(false);

    const imgSrc = useMemo(() => {
        if (!detection.isImage) return null;
        if (dataUriPrefix) return `${dataUriPrefix}${value}`;
        if (detection.mimeType) return `data:${detection.mimeType};base64,${value}`;
        return null;
    }, [value, detection, dataUriPrefix]);

    const copyToClipboard = () => { navigator.clipboard.writeText(value); };

    if (showFull) {
        return (
            <span className="relative group/b64">
                <span className="text-emerald-600 dark:text-emerald-400 break-all bg-emerald-500/5 p-0.5 rounded">{value}</span>
                <Button variant="ghost" size="sm" onClick={() => setShowFull(false)} className="h-6 px-2 text-[10px] font-bold ml-1">
                    {t('json_viewer.collapse')}
                </Button>
            </span>
        );
    }

    return (
        <span className="inline-flex items-center gap-1.5 py-0.5 px-2 rounded-md bg-indigo-50 dark:bg-indigo-500/10 border border-indigo-200 dark:border-indigo-500/25 hover:border-indigo-400 dark:hover:border-indigo-500/50 transition-all my-0.5">
            {detection.isImage
                ? <ImageIcon className="h-3 w-3 text-indigo-600 dark:text-indigo-400" />
                : <FileCode className="h-3 w-3 text-indigo-600 dark:text-indigo-400" />}
            <span className="text-[11px] font-bold text-indigo-600 dark:text-indigo-400">
                {detection.label} ({formatSize(value.length)})
            </span>

            <span className="flex items-center gap-1.5 ml-1 border-l border-indigo-200 dark:border-indigo-500/25 pl-2">
                <button onClick={copyToClipboard} className="text-[10px] font-bold text-muted-foreground hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors">
                    {t('json_viewer.copy')}
                </button>
                {imgSrc && (
                    <button onClick={() => setPreviewOpen(true)} className="text-[10px] font-bold text-muted-foreground hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors flex items-center gap-0.5">
                        <Eye className="h-2.5 w-2.5" />
                        {t('json_viewer.preview')}
                    </button>
                )}
                <button onClick={() => setShowFull(true)} className="text-[10px] font-bold text-muted-foreground hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors">
                    {t('json_viewer.expand')}
                </button>
            </span>

            {imgSrc && (
                <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
                    <DialogContent className="max-w-3xl max-h-[90vh] flex flex-col p-1 overflow-hidden border-none shadow-2xl">
                        <DialogHeader className="p-4 bg-muted/20 border-b border-border/40">
                            <DialogTitle className="text-xs font-bold flex items-center gap-2">
                                <ImageIcon className="h-3.5 w-3.5" />
                                {t('json_viewer.image_preview')} · {detection.label} ({formatSize(value.length)})
                            </DialogTitle>
                        </DialogHeader>
                        <div className="flex-1 overflow-auto p-8 flex items-center justify-center bg-slate-50 dark:bg-card/50">
                            <img src={imgSrc} alt="Preview" className="max-w-full max-h-full shadow-2xl rounded border border-white/10" />
                        </div>
                        <div className="p-4 bg-muted/20 border-t border-border/40 flex justify-end gap-2">
                            <Button variant="outline" size="sm" onClick={copyToClipboard} className="text-[10px] font-bold h-8">
                                <Copy className="h-3 w-3 mr-2" />
                                {t('json_viewer.copy')}
                            </Button>
                            <Button variant="secondary" size="sm" onClick={() => setPreviewOpen(false)} className="text-[10px] font-bold h-8 px-4">
                                {t('common.cancel')}
                            </Button>
                        </div>
                    </DialogContent>
                </Dialog>
            )}
        </span>
    );
}
