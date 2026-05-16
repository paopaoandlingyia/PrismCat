import {
    useEffect,
    useState,
    useCallback,
    useMemo,
    useId,
    type FormEvent,
    type ReactNode,
} from 'react'
import {
    Plus,
    Trash2,
    Save,
    Upload,
    Copy,
    CircleHelp,
    AlertTriangle,
    Pencil,
    ExternalLink,
    Activity,
    Cpu,
    MemoryStick,
    RefreshCw,
    Timer,
    Download,
    Database,
    Archive,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from '@/components/ui/tooltip'
import { fetchUpstreams, addUpstream, removeUpstream, fetchConfig, updateConfig, fetchSystemMetrics, fetchUpdateInfo, fetchStorageUsage } from '@/lib/api'
import type { Upstream, AppConfig, SystemMetrics, UpdateInfo, StorageUsage } from '@/lib/api'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'

type FieldBlockProps = {
    label: string
    hint?: string
    htmlFor?: string
    unit?: string
    children: ReactNode
}

type ToggleSettingProps = {
    label: string
    description: string
    checked: boolean
    onCheckedChange: (checked: boolean) => void
}

const requestOverrideExample = JSON.stringify([
    {
        name: 'Cost guard: cap max_tokens to 2048',
        enabled: true,
        match: {
            methods: ['POST'],
            path_prefixes: ['/v1/chat/completions', '/v1/messages'],
        },
        patch: [
            { op: 'replace', path: '/max_tokens', value: 2048 },
        ],
    },
    {
        name: 'Dev-only: downgrade gpt-4o to gpt-4o-mini',
        enabled: false,
        match: {
            methods: ['POST'],
            json: [
                { path: '/model', equals: 'gpt-4o' },
            ],
        },
        patch: [
            { op: 'replace', path: '/model', value: 'gpt-4o-mini' },
        ],
    },
    {
        name: 'Inject a global safety system prompt',
        enabled: false,
        match: {
            methods: ['POST'],
            path_prefixes: ['/v1/messages'],
        },
        patch: [
            { op: 'add', path: '/system', value: "Refuse anything outside the user's explicit request." },
        ],
    },
    {
        name: 'Strip the `user` field LangChain auto-injects',
        enabled: false,
        match: {
            methods: ['POST'],
            path_prefixes: ['/v1/chat/completions'],
        },
        patch: [
            { op: 'remove', path: '/user' },
        ],
    },
], null, 2)

function getErrorMessage(error: unknown, fallback: string) {
    return error instanceof Error ? error.message : fallback
}

type OverrideBinding = {
    enabled: boolean
    rule_names: string[]
}

type EditingUpstream = {
    name: string
    target: string
    timeout: number
    order: number
    outboundProxy: string
    overrideEnabled: boolean
    ruleNames: string[]
}

type OutboundProxyMode = 'env' | 'direct' | 'custom'
type SettingsTab = 'routing' | 'logging' | 'overrides' | 'system'

const customProxyPlaceholder = 'http://127.0.0.1:7890'

function outboundProxyMode(value?: string): OutboundProxyMode {
    const normalized = (value || '').trim().toLowerCase()
    if (!normalized || normalized === 'env') return 'env'
    if (normalized === 'direct') return 'direct'
    return 'custom'
}

function normalizedOutboundProxy(value: string): string {
    return value.trim() || 'env'
}

function formatBytes(value?: number | null): string {
    if (value === undefined || value === null) return '-'
    if (value < 1024) return `${value} B`
    const units = ['KB', 'MB', 'GB', 'TB']
    let size = value / 1024
    let unitIndex = 0
    while (size >= 1024 && unitIndex < units.length - 1) {
        size /= 1024
        unitIndex += 1
    }
    return `${size >= 100 ? size.toFixed(0) : size.toFixed(1)} ${units[unitIndex]}`
}

function formatPercent(value?: number | null): string {
    if (value === undefined || value === null) return '-'
    if (value < 0.1) return '0%'
    return `${value >= 10 ? value.toFixed(1) : value.toFixed(2)}%`
}

function formatDuration(seconds?: number | null): string {
    if (seconds === undefined || seconds === null) return '-'
    const total = Math.max(0, Math.floor(seconds))
    const days = Math.floor(total / 86400)
    const hours = Math.floor((total % 86400) / 3600)
    const minutes = Math.floor((total % 3600) / 60)
    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${minutes}m`
    return `${minutes}m ${total % 60}s`
}

function OutboundProxyControl({
    value,
    onChange,
    t,
}: {
    value: string
    onChange: (value: string) => void
    t: (key: string) => string
}) {
    const mode = outboundProxyMode(value)
    return (
        <div className="space-y-2">
            <Select
                value={mode}
                onValueChange={(nextMode: OutboundProxyMode) => {
                    if (nextMode === 'custom') {
                        onChange(mode === 'custom' ? value : customProxyPlaceholder)
                        return
                    }
                    onChange(nextMode)
                }}
            >
                <SelectTrigger className="h-10 w-full rounded-xl border-border/30 bg-background/50 text-sm">
                    <SelectValue />
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="env">{t('upstream_manager.outbound_proxy_env')}</SelectItem>
                    <SelectItem value="direct">{t('upstream_manager.outbound_proxy_direct')}</SelectItem>
                    <SelectItem value="custom">{t('upstream_manager.outbound_proxy_custom')}</SelectItem>
                </SelectContent>
            </Select>
            {mode === 'custom' && (
                <Input
                    value={value}
                    onChange={e => onChange(e.target.value)}
                    placeholder={customProxyPlaceholder}
                    className="h-10 rounded-xl border-border/30 bg-background/50 font-mono text-sm"
                />
            )}
        </div>
    )
}


function InfoTooltip({ content }: { content: string }) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <button
                    type="button"
                    className="inline-flex h-4 w-4 items-center justify-center rounded-full text-muted-foreground/85 transition-colors hover:text-foreground"
                    aria-label="More info"
                >
                    <CircleHelp className="h-3.5 w-3.5" />
                </button>
            </TooltipTrigger>
            <TooltipContent sideOffset={6} className="max-w-xs px-3 py-2 text-[12px] leading-6">
                {content}
            </TooltipContent>
        </Tooltip>
    )
}

function FieldBlock({ label, hint, htmlFor, unit, children }: FieldBlockProps) {
    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2.5">
                <Label
                    htmlFor={htmlFor}
                    className="cursor-pointer select-none text-sm font-medium text-foreground hover:text-foreground/90 transition-colors"
                >
                    {label}
                </Label>
                {hint && <InfoTooltip content={hint} />}
                {unit && (
                    <span className="rounded-full bg-muted/60 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                        {unit}
                    </span>
                )}
            </div>
            {children}
        </div>
    )
}

function ToggleSetting({
    label,
    description,
    checked,
    onCheckedChange,
}: ToggleSettingProps) {
    const id = useId()
    return (
        <div className="flex items-center gap-3">
            <Switch
                id={id}
                checked={checked}
                onCheckedChange={onCheckedChange}
                className="shrink-0 data-[state=unchecked]:bg-border/60"
            />
            <div className="flex items-center gap-1.5">
                <Label
                    htmlFor={id}
                    className="cursor-pointer select-none text-sm font-medium text-foreground hover:text-foreground transition-colors"
                >
                    {label}
                </Label>
                <InfoTooltip content={description} />
            </div>
        </div>
    )
}

function MetricCard({
    icon,
    label,
    value,
    detail,
}: {
    icon: ReactNode
    label: string
    value: string
    detail?: string
}) {
    return (
        <div className="rounded-xl border border-border/40 bg-background/45 px-4 py-4 shadow-sm">
            <div className="mb-4 flex items-center justify-between gap-3">
                <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    {label}
                </div>
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    {icon}
                </div>
            </div>
            <div className="text-2xl font-semibold text-foreground">{value}</div>
            {detail && (
                <div className="mt-2 text-xs leading-5 text-muted-foreground">
                    {detail}
                </div>
            )}
        </div>
    )
}


export function Settings() {
    const { t } = useTranslation()
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [config, setConfig] = useState<AppConfig | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showAddForm, setShowAddForm] = useState(false)
    const [activeTab, setActiveTab] = useState<SettingsTab>('routing')
    const [metrics, setMetrics] = useState<SystemMetrics | null>(null)
    const [metricsLoading, setMetricsLoading] = useState(false)
    const [metricsError, setMetricsError] = useState('')
    const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null)
    const [updateLoading, setUpdateLoading] = useState(false)
    const [updateError, setUpdateError] = useState('')
    const [storageUsage, setStorageUsage] = useState<StorageUsage | null>(null)
    const [storageLoading, setStorageLoading] = useState(false)
    const [storageError, setStorageError] = useState('')

    const [newName, setNewName] = useState('')
    const [newTarget, setNewTarget] = useState('')
    const [newTimeout, setNewTimeout] = useState(30)
    const [newOrder, setNewOrder] = useState(100)
    const [newOutboundProxy, setNewOutboundProxy] = useState('env')
    const [editingUpstream, setEditingUpstream] = useState<EditingUpstream | null>(null)

    const [enablePathRouting, setEnablePathRouting] = useState(false)
    const [pathRoutingPrefix, setPathRoutingPrefix] = useState('/_proxy')

    const [maxRequestBody, setMaxRequestBody] = useState(5120)
    const [maxResponseBody, setMaxResponseBody] = useState(32768)
    const [sensitiveHeaders, setSensitiveHeaders] = useState('')
    const [detachBodyOver, setDetachBodyOver] = useState(2048)
    const [storeBase64, setStoreBase64] = useState(true)
    const [earlyRequestBodySnapshot, setEarlyRequestBodySnapshot] = useState(false)

    const [retentionDays, setRetentionDays] = useState(30)
    const [requestOverridesEnabled, setRequestOverridesEnabled] = useState(false)
    const [overrideMaxBodyKB, setOverrideMaxBodyKB] = useState(1024)
    const [overrideRulesText, setOverrideRulesText] = useState('')
    const [overrideBindings, setOverrideBindings] = useState<Record<string, OverrideBinding>>({})

    const domainSuffix = config?.server?.proxy_domains?.[0] || 'localhost'
    const previewUpstreamName = upstreams[0]?.name || 'openai'
    const sortedUpstreams = useMemo(() => {
        return [...upstreams].sort((a, b) => {
            const orderDiff = (a.order || 0) - (b.order || 0)
            if (orderDiff !== 0) return orderDiff
            return a.name.localeCompare(b.name)
        })
    }, [upstreams])
    const parsedOverrideRules = useMemo(() => {
        try {
            const parsed = overrideRulesText.trim() ? JSON.parse(overrideRulesText) : []
            if (!Array.isArray(parsed)) return []
            return parsed
                .map((rule) => {
                    if (!rule || typeof rule !== 'object') return ''
                    const name = (rule as { name?: unknown }).name
                    return typeof name === 'string' ? name.trim() : ''
                })
                .filter(Boolean)
        } catch {
            return []
        }
    }, [overrideRulesText])

    const formatOutboundProxy = useCallback((value?: string) => {
        const mode = outboundProxyMode(value)
        if (mode === 'env') return t('upstream_manager.outbound_proxy_env')
        if (mode === 'direct') return t('upstream_manager.outbound_proxy_direct')
        return value || customProxyPlaceholder
    }, [t])

    const proxyBase = useMemo(() => {
        const proto = window.location.protocol
        const hostname = window.location.hostname
        const port = window.location.port
        const portSuffix = port && port !== '80' && port !== '443' ? `:${port}` : ''
        return { proto, hostname, portSuffix }
    }, [])

    const getProxyUrl = useCallback((name: string) => {
        return `${proxyBase.proto}//${name}.${domainSuffix}${proxyBase.portSuffix}`
    }, [proxyBase, domainSuffix])

    const getPathProxyUrl = useCallback((name: string) => {
        const trimmedPrefix = pathRoutingPrefix.trim()
        let normalizedPrefix = trimmedPrefix || '/_proxy'
        if (!normalizedPrefix.startsWith('/')) {
            normalizedPrefix = `/${normalizedPrefix}`
        }
        normalizedPrefix = normalizedPrefix.replace(/\/+$/, '') || '/_proxy'
        return `${proxyBase.proto}//${proxyBase.hostname}${proxyBase.portSuffix}${normalizedPrefix}/${name}`
    }, [pathRoutingPrefix, proxyBase])

    const handleCopy = useCallback((value: string) => {
        navigator.clipboard.writeText(value)
        toast.success(t('log_detail.copy_success'))
    }, [t])

    const loadData = useCallback(async () => {
        setLoading(true)
        try {
            const [upstreamsData, configData] = await Promise.all([
                fetchUpstreams(),
                fetchConfig(),
            ])
            setUpstreams(upstreamsData || [])
            setConfig(configData)
            setShowAddForm(prev => prev || !upstreamsData?.length)
            const nextOrder = Math.max(0, ...(upstreamsData || []).map(item => item.order || 0)) + 10
            setNewOrder(nextOrder)

            setMaxRequestBody(Math.round(configData.logging.max_request_body / 1024))
            setMaxResponseBody(Math.round(configData.logging.max_response_body / 1024))
            setSensitiveHeaders(configData.logging.sensitive_headers.join('\n'))
            setDetachBodyOver(Math.round(configData.logging.detach_body_over_bytes / 1024))
            setStoreBase64(configData.logging.store_base64)
            setEarlyRequestBodySnapshot(configData.logging.early_request_body_snapshot)
            setRetentionDays(configData.storage.retention_days)
            setEnablePathRouting(configData.server.enable_path_routing)
            setPathRoutingPrefix(configData.server.path_routing_prefix || '/_proxy')
            setRequestOverridesEnabled(configData.request_overrides?.enabled ?? false)
            setOverrideMaxBodyKB(Math.round((configData.request_overrides?.max_body_bytes ?? 1048576) / 1024))
            setOverrideBindings(configData.request_overrides?.upstreams ?? {})
            const overrideRules = configData.request_overrides?.rules ?? []
            setOverrideRulesText(overrideRules.length ? JSON.stringify(overrideRules, null, 2) : '')
        } catch (err) {
            console.error('Failed to load settings:', err)
            toast.error(t('common.error'))
        } finally {
            setLoading(false)
        }
    }, [t])

    useEffect(() => {
        loadData()
    }, [loadData])

    const loadMetrics = useCallback(async (showLoading = false) => {
        if (showLoading) setMetricsLoading(true)
        try {
            const nextMetrics = await fetchSystemMetrics()
            setMetrics(nextMetrics)
            setMetricsError('')
        } catch (err) {
            console.error('Failed to load system metrics:', err)
            setMetricsError(t('settings.system_metrics_failed'))
        } finally {
            if (showLoading) setMetricsLoading(false)
        }
    }, [t])

    useEffect(() => {
        if (activeTab !== 'system') return
        loadMetrics(true)
        const timer = window.setInterval(() => loadMetrics(false), 5000)
        return () => window.clearInterval(timer)
    }, [activeTab, loadMetrics])

    const memoryUsedPercent = useMemo(() => {
        if (!metrics?.memory.total_bytes || metrics.memory.used_bytes === undefined) return null
        return metrics.memory.used_bytes / metrics.memory.total_bytes * 100
    }, [metrics])

    const metricsUpdatedAt = useMemo(() => {
        if (!metrics?.timestamp) return '-'
        return new Date(metrics.timestamp).toLocaleTimeString()
    }, [metrics])

    const storageCalculatedAt = useMemo(() => {
        if (!storageUsage?.calculated_at) return '-'
        return new Date(storageUsage.calculated_at).toLocaleTimeString()
    }, [storageUsage])

    const handleCheckUpdate = async () => {
        setUpdateLoading(true)
        try {
            const info = await fetchUpdateInfo()
            setUpdateInfo(info)
            setUpdateError('')
        } catch (err) {
            setUpdateError(getErrorMessage(err, t('settings.update_failed')))
        } finally {
            setUpdateLoading(false)
        }
    }

    const handleCalculateStorage = async () => {
        setStorageLoading(true)
        try {
            const usage = await fetchStorageUsage()
            setStorageUsage(usage)
            setStorageError('')
        } catch (err) {
            setStorageError(getErrorMessage(err, t('settings.storage_usage_failed')))
        } finally {
            setStorageLoading(false)
        }
    }

    const handleAddUpstream = async (e: FormEvent) => {
        e.preventDefault()
        try {
            await addUpstream(newName, newTarget, newTimeout, newOrder, normalizedOutboundProxy(newOutboundProxy))
            setNewName('')
            setNewTarget('')
            setNewTimeout(30)
            setNewOrder(prev => prev + 10)
            setNewOutboundProxy('env')
            setShowAddForm(false)
            loadData()
            toast.success(t('settings.upstream_added'))
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        }
    }

    const handleRemoveUpstream = async (name: string) => {
        if (!confirm(t('upstream_manager.confirm_delete', { name }))) return
        try {
            await removeUpstream(name)
            loadData()
            toast.success(t('settings.upstream_removed'))
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        }
    }

    const buildOverridesPayload = (bindings: Record<string, OverrideBinding>, rules: unknown[]) => ({
        enabled: requestOverridesEnabled,
        max_body_bytes: overrideMaxBodyKB * 1024,
        upstreams: bindings,
        rules,
    })

    const parseOverrideRules = () => {
        const trimmedRules = overrideRulesText.trim()
        if (!trimmedRules) return []
        const parsed = JSON.parse(trimmedRules)
        if (!Array.isArray(parsed)) {
            throw new Error(t('settings.request_overrides_rules_must_be_array'))
        }
        return parsed
    }

    const handleEditUpstream = (upstream: Upstream) => {
        const binding = overrideBindings[upstream.name]
        setEditingUpstream({
            name: upstream.name,
            target: upstream.target,
            timeout: upstream.timeout,
            order: upstream.order || 0,
            outboundProxy: upstream.outbound_proxy || 'env',
            overrideEnabled: binding?.enabled ?? false,
            ruleNames: binding?.rule_names ?? [],
        })
    }

    const toggleEditingRule = (ruleName: string, checked: boolean) => {
        setEditingUpstream(current => {
            if (!current) return current
            const nextRules = checked
                ? [...current.ruleNames, ruleName].filter((value, index, arr) => arr.indexOf(value) === index)
                : current.ruleNames.filter(name => name !== ruleName)
            return { ...current, ruleNames: nextRules }
        })
    }

    const handleSaveUpstreamEdit = async () => {
        if (!editingUpstream) return
        setSaving(true)
        try {
            const overrideRules = parseOverrideRules()
            const nextBindings = {
                ...overrideBindings,
                [editingUpstream.name]: {
                    enabled: editingUpstream.overrideEnabled,
                    rule_names: editingUpstream.ruleNames,
                },
            }
            await addUpstream(
                editingUpstream.name,
                editingUpstream.target,
                editingUpstream.timeout,
                editingUpstream.order,
                normalizedOutboundProxy(editingUpstream.outboundProxy),
            )
            await updateConfig({
                request_overrides: buildOverridesPayload(nextBindings, overrideRules),
            })
            toast.success(t('settings.config_saved'))
            setEditingUpstream(null)
            loadData()
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        } finally {
            setSaving(false)
        }
    }

    const handleSaveAll = async () => {
        setSaving(true)
        try {
            const overrideRules = parseOverrideRules()

            await updateConfig({
                server: {
                    enable_path_routing: enablePathRouting,
                    path_routing_prefix: pathRoutingPrefix,
                },
                logging: {
                    max_request_body: maxRequestBody * 1024,
                    max_response_body: maxResponseBody * 1024,
                    sensitive_headers: sensitiveHeaders.split('\n').map(s => s.trim()).filter(Boolean),
                    detach_body_over_bytes: detachBodyOver * 1024,
                    store_base64: storeBase64,
                    early_request_body_snapshot: earlyRequestBodySnapshot,
                },
                storage: {
                    retention_days: retentionDays,
                },
                request_overrides: buildOverridesPayload(overrideBindings, overrideRules),
            })
            toast.success(t('settings.config_saved'))
            loadData()
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        } finally {
            setSaving(false)
        }
    }

    if (loading) {
        return (
            <div className="flex h-96 flex-col items-center justify-center gap-4">
                <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
                <div className="text-sm font-medium text-muted-foreground">
                    {t('common.loading')}
                </div>
            </div>
        )
    }

    return (
        <div className="w-full">
            <Dialog open={!!editingUpstream} onOpenChange={(open) => !open && setEditingUpstream(null)}>
                <DialogContent className="max-w-2xl rounded-2xl border border-border/60 bg-card p-0 shadow-2xl">
                    <DialogHeader className="border-b border-border/60 px-6 py-5">
                        <DialogTitle className="text-base font-bold">
                            {editingUpstream ? t('upstream_manager.edit_title', { name: editingUpstream.name }) : t('common.edit')}
                        </DialogTitle>
                    </DialogHeader>
                    {editingUpstream && (
                        <div className="space-y-6 px-6 py-5">
                            <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                                <FieldBlock label={t('upstream_manager.name')}>
                                    <Input
                                        value={editingUpstream.name}
                                        readOnly
                                        className="h-10 rounded-xl border-border/30 bg-muted/50 font-mono text-sm"
                                    />
                                </FieldBlock>
                                <FieldBlock label={t('upstream_manager.order')}>
                                    <Input
                                        type="number"
                                        min={0}
                                        value={editingUpstream.order}
                                        onChange={e => setEditingUpstream(current => current ? { ...current, order: Number(e.target.value) } : current)}
                                        className="h-10 rounded-xl border-border/30 bg-background/50 text-sm"
                                    />
                                </FieldBlock>
                                <div className="sm:col-span-2">
                                    <FieldBlock label={t('upstream_manager.target')}>
                                        <Input
                                            value={editingUpstream.target}
                                            onChange={e => setEditingUpstream(current => current ? { ...current, target: e.target.value } : current)}
                                            className="h-10 rounded-xl border-border/30 bg-background/50 font-mono text-sm"
                                        />
                                    </FieldBlock>
                                </div>
                                <FieldBlock label={t('upstream_manager.timeout')}>
                                    <Input
                                        type="number"
                                        min="1"
                                        value={editingUpstream.timeout}
                                        onChange={e => setEditingUpstream(current => current ? { ...current, timeout: Number(e.target.value) } : current)}
                                        className="h-10 rounded-xl border-border/30 bg-background/50 text-sm"
                                    />
                                </FieldBlock>
                                <FieldBlock
                                    label={t('upstream_manager.outbound_proxy')}
                                    hint={t('upstream_manager.outbound_proxy_hint')}
                                >
                                    <OutboundProxyControl
                                        value={editingUpstream.outboundProxy}
                                        onChange={value => setEditingUpstream(current => current ? { ...current, outboundProxy: value } : current)}
                                        t={t}
                                    />
                                </FieldBlock>
                            </div>

                            <div className="rounded-xl border border-border/50 bg-muted/20 p-4">
                                <ToggleSetting
                                    label={t('upstream_manager.override_enabled')}
                                    description={t('upstream_manager.override_enabled_hint')}
                                    checked={editingUpstream.overrideEnabled}
                                    onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, overrideEnabled: checked } : current)}
                                />
                                <div className="mt-4 space-y-2">
                                    <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                        {t('upstream_manager.bound_rules')}
                                    </div>
                                    {parsedOverrideRules.length === 0 ? (
                                        <div className="rounded-lg border border-dashed border-border/50 bg-background/50 px-3 py-4 text-xs text-muted-foreground">
                                            {t('upstream_manager.no_rules')}
                                        </div>
                                    ) : (
                                        <div className="grid gap-2 sm:grid-cols-2">
                                            {parsedOverrideRules.map(ruleName => (
                                                <label
                                                    key={ruleName}
                                                    className="flex items-center gap-2 rounded-lg border border-border/40 bg-background/50 px-3 py-2 text-sm"
                                                >
                                                    <input
                                                        type="checkbox"
                                                        checked={editingUpstream.ruleNames.includes(ruleName)}
                                                        onChange={e => toggleEditingRule(ruleName, e.target.checked)}
                                                    />
                                                    <span className="min-w-0 truncate font-mono text-xs">{ruleName}</span>
                                                </label>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </div>

                            <div className="flex justify-end gap-2 border-t border-border/50 pt-4">
                                <Button type="button" variant="ghost" onClick={() => setEditingUpstream(null)}>
                                    {t('common.cancel')}
                                </Button>
                                <Button type="button" onClick={handleSaveUpstreamEdit} disabled={saving}>
                                    <Save className="mr-2 h-4 w-4" />
                                    {t('common.save')}
                                </Button>
                            </div>
                        </div>
                    )}
                </DialogContent>
            </Dialog>

            <div className="flex w-full justify-center">
                <div className="relative z-10 w-full space-y-10 pb-20 px-4 sm:px-10 pt-6 animate-fade-in">

                    {/* Header & Tabs */}
                    <div className="flex items-center gap-2 border-b border-border/40 pb-5">

                        <div className="flex items-center gap-2">
                            <button
                                onClick={() => setActiveTab('routing')}
                                className={cn(
                                    "px-5 py-2.5 rounded-xl text-base font-medium transition-all duration-200",
                                    activeTab === 'routing'
                                        ? "bg-primary/10 text-primary shadow-sm"
                                        : "text-foreground/85 hover:bg-muted/50 hover:text-foreground"
                                )}
                            >
                                {t('settings.tabs.upstreams')}
                            </button>
                            <button
                                onClick={() => setActiveTab('logging')}
                                className={cn(
                                    "px-5 py-2.5 rounded-xl text-base font-medium transition-all duration-200",
                                    activeTab === 'logging'
                                        ? "bg-primary/10 text-primary shadow-sm"
                                        : "text-foreground/70 hover:bg-muted/50 hover:text-foreground"
                                )}
                            >
                                {t('settings.tabs.logging')}
                            </button>
                            <button
                                onClick={() => setActiveTab('overrides')}
                                className={cn(
                                    "px-5 py-2.5 rounded-xl text-base font-medium transition-all duration-200",
                                    activeTab === 'overrides'
                                        ? "bg-primary/10 text-primary shadow-sm"
                                        : "text-foreground/70 hover:bg-muted/50 hover:text-foreground"
                                )}
                            >
                                {t('settings.tabs.overrides')}
                            </button>
                            <button
                                onClick={() => setActiveTab('system')}
                                className={cn(
                                    "px-5 py-2.5 rounded-xl text-base font-medium transition-all duration-200",
                                    activeTab === 'system'
                                        ? "bg-primary/10 text-primary shadow-sm"
                                        : "text-foreground/70 hover:bg-muted/50 hover:text-foreground"
                                )}
                            >
                                {t('settings.tabs.system')}
                            </button>
                        </div>
                    </div>

                    {/* Content Area */}
                    <div className="pt-2">
                        {activeTab === 'routing' && (
                            <div className="space-y-12 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                {/* Action Toolbar */}
                                <section className="flex flex-col gap-6">
                                    <div className="flex flex-wrap items-end gap-x-8 gap-y-6">
                                        <div className="w-[240px]">
                                            <FieldBlock
                                                label={t('settings.proxy_domain_suffix')}
                                                hint={t('settings.proxy_domain_suffix_hint', {
                                                    name: previewUpstreamName,
                                                    suffix: domainSuffix,
                                                })}
                                            >
                                                <Input
                                                    value={`.${domainSuffix}`}
                                                    readOnly
                                                    className="h-11 rounded-xl border-border/30 bg-background/40 text-sm font-medium shadow-sm transition-colors cursor-default"
                                                />
                                            </FieldBlock>
                                        </div>

                                        <div className="w-[240px]">
                                            <FieldBlock
                                                label={t('settings.path_routing_prefix')}
                                                htmlFor="path-routing-prefix"
                                                hint={t('settings.path_routing_prefix_hint')}
                                            >
                                                <Input
                                                    id="path-routing-prefix"
                                                    value={pathRoutingPrefix}
                                                    onChange={e => setPathRoutingPrefix(e.target.value)}
                                                    placeholder="/_proxy"
                                                    className="h-11 rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>

                                        <div className="h-11 flex items-center">
                                            <ToggleSetting
                                                label={t('settings.enable_path_routing')}
                                                description={t('settings.enable_path_routing_hint')}
                                                checked={enablePathRouting}
                                                onCheckedChange={setEnablePathRouting}
                                            />
                                        </div>

                                        <div className="h-11 flex items-center gap-3 sm:ml-auto">
                                            <Button
                                                type="button"
                                                onClick={handleSaveAll}
                                                disabled={saving}
                                                variant="default"
                                                size="lg"
                                                className="h-11 rounded-xl min-w-[120px] font-medium shadow-sm transition-all whitespace-nowrap shrink-0"
                                            >
                                                <Save className="mr-1.5 h-4 w-4 shrink-0" />
                                                {t('common.save')}
                                            </Button>
                                            <Button
                                                type="button"
                                                variant={showAddForm ? 'secondary' : 'default'}
                                                onClick={() => setShowAddForm(prev => !prev)}
                                                size="lg"
                                                className="h-11 rounded-xl min-w-[140px] font-medium shadow-sm transition-all whitespace-nowrap shrink-0"
                                            >
                                                {!showAddForm && <Plus className="mr-1.5 h-4 w-4 shrink-0" />}
                                                {showAddForm ? t('common.cancel') : t('upstream_manager.add_new')}
                                            </Button>
                                        </div>
                                    </div>
                                </section>

                                {/* Upstreams List Area */}
                                <section className="flex flex-col gap-6 pt-2">
                                    <div className="w-full">
                                        {showAddForm && (
                                            <div className="mb-8 rounded-2xl bg-background/40 p-6 ring-1 ring-border/20 backdrop-blur-sm w-fit">
                                                <form onSubmit={handleAddUpstream} className="flex flex-wrap items-end gap-6">
                                                    <div className="w-[240px]">
                                                        <FieldBlock label={t('upstream_manager.name')} htmlFor="name">
                                                            <div className="relative">
                                                                <Input
                                                                    id="name"
                                                                    value={newName}
                                                                    onChange={e => setNewName(e.target.value)}
                                                                    placeholder="openai"
                                                                    className="h-11 rounded-xl border-border/30 bg-background/80 pr-20 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                                    required
                                                                />
                                                                <div className="pointer-events-none absolute inset-y-0 right-4 flex items-center text-xs text-muted-foreground">
                                                                    .{domainSuffix}
                                                                </div>
                                                            </div>
                                                        </FieldBlock>
                                                    </div>

                                                    <div className="w-[320px] max-w-full">
                                                        <FieldBlock label={t('upstream_manager.target')} htmlFor="target">
                                                            <Input
                                                                id="target"
                                                                value={newTarget}
                                                                onChange={e => setNewTarget(e.target.value)}
                                                                placeholder="https://api.openai.com"
                                                                className="h-11 rounded-xl border-border/30 bg-background/80 font-mono text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                                required
                                                            />
                                                        </FieldBlock>
                                                    </div>

                                                    <div className="w-[120px]">
                                                        <FieldBlock label={t('upstream_manager.timeout')} htmlFor="timeout">
                                                            <Input
                                                                id="timeout"
                                                                type="number"
                                                                min="1"
                                                                value={newTimeout}
                                                                onChange={e => setNewTimeout(Number(e.target.value))}
                                                                className="h-11 rounded-xl border-border/30 bg-background/80 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                            />
                                                        </FieldBlock>
                                                    </div>

                                                    <div className="w-[120px]">
                                                        <FieldBlock label={t('upstream_manager.order')} htmlFor="order">
                                                            <Input
                                                                id="order"
                                                                type="number"
                                                                min={0}
                                                                value={newOrder}
                                                                onChange={e => setNewOrder(Number(e.target.value))}
                                                                className="h-11 rounded-xl border-border/30 bg-background/80 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                            />
                                                        </FieldBlock>
                                                    </div>

                                                    <div className="w-[260px]">
                                                        <FieldBlock
                                                            label={t('upstream_manager.outbound_proxy')}
                                                            hint={t('upstream_manager.outbound_proxy_hint')}
                                                        >
                                                            <OutboundProxyControl
                                                                value={newOutboundProxy}
                                                                onChange={setNewOutboundProxy}
                                                                t={t}
                                                            />
                                                        </FieldBlock>
                                                    </div>

                                                    <div className="flex h-11 items-center">
                                                        <Button type="submit" variant="default" size="lg" className="h-11 rounded-xl min-w-[120px] font-medium shadow-sm whitespace-nowrap shrink-0">
                                                            <Save className="mr-1.5 h-4 w-4 shrink-0" />
                                                            {t('common.save')}
                                                        </Button>
                                                    </div>
                                                </form>
                                            </div>
                                        )}

                                        {upstreams.length === 0 ? (
                                            <div className="rounded-3xl border border-dashed border-border/60 bg-muted/10 px-6 py-20 text-center">
                                                <Upload className="mx-auto mb-4 h-10 w-10 text-muted-foreground/30" />
                                                <p className="text-sm text-foreground/75">
                                                    {t('upstream_manager.no_upstreams')}
                                                </p>
                                            </div>
                                        ) : (
                                            <div className="space-y-0">
                                                <div className="hidden grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)_150px_100px_96px] gap-6 border-b border-border/40 pb-3 px-2 lg:grid">
                                                    <span className="text-xs font-semibold uppercase tracking-wider text-foreground/65">{t('upstream_manager.name')}</span>
                                                    <span className="text-xs font-semibold uppercase tracking-wider text-foreground/65">{t('upstream_manager.target')}</span>
                                                    <span className="text-xs font-semibold uppercase tracking-wider text-foreground/65">{t('upstream_manager.outbound_proxy')}</span>
                                                    <span className="text-xs font-semibold uppercase tracking-wider text-foreground/65">{t('upstream_manager.timeout')}</span>
                                                    <span className="text-xs font-semibold uppercase tracking-wider text-foreground/65">{t('upstream_manager.actions')}</span>
                                                </div>

                                                <div className="divide-y divide-border/20">
                                                    {sortedUpstreams.map(upstream => (
                                                        <div
                                                            key={upstream.name}
                                                            className="group grid gap-5 py-5 px-2 transition-colors hover:bg-muted/20 rounded-xl lg:-mx-2 lg:px-4 lg:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)_150px_100px_96px] lg:items-start lg:gap-6"
                                                        >
                                                            <div className="min-w-0 space-y-3">
                                                                <div className="flex flex-wrap items-center gap-2">
                                                                    <span className="text-base font-semibold text-foreground">
                                                                        {upstream.name}
                                                                    </span>
                                                                    <Badge variant="outline" className="rounded-full border-border/40 bg-background/50 px-2 py-0.5 text-[11px] font-medium text-foreground/80">
                                                                        .{domainSuffix}
                                                                    </Badge>
                                                                    {overrideBindings[upstream.name]?.enabled && (
                                                                        <Badge variant="outline" className="rounded-full border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-600 dark:text-amber-400">
                                                                            {t('log_detail.request_override')}
                                                                            {overrideBindings[upstream.name]?.rule_names?.length
                                                                                ? ` · ${overrideBindings[upstream.name].rule_names.length}`
                                                                                : ''}
                                                                        </Badge>
                                                                    )}
                                                                </div>

                                                                <div className="space-y-2">
                                                                    <button
                                                                        type="button"
                                                                        onClick={() => handleCopy(getProxyUrl(upstream.name))}
                                                                        className="flex items-start gap-2 text-left text-[13px] leading-relaxed text-primary/80 transition-colors hover:text-primary"
                                                                    >
                                                                        <Copy className="mt-1 h-3.5 w-3.5 shrink-0" />
                                                                        <span className="break-all font-mono">{getProxyUrl(upstream.name)}</span>
                                                                    </button>

                                                                    {enablePathRouting && (
                                                                        <button
                                                                            type="button"
                                                                            onClick={() => handleCopy(getPathProxyUrl(upstream.name))}
                                                                            className="flex items-start gap-2 text-left text-[13px] leading-relaxed text-emerald-600/80 transition-colors hover:text-emerald-600 dark:text-emerald-400/80 dark:hover:text-emerald-400"
                                                                        >
                                                                            <Copy className="mt-1 h-3.5 w-3.5 shrink-0" />
                                                                            <span className="break-all font-mono">{getPathProxyUrl(upstream.name)}</span>
                                                                        </button>
                                                                    )}
                                                                </div>
                                                            </div>

                                                            <div className="min-w-0">
                                                                <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-foreground/60 lg:hidden">
                                                                    {t('upstream_manager.target')}
                                                                </p>
                                                                <div className="text-[13px] leading-relaxed">
                                                                    <button
                                                                        type="button"
                                                                        onClick={() => handleCopy(upstream.target)}
                                                                        className="flex items-start gap-2 text-left text-foreground/80 transition-colors hover:text-primary dark:hover:text-primary-foreground group/target"
                                                                    >
                                                                        <Copy className="mt-1 h-3.5 w-3.5 shrink-0 opacity-40 group-hover/target:opacity-100 transition-opacity" />
                                                                        <span className="break-all font-mono">{upstream.target}</span>
                                                                    </button>
                                                                </div>
                                                            </div>

                                                            <div className="min-w-0">
                                                                <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-foreground/50 lg:hidden">
                                                                    {t('upstream_manager.outbound_proxy')}
                                                                </p>
                                                                <div className="text-[13px] font-medium text-foreground/80">
                                                                    {outboundProxyMode(upstream.outbound_proxy) === 'custom' ? (
                                                                        <button
                                                                            type="button"
                                                                            onClick={() => handleCopy(upstream.outbound_proxy)}
                                                                            className="flex items-start gap-2 text-left transition-colors hover:text-primary"
                                                                        >
                                                                            <Copy className="mt-1 h-3.5 w-3.5 shrink-0 opacity-40" />
                                                                            <span className="break-all font-mono">{upstream.outbound_proxy}</span>
                                                                        </button>
                                                                    ) : (
                                                                        <span>{formatOutboundProxy(upstream.outbound_proxy)}</span>
                                                                    )}
                                                                </div>
                                                            </div>

                                                            <div>
                                                                <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-foreground/50 lg:hidden">
                                                                    {t('upstream_manager.timeout')}
                                                                </p>
                                                                <div className="text-[13px] font-medium text-foreground/80">
                                                                    {upstream.timeout}s
                                                                </div>
                                                            </div>

                                                            <div className="flex flex-col items-start gap-2 opacity-100 lg:opacity-0 transition-opacity duration-200 group-hover:opacity-100">
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="sm"
                                                                    onClick={() => handleEditUpstream(upstream)}
                                                                    className="h-8 justify-start rounded-lg px-2.5 text-foreground/65 hover:bg-primary/10 hover:text-primary"
                                                                >
                                                                    <Pencil className="h-4 w-4 mr-1.5" />
                                                                    {t('common.edit')}
                                                                </Button>
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="sm"
                                                                    onClick={() => handleRemoveUpstream(upstream.name)}
                                                                    className="h-8 justify-start rounded-lg px-2.5 text-foreground/65 hover:bg-destructive/10 hover:text-destructive"
                                                                >
                                                                    <Trash2 className="h-4 w-4 mr-1.5" />
                                                                    {t('common.delete')}
                                                                </Button>
                                                            </div>
                                                        </div>
                                                    ))}
                                                </div>
                                            </div>
                                        )}
                                    </div>
                                </section>
                            </div>
                        )}

                        {activeTab === 'logging' && (
                            <div className="space-y-12 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                <div className="max-w-3xl space-y-10">
                                    
                                    {/* 表单项网格 */}
                                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-12 gap-y-8">
                                        <FieldBlock
                                            label={t('settings.max_request_body')}
                                            hint={t('settings.max_request_body_hint')}
                                            htmlFor="max-req"
                                            unit="KB"
                                        >
                                            <Input
                                                id="max-req"
                                                type="number"
                                                min="1"
                                                value={maxRequestBody}
                                                onChange={e => setMaxRequestBody(Number(e.target.value))}
                                                className="h-11 rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                            />
                                        </FieldBlock>

                                        <FieldBlock
                                            label={t('settings.max_response_body')}
                                            hint={t('settings.max_response_body_hint')}
                                            htmlFor="max-res"
                                            unit="KB"
                                        >
                                            <Input
                                                id="max-res"
                                                type="number"
                                                min="1"
                                                value={maxResponseBody}
                                                onChange={e => setMaxResponseBody(Number(e.target.value))}
                                                className="h-11 rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                            />
                                        </FieldBlock>

                                        <FieldBlock
                                            label={t('settings.detach_body_over_bytes')}
                                            hint={t('settings.detach_body_over_bytes_hint')}
                                            htmlFor="detach-over"
                                            unit="KB"
                                        >
                                            <Input
                                                id="detach-over"
                                                type="number"
                                                min="0"
                                                value={detachBodyOver}
                                                onChange={e => setDetachBodyOver(Number(e.target.value))}
                                                className="h-11 rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                            />
                                        </FieldBlock>

                                        <FieldBlock
                                            label={t('settings.retention_days')}
                                            hint={t('settings.retention_days_hint')}
                                            htmlFor="retention-days"
                                            unit={t('settings.days')}
                                        >
                                            <Input
                                                id="retention-days"
                                                type="number"
                                                min="0"
                                                value={retentionDays}
                                                onChange={e => setRetentionDays(Number(e.target.value))}
                                                className="h-11 rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                            />
                                        </FieldBlock>

                                        {/* 开关项目放入最后网格列中 */}
                                        <div className="flex flex-col justify-center gap-y-5 pt-3">
                                            <ToggleSetting
                                                label={t('settings.early_request_body_snapshot')}
                                                description={t('settings.early_request_body_snapshot_hint')}
                                                checked={earlyRequestBodySnapshot}
                                                onCheckedChange={setEarlyRequestBodySnapshot}
                                            />

                                            <ToggleSetting
                                                label={t('settings.store_base64')}
                                                description={t('settings.store_base64_hint')}
                                                checked={storeBase64}
                                                onCheckedChange={setStoreBase64}
                                            />
                                        </div>
                                    </div>

                                    {/* 文本输入池 */}
                                    <div className="pt-2">
                                        <FieldBlock
                                            label={t('settings.sensitive_headers')}
                                            hint={t('settings.sensitive_headers_hint')}
                                        >
                                            <Textarea
                                                value={sensitiveHeaders}
                                                onChange={e => setSensitiveHeaders(e.target.value)}
                                                rows={5}
                                                className="min-h-[140px] w-full rounded-xl border-border/30 bg-background/50 font-mono text-sm leading-relaxed shadow-sm transition-colors focus-visible:bg-background resize-y"
                                                placeholder="Authorization&#10;x-api-key&#10;api-key"
                                            />
                                        </FieldBlock>
                                    </div>

                                    {/* 操作区 */}
                                    <div className="flex justify-center pt-4">
                                        <Button
                                            type="button"
                                            onClick={handleSaveAll}
                                            disabled={saving}
                                            variant="default"
                                            size="lg"
                                            className="h-11 rounded-xl font-medium shadow-sm transition-all whitespace-nowrap shrink-0"
                                        >
                                            <Save className="mr-2 h-4 w-4 shrink-0" />
                                            {t('common.save')}
                                        </Button>
                                    </div>
                                </div>
                            </div>
                        )}

                        {activeTab === 'system' && (
                            <div className="space-y-8 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                {metricsError && (
                                    <div className="flex items-start gap-3 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                        <span>{metricsError}</span>
                                    </div>
                                )}

                                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                                    <MetricCard
                                        icon={<Activity className="h-4 w-4" />}
                                        label={t('settings.system_process_memory')}
                                        value={formatBytes(metrics?.process.rss_bytes)}
                                        detail={t('settings.system_heap_detail', {
                                            alloc: formatBytes(metrics?.process.heap_alloc_bytes),
                                            sys: formatBytes(metrics?.process.heap_sys_bytes),
                                        })}
                                    />
                                    <MetricCard
                                        icon={<Cpu className="h-4 w-4" />}
                                        label={t('settings.system_process_cpu')}
                                        value={formatPercent(metrics?.process.cpu_percent)}
                                        detail={t('settings.system_cpu_detail', {
                                            seconds: metrics?.process.cpu_seconds?.toFixed(1) ?? '-',
                                        })}
                                    />
                                    <MetricCard
                                        icon={<MemoryStick className="h-4 w-4" />}
                                        label={t('settings.system_total_memory')}
                                        value={formatPercent(memoryUsedPercent)}
                                        detail={t('settings.system_memory_detail', {
                                            used: formatBytes(metrics?.memory.used_bytes),
                                            total: formatBytes(metrics?.memory.total_bytes),
                                            source: metrics?.memory.source || '-',
                                        })}
                                    />
                                    <MetricCard
                                        icon={<Timer className="h-4 w-4" />}
                                        label={t('settings.system_uptime')}
                                        value={formatDuration(metrics?.runtime.uptime_seconds)}
                                        detail={t('settings.system_runtime_detail', {
                                            goroutines: metrics?.runtime.goroutines ?? '-',
                                            cpu: metrics?.runtime.num_cpu ?? '-',
                                        })}
                                    />
                                </div>

                                <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                                    <div className="rounded-xl border border-border/40 bg-background/45 px-5 py-4">
                                        <div className="mb-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                            {t('settings.system_runtime')}
                                        </div>
                                        <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                                            <dt className="text-muted-foreground">{t('settings.system_platform')}</dt>
                                            <dd className="font-mono text-foreground">{metrics?.platform || '-'}</dd>
                                            <dt className="text-muted-foreground">{t('settings.system_pid')}</dt>
                                            <dd className="font-mono text-foreground">{metrics?.process.pid ?? '-'}</dd>
                                            <dt className="text-muted-foreground">{t('settings.system_go_version')}</dt>
                                            <dd className="break-all font-mono text-foreground">{metrics?.runtime.go_version || '-'}</dd>
                                        </dl>
                                    </div>
                                    <div className="rounded-xl border border-border/40 bg-background/45 px-5 py-4">
                                        <div className="mb-3 flex items-center justify-between gap-3">
                                            <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                {t('settings.system_snapshot')}
                                            </div>
                                            <Button
                                                type="button"
                                                variant="outline"
                                                size="sm"
                                                onClick={() => loadMetrics(true)}
                                                disabled={metricsLoading}
                                                className="h-8 rounded-lg px-2.5 text-xs"
                                            >
                                                <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", metricsLoading && "animate-spin")} />
                                                {t('common.refresh')}
                                            </Button>
                                        </div>
                                        <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                                            <dt className="text-muted-foreground">{t('settings.system_updated_at')}</dt>
                                            <dd className="font-mono text-foreground">{metricsUpdatedAt}</dd>
                                            <dt className="text-muted-foreground">{t('settings.system_available_memory')}</dt>
                                            <dd className="font-mono text-foreground">{formatBytes(metrics?.memory.available_bytes)}</dd>
                                            <dt className="text-muted-foreground">{t('settings.system_refresh_interval')}</dt>
                                            <dd className="font-mono text-foreground">5s</dd>
                                        </dl>
                                    </div>
                                </div>

                                <div className="rounded-xl border border-border/40 bg-background/45 px-5 py-4">
                                    <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                                        <div>
                                            <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                {t('settings.storage_usage_title')}
                                            </div>
                                            <div className="mt-2 text-sm text-muted-foreground">
                                                {t('settings.storage_usage_calculated_at', {
                                                    time: storageCalculatedAt,
                                                })}
                                            </div>
                                        </div>
                                        <Button
                                            type="button"
                                            variant="outline"
                                            onClick={handleCalculateStorage}
                                            disabled={storageLoading}
                                            className="h-10 rounded-xl"
                                        >
                                            <RefreshCw className={cn("mr-2 h-4 w-4", storageLoading && "animate-spin")} />
                                            {t('settings.storage_usage_calculate')}
                                        </Button>
                                    </div>

                                    {storageError && (
                                        <div className="mb-4 flex items-start gap-3 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                            <span>{storageError}</span>
                                        </div>
                                    )}

                                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                                        <div className="rounded-lg border border-border/30 bg-background/40 px-4 py-3">
                                            <div className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                <Archive className="h-3.5 w-3.5" />
                                                {t('settings.storage_usage_total')}
                                            </div>
                                            <div className="text-xl font-semibold text-foreground">
                                                {formatBytes(storageUsage?.total_bytes)}
                                            </div>
                                            <div className="mt-1 text-xs text-muted-foreground">
                                                {t('settings.storage_usage_blob_store', {
                                                    store: storageUsage?.blob_store || '-',
                                                })}
                                            </div>
                                        </div>
                                        <div className="rounded-lg border border-border/30 bg-background/40 px-4 py-3">
                                            <div className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                <Database className="h-3.5 w-3.5" />
                                                {t('settings.storage_usage_database')}
                                            </div>
                                            <div className="text-xl font-semibold text-foreground">
                                                {formatBytes(storageUsage?.database_bytes)}
                                            </div>
                                            <div className="mt-1 text-xs text-muted-foreground">
                                                {storageUsage
                                                    ? t('settings.storage_usage_files', { count: storageUsage.database_files })
                                                    : '-'}
                                            </div>
                                        </div>
                                        <div className="rounded-lg border border-border/30 bg-background/40 px-4 py-3">
                                            <div className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                <Archive className="h-3.5 w-3.5" />
                                                {t('settings.storage_usage_blobs')}
                                            </div>
                                            <div className="text-xl font-semibold text-foreground">
                                                {formatBytes(storageUsage?.blob_bytes)}
                                            </div>
                                            <div className="mt-1 text-xs text-muted-foreground">
                                                {storageUsage
                                                    ? t('settings.storage_usage_files', { count: storageUsage.blob_files })
                                                    : '-'}
                                            </div>
                                        </div>
                                    </div>
                                </div>

                                <div className="rounded-xl border border-border/40 bg-background/45 px-5 py-4">
                                    <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                                        <div>
                                            <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                {t('settings.update_title')}
                                            </div>
                                            <div className="mt-2 text-sm text-muted-foreground">
                                                {t('settings.update_current_version', {
                                                    version: config?.version ? `v${config.version.replace(/^v/, '')}` : '-',
                                                })}
                                            </div>
                                        </div>
                                        <Button
                                            type="button"
                                            variant="outline"
                                            onClick={handleCheckUpdate}
                                            disabled={updateLoading}
                                            className="h-10 rounded-xl"
                                        >
                                            <RefreshCw className={cn("mr-2 h-4 w-4", updateLoading && "animate-spin")} />
                                            {t('settings.update_check')}
                                        </Button>
                                    </div>

                                    {updateError && (
                                        <div className="mb-4 flex items-start gap-3 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                            <span>{updateError}</span>
                                        </div>
                                    )}

                                    {updateInfo && (
                                        <div className="space-y-4">
                                            <div className="flex flex-wrap items-center gap-3">
                                                <Badge
                                                    variant="outline"
                                                    className={cn(
                                                        "rounded-full px-3 py-1 text-xs font-semibold",
                                                        updateInfo.update_available
                                                            ? "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400"
                                                            : "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                                                    )}
                                                >
                                                    {updateInfo.update_available ? t('settings.update_available') : t('settings.update_latest')}
                                                </Badge>
                                                {updateInfo.update_available && (
                                                    <span className="text-sm text-muted-foreground">
                                                        {t('settings.update_latest_version', {
                                                            version: updateInfo.latest_tag || `v${updateInfo.latest_version}`,
                                                        })}
                                                    </span>
                                                )}
                                            </div>

                                            {updateInfo.update_available && (
                                                <div className="flex flex-wrap gap-2">
                                                    {updateInfo.matching_asset && (
                                                        <Button asChild className="h-10 rounded-xl">
                                                            <a href={updateInfo.matching_asset.download_url} target="_blank" rel="noreferrer noopener">
                                                                <Download className="mr-2 h-4 w-4" />
                                                                {t('settings.update_download_asset', {
                                                                    size: formatBytes(updateInfo.matching_asset.size),
                                                                })}
                                                            </a>
                                                        </Button>
                                                    )}
                                                    <Button asChild variant="outline" className="h-10 rounded-xl">
                                                        <a href={updateInfo.release_url} target="_blank" rel="noreferrer noopener">
                                                            <ExternalLink className="mr-2 h-4 w-4" />
                                                            {t('settings.update_open_release')}
                                                        </a>
                                                    </Button>
                                                </div>
                                            )}
                                        </div>
                                    )}
                                </div>
                            </div>
                        )}

                        {activeTab === 'overrides' && (
                            <div className="space-y-10 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                <div className="max-w-4xl space-y-8">
                                    <div className="flex items-start gap-3 rounded-xl border border-yellow-500/30 bg-yellow-500/10 px-4 py-3 text-sm text-yellow-800 dark:text-yellow-300">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                        <div className="space-y-1">
                                            <p className="font-semibold">{t('settings.request_overrides_warning_title')}</p>
                                            <p className="text-xs leading-6 opacity-90">{t('settings.request_overrides_warning')}</p>
                                        </div>
                                    </div>

                                    <div className="grid grid-cols-1 gap-x-12 gap-y-8 sm:grid-cols-2">
                                        <div className="flex items-center pt-7">
                                            <ToggleSetting
                                                label={t('settings.request_overrides_enable')}
                                                description={t('settings.request_overrides_enable_hint')}
                                                checked={requestOverridesEnabled}
                                                onCheckedChange={setRequestOverridesEnabled}
                                            />
                                        </div>

                                        <FieldBlock
                                            label={t('settings.request_overrides_max_body')}
                                            hint={t('settings.request_overrides_max_body_hint')}
                                            htmlFor="override-max-body"
                                            unit="KB"
                                        >
                                            <Input
                                                id="override-max-body"
                                                type="number"
                                                min="1"
                                                value={overrideMaxBodyKB}
                                                onChange={e => setOverrideMaxBodyKB(Number(e.target.value))}
                                                className="h-11 rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                            />
                                        </FieldBlock>
                                    </div>

                                    <FieldBlock
                                        label={t('settings.request_overrides_rules')}
                                        hint={t('settings.request_overrides_rules_hint')}
                                    >
                                        <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/40 bg-muted/30 px-3 py-2">
                                            <p className="text-xs leading-5 text-muted-foreground">
                                                {t('settings.request_overrides_scope_hint')}
                                            </p>
                                            <div className="flex shrink-0 items-center gap-2">
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    asChild
                                                    className="h-8 text-xs font-semibold"
                                                >
                                                    <a
                                                        href="https://jsonpatch.com/"
                                                        target="_blank"
                                                        rel="noreferrer noopener"
                                                    >
                                                        <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
                                                        {t('settings.request_overrides_learn_more')}
                                                    </a>
                                                </Button>
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    onClick={() => setOverrideRulesText(requestOverrideExample)}
                                                    className="h-8 text-xs font-semibold"
                                                >
                                                    <Plus className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('settings.request_overrides_insert_example')}
                                                </Button>
                                            </div>
                                        </div>
                                        <Textarea
                                            value={overrideRulesText}
                                            onChange={e => setOverrideRulesText(e.target.value)}
                                            rows={18}
                                            spellCheck={false}
                                            className="min-h-[420px] w-full rounded-xl border-border/30 bg-background/50 font-mono text-xs leading-relaxed shadow-sm transition-colors focus-visible:bg-background resize-y"
                                            placeholder={requestOverrideExample}
                                        />
                                    </FieldBlock>

                                    <div className="flex justify-center pt-2">
                                        <Button
                                            type="button"
                                            onClick={handleSaveAll}
                                            disabled={saving}
                                            variant="default"
                                            size="lg"
                                            className="h-11 font-medium shadow-sm transition-all whitespace-nowrap shrink-0"
                                        >
                                            <Save className="mr-2 h-4 w-4 shrink-0" />
                                            {t('common.save')}
                                        </Button>
                                    </div>
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            </div>
        </div>
    )
}
