import { useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Copy, Eye, Image as ImageIcon, FileCode } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from './ui/button';
import { formatSize } from '@/lib/utils';
import { copyText } from '@/lib/clipboard';
import {
    buildSmartTextSegments,
    detectionFromMime,
    detectBase64,
    LARGE_TEXT_PREVIEW_LENGTH,
    LARGE_TEXT_THRESHOLD,
    normalizeBase64,
    parseDataUri,
    type Base64Detection,
} from '@/lib/bodyContent';
import {
    childJsonSearchPath as childNodePath,
    collectTextSearchMatches,
    createJsonSearchPlan,
    jsonKeySearchSlot as keySearchSlot,
    jsonValueSearchSlot as valueSearchSlot,
    MAX_RENDERED_SEARCH_MATCHES,
    type JsonSearchPlan,
} from '@/lib/jsonSearch';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";

const ROOT_AUTO_EXPAND_LIMIT = 12;
const CHILD_AUTO_EXPAND_LIMIT = 6;
const ARRAY_SHAPE_SAMPLE_SIZE = 5;
const ARRAY_SHAPE_FORCE_EXPAND_LIMIT = 6;

export function HighlightText({
    text,
    searchTerm,
    maxMatches = MAX_RENDERED_SEARCH_MATCHES,
}: {
    text: string;
    searchTerm?: string;
    maxMatches?: number;
}) {
    if (!searchTerm || maxMatches <= 0) return <>{text}</>;
    const { ranges } = collectTextSearchMatches(text, searchTerm, maxMatches);
    if (!ranges.length) return <>{text}</>;
    const parts: ReactNode[] = [];
    let lastIndex = 0;
    ranges.forEach((range, index) => {
        if (range.start > lastIndex) parts.push(text.slice(lastIndex, range.start));
        parts.push(
            <mark key={index} className="bg-warning/30 text-inherit rounded-md" data-search-match="">
                {text.slice(range.start, range.end)}
            </mark>
        );
        lastIndex = range.end;
    });
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return <>{parts}</>;
}

// ─── Components ──────────────────────────────────────────────────────

export type JsonExpandMode = 'default' | 'all' | 'none';
type JsonContainer = Record<string, unknown> | unknown[];

interface JsonViewerProps {
    data: unknown;
    initialExpanded?: boolean;
    expandMode?: JsonExpandMode;
    searchTerm?: string;
    searchPlan?: JsonSearchPlan | null;
}

export function JsonViewer({ data, initialExpanded = true, expandMode = 'default', searchTerm, searchPlan }: JsonViewerProps) {
    const fallbackSearchPlan = useMemo(
        () => !searchPlan && searchTerm ? createJsonSearchPlan(data, searchTerm) : null,
        [data, searchPlan, searchTerm],
    );
    const effectiveSearchPlan = searchPlan ?? fallbackSearchPlan;

    if (typeof data === 'string') return <SmartText text={data} searchTerm={searchTerm} searchPlan={effectiveSearchPlan} nodePath="" />;
    if (typeof data !== 'object' || data === null) return <ValueNode value={data} searchTerm={searchTerm} searchPlan={effectiveSearchPlan} nodePath="" />;

    const rootData: JsonContainer = Array.isArray(data) || isRecord(data) ? data : {};
    const rootInitialExpanded = shouldAutoExpandNode({
        data: rootData,
        depth: 0,
        isRoot: true,
        initialExpanded,
    });

    return (
        <div className="font-mono text-sm leading-relaxed select-text">
            <CollapsibleNode data={rootData} label="" isRoot initialExpanded={rootInitialExpanded} depth={0} expandMode={expandMode} searchTerm={searchTerm} searchPlan={effectiveSearchPlan} nodePath="" />
        </div>
    );
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function getSearchMatchLimit(searchPlan: JsonSearchPlan | null | undefined, slot: string): number {
    return searchPlan?.matchesBySlot.get(slot) ?? 0;
}

// ─── SmartText: raw text with base64 detection ───────────────────────

export function SmartText({
    text,
    searchTerm,
    searchPlan,
    nodePath,
}: {
    text: string;
    searchTerm?: string;
    searchPlan?: JsonSearchPlan | null;
    nodePath: string;
}) {
    const isLargeText = text.length > LARGE_TEXT_THRESHOLD;
    const segments = useMemo(() => buildSmartTextSegments(text), [text]);

    if (isLargeText) {
        return <LargeTextPreview text={text} searchTerm={searchTerm} maxMatches={getSearchMatchLimit(searchPlan, valueSearchSlot(nodePath))} />;
    }

    if (!segments) return <pre className="whitespace-pre-wrap break-all text-xs font-mono">{searchTerm ? <HighlightText text={text} searchTerm={searchTerm} maxMatches={getSearchMatchLimit(searchPlan, valueSearchSlot(nodePath))} /> : text}</pre>;
    let searchableFragmentIndex = 0;
    return (
        <div className="whitespace-pre-wrap break-all leading-relaxed text-xs font-mono">
            {segments.map((seg, i) => {
                if (seg.type === 'b64') {
                    return <Base64Placeholder key={i} value={seg.content} detection={seg.detection} dataUriPrefix={seg.prefix} />;
                }
                const fragmentIndex = searchableFragmentIndex++;
                return <span key={i}>{searchTerm ? <HighlightText text={seg.content} searchTerm={searchTerm} maxMatches={getSearchMatchLimit(searchPlan, valueSearchSlot(nodePath, fragmentIndex))} /> : seg.content}</span>;
            })}
        </div>
    );
}

function LargeTextPreview({ text, searchTerm, maxMatches }: { text: string; searchTerm?: string; maxMatches: number }) {
    const { t } = useTranslation();
    const [expanded, setExpanded] = useState(false);
    const preview = useMemo(() => text.slice(0, LARGE_TEXT_PREVIEW_LENGTH), [text]);
    const searchRanges = useMemo(
        () => searchTerm ? collectTextSearchMatches(text, searchTerm, maxMatches).ranges : [],
        [maxMatches, searchTerm, text],
    );

    const copyToClipboard = async () => {
        if (await copyText(text)) {
            toast.success(t('log_detail.copy_success'));
        } else {
            toast.error(t('log_detail.copy_failed'));
        }
    };

    return (
        <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-2 text-xs font-medium text-muted-foreground/60">
                <span>{t('json_viewer.large_text', 'Large Text')}</span>
                <span>{formatSize(text.length)}</span>
                <button
                    type="button"
                    onClick={copyToClipboard}
                    className="text-muted-foreground transition-colors hover:text-primary"
                >
                    {t('json_viewer.copy')}
                </button>
                <button
                    type="button"
                    onClick={() => setExpanded((value) => !value)}
                    className="text-muted-foreground transition-colors hover:text-primary"
                >
                    {expanded ? t('json_viewer.collapse') : t('json_viewer.expand')}
                </button>
            </div>
            <pre className="whitespace-pre-wrap break-all rounded-md border border-border bg-background p-3 text-xs font-mono">
                {searchTerm && searchRanges.length && !expanded ? (
                    <LargeTextSearchSnippets text={text} ranges={searchRanges} />
                ) : expanded || text.length <= preview.length ? (
                    searchTerm ? <HighlightText text={text} searchTerm={searchTerm} maxMatches={maxMatches} /> : text
                ) : `${preview}\n...`}
            </pre>
        </div>
    );
}

function LargeTextSearchSnippets({ text, ranges }: { text: string; ranges: Array<{ start: number; end: number }> }) {
    const contextLength = 80;
    return (
        <>
            {ranges.map((range, index) => {
                const start = Math.max(0, range.start - contextLength);
                const end = Math.min(text.length, range.end + contextLength);
                return (
                    <span key={`${range.start}:${range.end}`} className="block border-b border-border py-1 last:border-b-0">
                        {start > 0 ? '…' : ''}
                        {text.slice(start, range.start)}
                        <mark className="bg-warning/30 text-inherit rounded-md" data-search-match="">
                            {text.slice(range.start, range.end)}
                        </mark>
                        {text.slice(range.end, end)}
                        {end < text.length ? '…' : ''}
                        {index < ranges.length - 1 ? '\n' : ''}
                    </span>
                );
            })}
        </>
    );
}

// ─── CollapsibleNode: renders objects and arrays as a tree ────────────

function getEntryCount(data: JsonContainer): number {
    return Array.isArray(data) ? data.length : Object.keys(data).length;
}

function getShallowValueKind(value: unknown): string {
    if (value === null) return 'null';
    if (Array.isArray(value)) return 'array';
    return typeof value === 'object' ? 'object' : typeof value;
}

function getShallowShapeSignature(value: unknown): string {
    if (value === null) return 'null';

    if (Array.isArray(value)) {
        const previewKinds = value.slice(0, 3).map(getShallowValueKind);
        return `array:${previewKinds.join('|')}:${value.length > 3 ? 'more' : 'full'}`;
    }

    if (isRecord(value)) {
        const keys = Object.keys(value).sort();
        const limitedKeys = keys.slice(0, 20);
        const fields = limitedKeys.map((key) => `${key}:${getShallowValueKind(value[key])}`);
        const suffix = keys.length > limitedKeys.length ? `|+${keys.length - limitedKeys.length}` : '';
        return `object:${fields.join('|')}${suffix}`;
    }

    return typeof value;
}

function shouldAutoExpandNode({
    data,
    depth,
    isRoot,
    initialExpanded,
    forceExpanded = false,
}: {
    data: JsonContainer;
    depth: number;
    isRoot: boolean;
    initialExpanded: boolean;
    forceExpanded?: boolean;
}): boolean {
    if (!initialExpanded) return false;

    const entryCount = getEntryCount(data);
    if (isRoot) return entryCount <= ROOT_AUTO_EXPAND_LIMIT;
    if (forceExpanded) return entryCount <= ARRAY_SHAPE_FORCE_EXPAND_LIMIT;

    return depth < 1 && entryCount <= CHILD_AUTO_EXPAND_LIMIT;
}

function createExpansionSnapshot({
    data,
    depth,
    isRoot,
    initialExpanded,
    forceExpanded,
    expandMode,
}: {
    data: JsonContainer;
    depth: number;
    isRoot: boolean;
    initialExpanded: boolean;
    forceExpanded: boolean;
    expandMode: JsonExpandMode;
}) {
    let expanded: boolean;
    if (expandMode === 'all') {
        expanded = true;
    } else if (expandMode === 'none') {
        expanded = false;
    } else {
        expanded = shouldAutoExpandNode({ data, depth, isRoot, initialExpanded, forceExpanded });
    }
    return {
        data,
        depth,
        isRoot,
        initialExpanded,
        forceExpanded,
        expandMode,
        expanded,
    };
}

function CollapsibleNode({ data, label, isRoot = false, isArrayItem = false, initialExpanded = true, forceExpanded = false, suffix = null, depth = 0, expandMode = 'default', searchTerm, searchPlan, nodePath }: {
    data: JsonContainer;
    label: string;
    isRoot?: boolean;
    isArrayItem?: boolean;
    initialExpanded?: boolean;
    forceExpanded?: boolean;
    suffix?: ReactNode;
    depth?: number;
    expandMode?: JsonExpandMode;
    searchTerm?: string;
    searchPlan?: JsonSearchPlan | null;
    nodePath: string;
}) {
    const { t } = useTranslation();
    const [expansion, setExpansion] = useState(() => createExpansionSnapshot({
        data,
        depth,
        isRoot,
        initialExpanded,
        forceExpanded,
        expandMode,
    }));
    let currentExpansion = expansion;
    if (
        expansion.data !== data ||
        expansion.depth !== depth ||
        expansion.isRoot !== isRoot ||
        expansion.initialExpanded !== initialExpanded ||
        expansion.forceExpanded !== forceExpanded ||
        expansion.expandMode !== expandMode
    ) {
        currentExpansion = createExpansionSnapshot({
            data,
            depth,
            isRoot,
            initialExpanded,
            forceExpanded,
            expandMode,
        });
        setExpansion(currentExpansion);
    }
    const expanded = currentExpansion.expanded || Boolean(searchTerm && searchPlan?.expandedPaths.has(nodePath));
    const setExpanded = (value: boolean) => {
        setExpansion((current) => ({ ...current, expanded: value }));
    };
    const isArray = Array.isArray(data);
    const entries = Object.entries(data);
    const renderedEntries = searchTerm && searchPlan
        ? entries.filter(([key]) => searchPlan.visiblePaths.has(childNodePath(nodePath, key)))
        : entries;
    const isEmpty = entries.length === 0;
    const [open, close] = isArray ? ['[', ']'] : ['{', '}'];
    const showLabel = !isRoot && !isArrayItem;
    const sampledArrayShapes = useMemo(() => {
        if (!Array.isArray(data)) return null;

        const sampleSize = Math.min(data.length, ARRAY_SHAPE_SAMPLE_SIZE);
        const shapes = new Set<string>();

        for (let i = 0; i < sampleSize; i++) {
            const item = data[i];
            if (item === null || typeof item !== 'object') continue;
            shapes.add(getShallowShapeSignature(item));
        }

        return { sampleSize, shapes };
    }, [data]);

    const shouldForceExpandArrayChild = (idx: number, value: unknown) => {
        if (!isArray || !sampledArrayShapes || value === null || typeof value !== 'object') {
            return false;
        }

        if (idx < sampledArrayShapes.sampleSize) {
            return true;
        }

        if (sampledArrayShapes.shapes.size === 0) {
            return false;
        }

        return !sampledArrayShapes.shapes.has(getShallowShapeSignature(value));
    };

    if (isEmpty) {
        return (
            <div>
                {showLabel && <span className="text-info font-semibold mr-1">"<HighlightText text={label} searchTerm={searchTerm} maxMatches={getSearchMatchLimit(searchPlan, keySearchSlot(nodePath))} />": </span>}
                <span className="text-muted-foreground/60">{open}{close}</span>{suffix}
            </div>
        );
    }

    return (
        <>
            {/* Header: { or [ */}
            <div
                className="cursor-pointer hover:bg-muted/30 rounded-sm transition-colors flex items-center w-fit"
                onClick={() => setExpanded(!expanded)}
            >
                {showLabel && <span className="text-info font-semibold mr-1">"<HighlightText text={label} searchTerm={searchTerm} maxMatches={getSearchMatchLimit(searchPlan, keySearchSlot(nodePath))} />": </span>}
                <span className="text-muted-foreground/60">{open}</span>
                {!expanded && (
                    <>
                        <span className="mx-1 px-1 py-0.5 rounded bg-muted/50 text-xs text-muted-foreground font-medium">
                            {isArray ? t('json_viewer.items', { count: data.length }) : t('json_viewer.keys', { count: entries.length })}
                        </span>
                        <span className="text-muted-foreground/60">{close}</span>{suffix}
                    </>
                )}
            </div>

            {/* Children - wrapped in a container that draws the indent guide */}
            {expanded && (
                <div className="ml-[1ch] border-l border-muted-foreground/15 pl-[1ch]">
                    {renderedEntries.map(([key, value], idx) => {
                        const comma = idx < renderedEntries.length - 1 ? <span className="text-muted-foreground/40">,</span> : null;
                        const childPath = childNodePath(nodePath, key);
                        if (typeof value === 'object' && value !== null) {
                            const forceExpandChild = shouldForceExpandArrayChild(idx, value);
                            return (
                                <CollapsibleNode
                                    key={key}
                                    data={Array.isArray(value) || isRecord(value) ? value : {}}
                                    label={key}
                                    isArrayItem={isArray}
                                    initialExpanded={forceExpandChild || (depth === 0 && idx < 3)}
                                    forceExpanded={forceExpandChild}
                                    suffix={comma}
                                    depth={depth + 1}
                                    expandMode={expandMode}
                                    searchTerm={searchTerm}
                                    searchPlan={searchPlan}
                                    nodePath={childPath}
                                />
                            );
                        }
                        return (
                            <div key={key} className="flex items-start">
                                {!isArray && <span className="text-primary font-semibold mr-1 shrink-0">"<HighlightText text={key} searchTerm={searchTerm} maxMatches={getSearchMatchLimit(searchPlan, keySearchSlot(childPath))} />": </span>}
                                <span className="flex-1 min-w-0 break-all">
                                    <ValueNode value={value} searchTerm={searchTerm} searchPlan={searchPlan} nodePath={childPath} />{comma}
                                </span>
                            </div>
                        );
                    })}
                </div>
            )}

            {/* Footer: } or ] */}
            {expanded && (
                <div>
                    <span className="text-muted-foreground/60">{close}</span>{suffix}
                </div>
            )}
        </>
    );
}

// ─── ValueNode: renders leaf values ──────────────────────────────────

function ValueNode({ value, searchTerm, searchPlan, nodePath }: { value: unknown; searchTerm?: string; searchPlan?: JsonSearchPlan | null; nodePath: string }) {
    const valueMatchLimit = getSearchMatchLimit(searchPlan, valueSearchSlot(nodePath));
    if (value === null) return <span className="text-danger font-semibold"><HighlightText text="null" searchTerm={searchTerm} maxMatches={valueMatchLimit} /></span>;
    if (typeof value === 'boolean') return <span className="text-primary font-semibold"><HighlightText text={value.toString()} searchTerm={searchTerm} maxMatches={valueMatchLimit} /></span>;
    if (typeof value === 'number') return <span className="text-warning"><HighlightText text={String(value)} searchTerm={searchTerm} maxMatches={valueMatchLimit} /></span>;

    if (typeof value === 'string') {
        // Case 1: data URI → show prefix visibly, replace only base64 part
        const dataUri = parseDataUri(value);
        if (dataUri) {
            const det = detectBase64(dataUri.base64Data);
            const detection = det.isBase64 ? det : detectionFromMime(dataUri.mimeType);
            return (
                <span className="text-success break-all leading-relaxed">
                    "<HighlightText text={dataUri.prefix} searchTerm={searchTerm} maxMatches={valueMatchLimit} />
                    <Base64Placeholder value={dataUri.base64Data} detection={detection} dataUriPrefix={dataUri.prefix} />
                    "
                </span>
            );
        }

        // Case 2: pure base64 → detect via magic number
        const detection = detectBase64(value);
        if (detection.isBase64) {
            return (
                <span className="text-success">
                    "<Base64Placeholder value={value} detection={detection} />"
                </span>
            );
        }

        // Case 3: normal string
        return <span className="text-success break-all leading-relaxed">"<HighlightText text={value} searchTerm={searchTerm} maxMatches={valueMatchLimit} />"</span>;
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
        const imageData = normalizeBase64(value);
        if (dataUriPrefix) return `${dataUriPrefix}${imageData}`;
        if (detection.mimeType) return `data:${detection.mimeType};base64,${imageData}`;
        return null;
    }, [value, detection, dataUriPrefix]);

    const copyToClipboard = async () => {
        if (await copyText(value)) {
            toast.success(t('log_detail.copy_success'));
        } else {
            toast.error(t('log_detail.copy_failed'));
        }
    };

    if (showFull) {
        return (
            <span className="relative group/b64">
                <span className="text-success break-all rounded-sm bg-success/10 p-0.5">{value}</span>
                <Button variant="ghost" size="sm" onClick={() => setShowFull(false)} className="h-6 px-2 text-xs font-medium ml-1">
                    {t('json_viewer.collapse')}
                </Button>
            </span>
        );
    }

    return (
        <span className="my-0.5 inline-flex items-center gap-1.5 rounded-md border border-primary/20 bg-primary/10 px-2 py-0.5 transition-colors hover:border-primary/40">
            {detection.isImage
                ? <ImageIcon className="h-3 w-3 text-primary" />
                : <FileCode className="h-3 w-3 text-primary" />}
            <span className="text-xs font-medium text-primary">
                {detection.label} ({formatSize(value.length)})
            </span>

            <span className="flex items-center gap-1.5 ml-1 border-l border-primary/25 pl-2">
                <button onClick={copyToClipboard} className="text-xs font-medium text-muted-foreground hover:text-primary dark:hover:text-primary transition-colors">
                    {t('json_viewer.copy')}
                </button>
                {imgSrc && (
                    <button onClick={() => setPreviewOpen(true)} className="text-xs font-medium text-muted-foreground hover:text-primary dark:hover:text-primary transition-colors flex items-center gap-0.5">
                        <Eye className="h-2.5 w-2.5" />
                        {t('json_viewer.preview')}
                    </button>
                )}
                <button onClick={() => setShowFull(true)} className="text-xs font-medium text-muted-foreground hover:text-primary dark:hover:text-primary transition-colors">
                    {t('json_viewer.expand')}
                </button>
            </span>

            {imgSrc && (
                <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
                    <DialogContent className="max-h-[90vh] max-w-3xl overflow-hidden rounded-lg border border-border bg-card p-1">
                        <DialogHeader className="border-b border-border bg-muted/30 p-4">
                            <DialogTitle className="text-xs font-medium flex items-center gap-2">
                                <ImageIcon className="h-3.5 w-3.5" />
                                {t('json_viewer.image_preview')} · {detection.label} ({formatSize(value.length)})
                            </DialogTitle>
                        </DialogHeader>
                        <div className="flex flex-1 items-center justify-center overflow-auto bg-muted/30 p-8">
                            <img src={imgSrc} alt="Preview" className="max-h-full max-w-full rounded-md border border-border bg-background" />
                        </div>
                        <div className="flex justify-end gap-2 border-t border-border bg-muted/30 p-4">
                            <Button variant="outline" size="sm" onClick={copyToClipboard} className="text-xs font-medium h-8">
                                <Copy className="h-3 w-3 mr-2" />
                                {t('json_viewer.copy')}
                            </Button>
                            <Button variant="secondary" size="sm" onClick={() => setPreviewOpen(false)} className="text-xs font-medium h-8 px-4">
                                {t('common.cancel')}
                            </Button>
                        </div>
                    </DialogContent>
                </Dialog>
            )}
        </span>
    );
}
