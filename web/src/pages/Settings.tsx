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
    FileCode,
    ChevronDown,
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
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from '@/components/ui/tooltip'
import { DEFAULT_UPSTREAM_TIMEOUT_SECONDS, fetchUpstreams, addUpstream, removeUpstream, fetchConfig, updateConfig, fetchSystemMetrics, fetchUpdateInfo, fetchStorageUsage } from '@/lib/api'
import type { Upstream, AppConfig, SystemMetrics, UpdateInfo, StorageUsage } from '@/lib/api'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'

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
            { op: 'set', path: 'max_tokens', value: 2048 },
        ],
    },
    {
        name: 'Dev-only: downgrade gpt-4o to gpt-4o-mini',
        enabled: false,
        match: {
            methods: ['POST'],
            json: [
                { path: 'model', equals: 'gpt-4o' },
            ],
        },
        patch: [
            { op: 'set', path: 'model', value: 'gpt-4o-mini' },
        ],
    },
    {
        name: 'Inject device metadata for Claude requests',
        enabled: false,
        match: {
            methods: ['POST'],
            path_prefixes: ['/v1/messages'],
            json: [
                { path: 'model', starts_with: 'claude' },
            ],
        },
        patch: [
            { op: 'set', path: 'metadata.user_id', value: 'demo-user' },
        ],
    },
    {
        name: 'Default max_tokens only when missing',
        enabled: false,
        match: {
            methods: ['POST'],
        },
        patch: [
            { op: 'default', path: 'max_tokens', value: 4096 },
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
            { op: 'remove', path: 'user' },
        ],
    },
], null, 2)

const usageExtractionExample = JSON.stringify([
    {
        name: 'OpenAI compatible',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usage/prompt_tokens', '/usage/input_tokens'],
            output_tokens: ['/usage/completion_tokens', '/usage/output_tokens'],
            total_tokens: ['/usage/total_tokens'],
            raw_usage: ['/usage'],
        },
    },
    {
        name: 'OpenAI Responses',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usage/input_tokens', '/response/usage/input_tokens'],
            output_tokens: ['/usage/output_tokens', '/response/usage/output_tokens'],
            total_tokens: ['/usage/total_tokens', '/response/usage/total_tokens'],
            raw_usage: ['/usage', '/response/usage'],
        },
    },
    {
        name: 'Anthropic',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usage/input_tokens', '/message/usage/input_tokens'],
            output_tokens: ['/usage/output_tokens', '/message/usage/output_tokens'],
            raw_usage: ['/usage', '/message/usage'],
        },
    },
    {
        name: 'Gemini',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usageMetadata/promptTokenCount'],
            output_tokens: ['/usageMetadata/candidatesTokenCount'],
            total_tokens: ['/usageMetadata/totalTokenCount'],
            raw_usage: ['/usageMetadata'],
        },
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
    responseHeaderTimeout: number
    responseBodyFirstByteTimeout: number
    responseBodyIdleTimeout: number
    order: number
    outboundProxy: string
    loggingEnabled: boolean
    overrideEnabled: boolean
    ruleNames: string[]
    usageEnabled: boolean
    usageRuleNames: string[]
}

type OutboundProxyMode = 'env' | 'direct' | 'custom'
type SettingsTab = 'routing' | 'logging' | 'overrides' | 'system'
type RuleTab = 'request_overrides' | 'usage_extraction'
type OverrideRuleObject = Record<string, unknown>
type HeaderOp = { op: string; name: string; value?: string }

const customProxyPlaceholder = 'http://127.0.0.1:7890'

function getOverrideRuleName(rule: unknown, fallback: string) {
    if (!rule || typeof rule !== 'object') return fallback
    const name = (rule as { name?: unknown }).name
    return typeof name === 'string' && name.trim() ? name.trim() : fallback
}

function getOverrideRuleEnabled(rule: unknown) {
    if (!rule || typeof rule !== 'object') return false
    const enabled = (rule as { enabled?: unknown }).enabled
    return enabled !== false
}

function getBindingRuleNames(binding?: OverrideBinding) {
    return Array.isArray(binding?.rule_names) ? binding.rule_names : []
}

type RuleRuntimeStatus =
    | { kind: 'active'; enabledUpstreams: string[]; disabledUpstreams: string[] }
    | { kind: 'blocked'; reason: 'global' | 'rule' | 'bindings'; enabledUpstreams: string[]; disabledUpstreams: string[] }
    | { kind: 'unbound' }

function getRuleRuntimeStatus(
    rule: OverrideRuleObject,
    bindings: Record<string, OverrideBinding>,
    globalEnabled: boolean,
): RuleRuntimeStatus {
    const ruleName = getOverrideRuleName(rule, '')
    if (!ruleName) return { kind: 'unbound' }

    const enabledUpstreams: string[] = []
    const disabledUpstreams: string[] = []
    for (const [name, binding] of Object.entries(bindings)) {
        if (getBindingRuleNames(binding).includes(ruleName)) {
            if (binding.enabled) enabledUpstreams.push(name)
            else disabledUpstreams.push(name)
        }
    }

    if (enabledUpstreams.length + disabledUpstreams.length === 0) {
        return { kind: 'unbound' }
    }
    if (!globalEnabled) {
        return { kind: 'blocked', reason: 'global', enabledUpstreams, disabledUpstreams }
    }
    if (!getOverrideRuleEnabled(rule)) {
        return { kind: 'blocked', reason: 'rule', enabledUpstreams, disabledUpstreams }
    }
    if (enabledUpstreams.length === 0) {
        return { kind: 'blocked', reason: 'bindings', enabledUpstreams, disabledUpstreams }
    }
    return { kind: 'active', enabledUpstreams, disabledUpstreams }
}

function formatUpstreamList(
    enabled: string[],
    disabled: string[],
    t: (key: string, options?: Record<string, unknown>) => string,
): string {
    const summarize = (names: string[]) => {
        if (names.length <= 2) return names.join(', ')
        return `${names.slice(0, 2).join(', ')} +${names.length - 2}`
    }
    const parts: string[] = []
    if (enabled.length > 0) parts.push(`→ ${summarize(enabled)}`)
    if (disabled.length > 0) parts.push(t('settings.rule_status_disabled_upstreams', { list: summarize(disabled) }))
    return parts.join('  ·  ')
}

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
    className,
}: {
    value: string
    onChange: (value: string) => void
    t: (key: string) => string
    className?: string
}) {
    const mode = outboundProxyMode(value)
    const [customValue, setCustomValue] = useState(
        mode === 'custom' ? value : customProxyPlaceholder,
    )

    return (
        <div
            className={cn(
                "relative flex h-10 overflow-hidden rounded-xl border border-border/30 bg-background/50 transition-shadow focus-within:ring-2 focus-within:ring-ring/50",
                className,
            )}
        >
            {mode === 'custom' && (
                <Input
                    value={value}
                    onChange={e => {
                        setCustomValue(e.target.value)
                        onChange(e.target.value)
                    }}
                    placeholder={customProxyPlaceholder}
                    aria-label={t('upstream_manager.outbound_proxy_custom_address')}
                    className="h-full min-w-0 flex-1 rounded-none border-0 bg-transparent px-3 pr-11 font-mono text-sm shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
                />
            )}
            <Select
                value={mode}
                onValueChange={(nextMode: OutboundProxyMode) => {
                    if (nextMode === 'custom') {
                        onChange(mode === 'custom' ? value : customValue)
                        return
                    }
                    onChange(nextMode)
                }}
            >
                <SelectTrigger
                    className={cn(
                        "h-full rounded-none border-0 bg-transparent text-sm shadow-none focus:ring-0 focus:ring-offset-0",
                        mode === 'custom'
                            ? "absolute inset-y-0 right-0 z-10 w-10 justify-center px-0 hover:bg-muted/40"
                            : "w-full",
                    )}
                >
                    {mode === 'custom' ? (
                        <span className="sr-only">
                            <SelectValue />
                        </span>
                    ) : (
                        <SelectValue />
                    )}
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="env">{t('upstream_manager.outbound_proxy_env')}</SelectItem>
                    <SelectItem value="direct">{t('upstream_manager.outbound_proxy_direct')}</SelectItem>
                    <SelectItem value="custom">{t('upstream_manager.outbound_proxy_custom')}</SelectItem>
                </SelectContent>
            </Select>
        </div>
    )
}


function InfoTooltip({ content }: { content: string }) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <button
                    type="button"
                    onClick={event => event.stopPropagation()}
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

function AdvancedSettings({
    children,
    open: controlledOpen,
    onOpenChange,
    defaultOpen = false,
    sidePanel = false,
}: {
    children: ReactNode
    open?: boolean
    onOpenChange?: (open: boolean) => void
    defaultOpen?: boolean
    sidePanel?: boolean
}) {
    const { t } = useTranslation()
    const [internalOpen, setInternalOpen] = useState(defaultOpen)
    const open = controlledOpen ?? internalOpen
    const setOpen = (nextOpen: boolean) => {
        if (controlledOpen === undefined) setInternalOpen(nextOpen)
        onOpenChange?.(nextOpen)
    }
    return (
        <section
            className={cn(
                "rounded-xl border border-border/50 bg-muted/20",
                open && sidePanel && "lg:flex lg:min-h-0 lg:flex-col lg:rounded-none lg:border-y-0 lg:border-r-0",
            )}
        >
            <button
                type="button"
                aria-expanded={open}
                onClick={() => setOpen(!open)}
                className={cn(
                    "flex w-full cursor-pointer items-center justify-between gap-4 px-4 py-3 text-left",
                    sidePanel && "lg:px-6",
                )}
            >
                <div className="flex min-w-0 items-center gap-2">
                    <div className="text-sm font-medium text-foreground">
                        {t('upstream_manager.advanced_settings')}
                    </div>
                    <InfoTooltip content={t('upstream_manager.advanced_settings_hint')} />
                </div>
                <ChevronDown className={cn(
                    "h-4 w-4 shrink-0 text-muted-foreground transition-transform",
                    open && "rotate-180",
                )} />
            </button>
            {open && (
                <div className={cn(
                    "space-y-6 border-t border-border/40 px-4 py-5",
                    sidePanel && "lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:px-6",
                )}>
                    {children}
                </div>
            )}
        </section>
    )
}

function AdvancedSettingsGroup({
    title,
    description,
    children,
}: {
    title: string
    description?: string
    children: ReactNode
}) {
    return (
        <div className="space-y-4">
            <div>
                <div className="text-xs font-semibold uppercase tracking-wider text-foreground/65">{title}</div>
                {description && <p className="mt-1.5 text-xs leading-5 text-muted-foreground">{description}</p>}
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

function SettingSection({
    title,
    description,
    action,
    children,
}: {
    title: string
    description?: string
    action?: ReactNode
    children: ReactNode
}) {
    return (
        <section className="overflow-hidden rounded-2xl border border-border/50 bg-card/40 shadow-sm">
            <div className="flex items-start justify-between gap-4 border-b border-border/40 px-6 py-4">
                <div className="min-w-0">
                    <h3 className="text-sm font-semibold text-foreground">{title}</h3>
                    {description && (
                        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{description}</p>
                    )}
                </div>
                {action && <div className="shrink-0">{action}</div>}
            </div>
            <div className="px-6 py-6">{children}</div>
        </section>
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
    const [activeRuleTab, setActiveRuleTab] = useState<RuleTab>('request_overrides')
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
    const [newTimeout, setNewTimeout] = useState(DEFAULT_UPSTREAM_TIMEOUT_SECONDS)
    const [newResponseHeaderTimeout, setNewResponseHeaderTimeout] = useState(0)
    const [newResponseBodyFirstByteTimeout, setNewResponseBodyFirstByteTimeout] = useState(0)
    const [newResponseBodyIdleTimeout, setNewResponseBodyIdleTimeout] = useState(0)
    const [newOrder, setNewOrder] = useState(100)
    const [newOutboundProxy, setNewOutboundProxy] = useState('env')
    const [newLoggingEnabled, setNewLoggingEnabled] = useState(true)
    const [editingUpstream, setEditingUpstream] = useState<EditingUpstream | null>(null)
    const [upstreamAdvancedOpen, setUpstreamAdvancedOpen] = useState(false)

    const [enablePathRouting, setEnablePathRouting] = useState(false)
    const [pathRoutingPrefix, setPathRoutingPrefix] = useState('/_proxy')

    const [maxRequestBody, setMaxRequestBody] = useState(5120)
    const [maxResponseBody, setMaxResponseBody] = useState(32768)
    const [sensitiveHeaders, setSensitiveHeaders] = useState('')
    const [detachBodyOver, setDetachBodyOver] = useState(2048)
    const [bodyPreview, setBodyPreview] = useState(512)
    const [storeBase64, setStoreBase64] = useState(true)
    const [earlyRequestBodySnapshot, setEarlyRequestBodySnapshot] = useState(false)

    const [retentionDays, setRetentionDays] = useState(30)
    const [maxStorageGB, setMaxStorageGB] = useState(0)
    const [requestOverridesEnabled, setRequestOverridesEnabled] = useState(false)
    const [overrideMaxBodyKB, setOverrideMaxBodyKB] = useState(1024)
    const [overrideRulesText, setOverrideRulesText] = useState('')
    const [selectedOverrideRuleIndex, setSelectedOverrideRuleIndex] = useState(0)
    const [selectedOverrideRuleText, setSelectedOverrideRuleText] = useState('')
    const [advancedRulesOpen, setAdvancedRulesOpen] = useState(false)
    const [overrideBindings, setOverrideBindings] = useState<Record<string, OverrideBinding>>({})
    const [usageExtractionEnabled, setUsageExtractionEnabled] = useState(false)
    const [usageRulesText, setUsageRulesText] = useState('')
    const [selectedUsageRuleIndex, setSelectedUsageRuleIndex] = useState(0)
    const [selectedUsageRuleText, setSelectedUsageRuleText] = useState('')
    const [usageAdvancedRulesOpen, setUsageAdvancedRulesOpen] = useState(false)
    const [usageBindings, setUsageBindings] = useState<Record<string, OverrideBinding>>({})

    const domainSuffix = config?.server?.proxy_domains?.[0] || 'localhost'
    const previewUpstreamName = upstreams[0]?.name || 'openai'
    const sortedUpstreams = useMemo(() => {
        return [...upstreams].sort((a, b) => {
            const orderDiff = (a.order || 0) - (b.order || 0)
            if (orderDiff !== 0) return orderDiff
            return a.name.localeCompare(b.name)
        })
    }, [upstreams])
    const overrideRulesParse = useMemo((): { rules: OverrideRuleObject[]; error: string } => {
        try {
            const parsed = overrideRulesText.trim() ? JSON.parse(overrideRulesText) : []
            if (!Array.isArray(parsed)) {
                return { rules: [], error: t('settings.request_overrides_rules_must_be_array') }
            }
            return {
                rules: parsed.filter((rule): rule is OverrideRuleObject => Boolean(rule) && typeof rule === 'object' && !Array.isArray(rule)),
                error: '',
            }
        } catch {
            return { rules: [], error: t('settings.request_overrides_rules_invalid') }
        }
    }, [overrideRulesText, t])
    const overrideRuleObjects = overrideRulesParse.rules
    const parsedOverrideRules = useMemo(() => {
        return overrideRuleObjects.map((rule, index) => getOverrideRuleName(rule, `rule-${index + 1}`))
    }, [overrideRuleObjects])
    const selectedOverrideRuleError = useMemo(() => {
        if (!selectedOverrideRuleText.trim()) return ''
        try {
            const parsed = JSON.parse(selectedOverrideRuleText)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                return t('settings.request_override_rule_must_be_object')
            }
            return ''
        } catch {
            return t('settings.request_override_rule_invalid')
        }
    }, [selectedOverrideRuleText, t])
    const usageRulesParse = useMemo((): { rules: OverrideRuleObject[]; error: string } => {
        try {
            const parsed = usageRulesText.trim() ? JSON.parse(usageRulesText) : []
            if (!Array.isArray(parsed)) {
                return { rules: [], error: t('settings.usage_extraction_rules_must_be_array') }
            }
            return {
                rules: parsed.filter((rule): rule is OverrideRuleObject => Boolean(rule) && typeof rule === 'object' && !Array.isArray(rule)),
                error: '',
            }
        } catch {
            return { rules: [], error: t('settings.usage_extraction_rules_invalid') }
        }
    }, [usageRulesText, t])
    const usageRuleObjects = usageRulesParse.rules
    const parsedUsageRules = useMemo(() => {
        return usageRuleObjects.map((rule, index) => getOverrideRuleName(rule, `usage-rule-${index + 1}`))
    }, [usageRuleObjects])
    const selectedUsageRuleError = useMemo(() => {
        if (!selectedUsageRuleText.trim()) return ''
        try {
            const parsed = JSON.parse(selectedUsageRuleText)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                return t('settings.usage_extraction_rule_must_be_object')
            }
            return ''
        } catch {
            return t('settings.usage_extraction_rule_invalid')
        }
    }, [selectedUsageRuleText, t])

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

    const handleCopy = useCallback(async (value: string) => {
        if (await copyText(value)) {
            toast.success(t('log_detail.copy_success'))
        } else {
            toast.error(t('log_detail.copy_failed'))
        }
    }, [t])

    useEffect(() => {
        if (overrideRulesParse.error || overrideRuleObjects.length === 0) {
            setSelectedOverrideRuleIndex(0)
            setSelectedOverrideRuleText('')
            return
        }

        const nextIndex = Math.min(selectedOverrideRuleIndex, overrideRuleObjects.length - 1)
        if (nextIndex !== selectedOverrideRuleIndex) {
            setSelectedOverrideRuleIndex(nextIndex)
        }
        setSelectedOverrideRuleText(JSON.stringify(overrideRuleObjects[nextIndex], null, 2))
    }, [overrideRulesParse.error, overrideRuleObjects, selectedOverrideRuleIndex])

    useEffect(() => {
        if (usageRulesParse.error || usageRuleObjects.length === 0) {
            setSelectedUsageRuleIndex(0)
            setSelectedUsageRuleText('')
            return
        }

        const nextIndex = Math.min(selectedUsageRuleIndex, usageRuleObjects.length - 1)
        if (nextIndex !== selectedUsageRuleIndex) {
            setSelectedUsageRuleIndex(nextIndex)
        }
        setSelectedUsageRuleText(JSON.stringify(usageRuleObjects[nextIndex], null, 2))
    }, [usageRulesParse.error, usageRuleObjects, selectedUsageRuleIndex])

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
            setBodyPreview(Math.round(configData.logging.body_preview_bytes / 1024))
            setStoreBase64(configData.logging.store_base64)
            setEarlyRequestBodySnapshot(configData.logging.early_request_body_snapshot)
            setRetentionDays(configData.storage.retention_days)
            setMaxStorageGB(parseFloat((configData.storage.max_storage_bytes / (1024 * 1024 * 1024)).toFixed(2)))
            setEnablePathRouting(configData.server.enable_path_routing)
            setPathRoutingPrefix(configData.server.path_routing_prefix || '/_proxy')
            setRequestOverridesEnabled(configData.request_overrides?.enabled ?? false)
            setOverrideMaxBodyKB(Math.round((configData.request_overrides?.max_body_bytes ?? 1048576) / 1024))
            setOverrideBindings(configData.request_overrides?.upstreams ?? {})
            const overrideRules = configData.request_overrides?.rules ?? []
            setOverrideRulesText(overrideRules.length ? JSON.stringify(overrideRules, null, 2) : '')
            setUsageExtractionEnabled(configData.usage_extraction?.enabled ?? false)
            setUsageBindings(configData.usage_extraction?.upstreams ?? {})
            const usageRules = configData.usage_extraction?.rules ?? []
            setUsageRulesText(usageRules.length ? JSON.stringify(usageRules, null, 2) : usageExtractionExample)
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
            await addUpstream(
                newName,
                newTarget,
                newTimeout,
                newResponseHeaderTimeout,
                newResponseBodyFirstByteTimeout,
                newResponseBodyIdleTimeout,
                newOrder,
                normalizedOutboundProxy(newOutboundProxy),
                newLoggingEnabled,
            )
            setNewName('')
            setNewTarget('')
            setNewTimeout(DEFAULT_UPSTREAM_TIMEOUT_SECONDS)
            setNewResponseHeaderTimeout(0)
            setNewResponseBodyFirstByteTimeout(0)
            setNewResponseBodyIdleTimeout(0)
            setNewOrder(prev => prev + 10)
            setNewOutboundProxy('env')
            setNewLoggingEnabled(true)
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

    const buildUsageExtractionPayload = (bindings: Record<string, OverrideBinding>, rules: unknown[]) => ({
        enabled: usageExtractionEnabled,
        upstreams: bindings,
        rules,
    })

    const parseOverrideRules = () => {
        if (selectedOverrideRuleError) {
            throw new Error(selectedOverrideRuleError)
        }
        const trimmedRules = overrideRulesText.trim()
        if (!trimmedRules) return []
        const parsed = JSON.parse(trimmedRules)
        if (!Array.isArray(parsed)) {
            throw new Error(t('settings.request_overrides_rules_must_be_array'))
        }
        return parsed
    }

    const parseUsageRules = () => {
        if (selectedUsageRuleError) {
            throw new Error(selectedUsageRuleError)
        }
        const trimmedRules = usageRulesText.trim()
        if (!trimmedRules) return []
        const parsed = JSON.parse(trimmedRules)
        if (!Array.isArray(parsed)) {
            throw new Error(t('settings.usage_extraction_rules_must_be_array'))
        }
        return parsed
    }

    const setOverrideRulesArray = (rules: OverrideRuleObject[], nextIndex = 0) => {
        setOverrideRulesText(rules.length ? JSON.stringify(rules, null, 2) : '')
        setSelectedOverrideRuleIndex(Math.max(0, Math.min(nextIndex, Math.max(0, rules.length - 1))))
        setSelectedOverrideRuleText(rules[nextIndex] ? JSON.stringify(rules[nextIndex], null, 2) : '')
    }

    const handleSelectOverrideRule = (index: number) => {
        if (!overrideRuleObjects[index]) return
        setSelectedOverrideRuleIndex(index)
        setSelectedOverrideRuleText(JSON.stringify(overrideRuleObjects[index], null, 2))
    }

    const handleOverrideRuleTextChange = (value: string) => {
        setSelectedOverrideRuleText(value)
        try {
            const parsed = JSON.parse(value)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
            const nextRules = [...overrideRuleObjects]
            nextRules[selectedOverrideRuleIndex] = parsed as OverrideRuleObject
            setOverrideRulesText(JSON.stringify(nextRules, null, 2))
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleAddOverrideRule = () => {
        const nextRule = {
            name: `New rule ${overrideRuleObjects.length + 1}`,
            enabled: true,
            match: {
                methods: ['POST'],
            },
            patch: [],
        }
        setOverrideRulesArray([...overrideRuleObjects, nextRule], overrideRuleObjects.length)
    }

    const handleDuplicateOverrideRule = (index: number) => {
        const source = overrideRuleObjects[index]
        if (!source) return
        const copy = JSON.parse(JSON.stringify(source)) as OverrideRuleObject
        copy.name = `${getOverrideRuleName(copy, `rule-${index + 1}`)} copy`
        const nextRules = [...overrideRuleObjects]
        nextRules.splice(index + 1, 0, copy)
        setOverrideRulesArray(nextRules, index + 1)
    }

    const handleDeleteOverrideRule = (index: number) => {
        const ruleName = parsedOverrideRules[index]
        const nextRules = overrideRuleObjects.filter((_, currentIndex) => currentIndex !== index)
        const nextBindings = Object.fromEntries(
            Object.entries(overrideBindings).map(([upstream, binding]) => [
                upstream,
                {
                    ...binding,
                    rule_names: getBindingRuleNames(binding).filter(name => name !== ruleName),
                },
            ]),
        )
        setOverrideBindings(nextBindings)
        setOverrideRulesArray(nextRules, Math.min(index, nextRules.length - 1))
    }

    const handleToggleOverrideRule = (index: number, enabled: boolean) => {
        const nextRules = [...overrideRuleObjects]
        nextRules[index] = { ...nextRules[index], enabled }
        setOverrideRulesArray(nextRules, index)
    }

    const getSelectedRuleHeaders = (): HeaderOp[] => {
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!rule || !Array.isArray(rule.headers)) return []
        return (rule.headers as HeaderOp[]).filter(
            (h): h is HeaderOp => h && typeof h === 'object' && typeof h.op === 'string' && typeof h.name === 'string',
        )
    }

    const updateSelectedRuleHeaders = (headers: HeaderOp[]) => {
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!rule) return
        const nextRule = { ...rule, headers: headers.length > 0 ? headers : undefined }
        const nextRules = [...overrideRuleObjects]
        nextRules[selectedOverrideRuleIndex] = nextRule
        setOverrideRulesArray(nextRules, selectedOverrideRuleIndex)
    }

    const handleAddHeaderOp = () => {
        updateSelectedRuleHeaders([...getSelectedRuleHeaders(), { op: 'set', name: '', value: '' }])
    }

    const handleUpdateHeaderOp = (hIndex: number, field: keyof HeaderOp, value: string) => {
        const headers = [...getSelectedRuleHeaders()]
        headers[hIndex] = { ...headers[hIndex], [field]: value }
        updateSelectedRuleHeaders(headers)
    }

    const handleRemoveHeaderOp = (hIndex: number) => {
        updateSelectedRuleHeaders(getSelectedRuleHeaders().filter((_, i) => i !== hIndex))
    }

    const setUsageRulesArray = (rules: OverrideRuleObject[], nextIndex = 0) => {
        setUsageRulesText(rules.length ? JSON.stringify(rules, null, 2) : '')
        setSelectedUsageRuleIndex(Math.max(0, Math.min(nextIndex, Math.max(0, rules.length - 1))))
        setSelectedUsageRuleText(rules[nextIndex] ? JSON.stringify(rules[nextIndex], null, 2) : '')
    }

    const handleSelectUsageRule = (index: number) => {
        if (!usageRuleObjects[index]) return
        setSelectedUsageRuleIndex(index)
        setSelectedUsageRuleText(JSON.stringify(usageRuleObjects[index], null, 2))
    }

    const handleUsageRuleTextChange = (value: string) => {
        setSelectedUsageRuleText(value)
        try {
            const parsed = JSON.parse(value)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
            const nextRules = [...usageRuleObjects]
            nextRules[selectedUsageRuleIndex] = parsed as OverrideRuleObject
            setUsageRulesText(JSON.stringify(nextRules, null, 2))
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleAddUsageRule = () => {
        const nextRule = {
            name: `New usage rule ${usageRuleObjects.length + 1}`,
            enabled: true,
            match: {
                content_types: ['application/json', 'text/event-stream'],
            },
            paths: {
                input_tokens: [],
                output_tokens: [],
                total_tokens: [],
                raw_usage: [],
            },
        }
        setUsageRulesArray([...usageRuleObjects, nextRule], usageRuleObjects.length)
    }

    const handleDuplicateUsageRule = (index: number) => {
        const source = usageRuleObjects[index]
        if (!source) return
        const copy = JSON.parse(JSON.stringify(source)) as OverrideRuleObject
        copy.name = `${getOverrideRuleName(copy, `usage-rule-${index + 1}`)} copy`
        const nextRules = [...usageRuleObjects]
        nextRules.splice(index + 1, 0, copy)
        setUsageRulesArray(nextRules, index + 1)
    }

    const handleDeleteUsageRule = (index: number) => {
        const ruleName = parsedUsageRules[index]
        const nextRules = usageRuleObjects.filter((_, currentIndex) => currentIndex !== index)
        const nextBindings = Object.fromEntries(
            Object.entries(usageBindings).map(([upstream, binding]) => [
                upstream,
                {
                    ...binding,
                    rule_names: getBindingRuleNames(binding).filter(name => name !== ruleName),
                },
            ]),
        )
        setUsageBindings(nextBindings)
        setUsageRulesArray(nextRules, Math.min(index, nextRules.length - 1))
    }

    const handleToggleUsageRule = (index: number, enabled: boolean) => {
        const nextRules = [...usageRuleObjects]
        nextRules[index] = { ...nextRules[index], enabled }
        setUsageRulesArray(nextRules, index)
    }

    const handleEditUpstream = (upstream: Upstream) => {
        const binding = overrideBindings[upstream.name]
        const usageBinding = usageBindings[upstream.name]
        setUpstreamAdvancedOpen(
            (upstream.response_header_timeout || 0) > 0 ||
            (upstream.response_body_first_byte_timeout || 0) > 0 ||
            (upstream.response_body_idle_timeout || 0) > 0 ||
            upstream.logging_enabled === false ||
            (binding?.enabled ?? false) ||
            (usageBinding?.enabled ?? false) ||
            getBindingRuleNames(binding).length > 0 ||
            getBindingRuleNames(usageBinding).length > 0,
        )
        setEditingUpstream({
            name: upstream.name,
            target: upstream.target,
            timeout: upstream.timeout,
            responseHeaderTimeout: upstream.response_header_timeout || 0,
            responseBodyFirstByteTimeout: upstream.response_body_first_byte_timeout || 0,
            responseBodyIdleTimeout: upstream.response_body_idle_timeout || 0,
            order: upstream.order || 0,
            outboundProxy: upstream.outbound_proxy || 'env',
            loggingEnabled: upstream.logging_enabled !== false,
            overrideEnabled: binding?.enabled ?? false,
            ruleNames: getBindingRuleNames(binding),
            usageEnabled: usageBinding?.enabled ?? false,
            usageRuleNames: getBindingRuleNames(usageBinding),
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

    const toggleEditingUsageRule = (ruleName: string, checked: boolean) => {
        setEditingUpstream(current => {
            if (!current) return current
            const nextRules = checked
                ? [...current.usageRuleNames, ruleName].filter((value, index, arr) => arr.indexOf(value) === index)
                : current.usageRuleNames.filter(name => name !== ruleName)
            return { ...current, usageRuleNames: nextRules }
        })
    }

    const handleSaveUpstreamEdit = async () => {
        if (!editingUpstream) return
        setSaving(true)
        try {
            const overrideRules = parseOverrideRules()
            const usageRules = parseUsageRules()
            const nextBindings = {
                ...overrideBindings,
                [editingUpstream.name]: {
                    enabled: editingUpstream.overrideEnabled,
                    rule_names: editingUpstream.ruleNames,
                },
            }
            const nextUsageBindings = {
                ...usageBindings,
                [editingUpstream.name]: {
                    enabled: editingUpstream.usageEnabled,
                    rule_names: editingUpstream.usageRuleNames,
                },
            }
            await addUpstream(
                editingUpstream.name,
                editingUpstream.target,
                editingUpstream.timeout,
                editingUpstream.responseHeaderTimeout,
                editingUpstream.responseBodyFirstByteTimeout,
                editingUpstream.responseBodyIdleTimeout,
                editingUpstream.order,
                normalizedOutboundProxy(editingUpstream.outboundProxy),
                editingUpstream.loggingEnabled,
            )
            await updateConfig({
                request_overrides: buildOverridesPayload(nextBindings, overrideRules),
                usage_extraction: buildUsageExtractionPayload(nextUsageBindings, usageRules),
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
            const usageRules = parseUsageRules()

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
                    body_preview_bytes: bodyPreview * 1024,
                    store_base64: storeBase64,
                    early_request_body_snapshot: earlyRequestBodySnapshot,
                },
                storage: {
                    retention_days: retentionDays,
                    max_storage_bytes: Math.round(maxStorageGB * 1024 * 1024 * 1024),
                },
                request_overrides: buildOverridesPayload(overrideBindings, overrideRules),
                usage_extraction: buildUsageExtractionPayload(usageBindings, usageRules),
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
                <DialogContent className={cn(
                    "max-h-[calc(100vh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-border/60 bg-card p-0 shadow-2xl transition-[max-width] duration-200",
                    upstreamAdvancedOpen ? "max-w-2xl lg:max-w-5xl" : "max-w-2xl",
                )}>
                    <DialogHeader className="border-b border-border/60 px-6 py-5">
                        <DialogTitle className="text-base font-bold">
                            {editingUpstream ? t('upstream_manager.edit_title', { name: editingUpstream.name }) : t('common.edit')}
                        </DialogTitle>
                        <DialogDescription className="sr-only">
                            {t('upstream_manager.edit_description')}
                        </DialogDescription>
                    </DialogHeader>
                    {editingUpstream && (
                        <>
                            <div className={cn(
                                "min-h-0 grid gap-6 overflow-y-auto px-6 py-5",
                                upstreamAdvancedOpen && "lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:gap-0 lg:overflow-hidden lg:p-0",
                            )}>
                                <div className={cn(
                                    "grid grid-cols-1 gap-5 sm:grid-cols-2",
                                    upstreamAdvancedOpen && "lg:min-h-0 lg:auto-rows-min lg:overflow-y-auto lg:px-6 lg:py-5",
                                )}>
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
                                    <div className="grid grid-cols-1 gap-5 sm:col-span-2 sm:grid-cols-[minmax(120px,0.8fr)_minmax(0,2.2fr)]">
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
                                </div>

                                <AdvancedSettings
                                    open={upstreamAdvancedOpen}
                                    onOpenChange={setUpstreamAdvancedOpen}
                                    sidePanel
                                >
                                    <AdvancedSettingsGroup
                                        title={t('upstream_manager.timeout_strategy')}
                                        description={t('upstream_manager.timeout_strategy_hint')}
                                    >
                                        <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                                            <FieldBlock
                                                label={t('upstream_manager.response_header_timeout')}
                                                hint={t('upstream_manager.response_header_timeout_hint')}
                                            >
                                                <Input
                                                    type="number"
                                                    min="0"
                                                    value={editingUpstream.responseHeaderTimeout}
                                                    onChange={e => setEditingUpstream(current => current ? { ...current, responseHeaderTimeout: Number(e.target.value) } : current)}
                                                    className="h-10 rounded-xl border-border/30 bg-background/50 text-sm"
                                                />
                                            </FieldBlock>
                                            <FieldBlock
                                                label={t('upstream_manager.response_body_first_byte_timeout')}
                                                hint={t('upstream_manager.response_body_first_byte_timeout_hint')}
                                            >
                                                <Input
                                                    type="number"
                                                    min="0"
                                                    value={editingUpstream.responseBodyFirstByteTimeout}
                                                    onChange={e => setEditingUpstream(current => current ? { ...current, responseBodyFirstByteTimeout: Number(e.target.value) } : current)}
                                                    className="h-10 rounded-xl border-border/30 bg-background/50 text-sm"
                                                />
                                            </FieldBlock>
                                            <FieldBlock
                                                label={t('upstream_manager.response_body_idle_timeout')}
                                                hint={t('upstream_manager.response_body_idle_timeout_hint')}
                                            >
                                                <Input
                                                    type="number"
                                                    min="0"
                                                    value={editingUpstream.responseBodyIdleTimeout}
                                                    onChange={e => setEditingUpstream(current => current ? { ...current, responseBodyIdleTimeout: Number(e.target.value) } : current)}
                                                    className="h-10 rounded-xl border-border/30 bg-background/50 text-sm"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </AdvancedSettingsGroup>

                                    <div className="border-t border-border/40 pt-5">
                                        <AdvancedSettingsGroup title={t('upstream_manager.request_logging')}>
                                            <ToggleSetting
                                                label={t('upstream_manager.logging_enabled')}
                                                description={t('upstream_manager.logging_enabled_hint')}
                                                checked={editingUpstream.loggingEnabled}
                                                onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, loggingEnabled: checked } : current)}
                                            />
                                        </AdvancedSettingsGroup>
                                    </div>

                                    <div className="border-t border-border/40 pt-5">
                                        <AdvancedSettingsGroup title={t('upstream_manager.request_overrides')}>
                                            <ToggleSetting
                                                label={t('upstream_manager.override_enabled')}
                                                description={t('upstream_manager.override_enabled_hint')}
                                                checked={editingUpstream.overrideEnabled}
                                                onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, overrideEnabled: checked } : current)}
                                            />
                                            {editingUpstream.overrideEnabled && (
                                                <div className="space-y-2">
                                                    <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                        {t('upstream_manager.bound_rules')}
                                                    </div>
                                                    {parsedOverrideRules.length === 0 ? (
                                                        <div className="rounded-lg border border-dashed border-border/50 bg-background/50 px-3 py-4 text-xs text-muted-foreground">
                                                            {t('upstream_manager.no_rules')}
                                                        </div>
                                                    ) : (
                                                        <div className="grid gap-2 sm:grid-cols-2">
                                                            {parsedOverrideRules.map((ruleName, ruleIndex) => {
                                                                const rule = overrideRuleObjects[ruleIndex]
                                                                const ruleEnabled = getOverrideRuleEnabled(rule)
                                                                const checked = editingUpstream.ruleNames.includes(ruleName)
                                                                return (
                                                                    <label
                                                                        key={`${ruleName}-${ruleIndex}`}
                                                                        className={cn(
                                                                            "flex items-center gap-2 rounded-lg border border-border/40 bg-background/50 px-3 py-2 text-sm",
                                                                            !ruleEnabled && "opacity-70"
                                                                        )}
                                                                    >
                                                                        <input
                                                                            type="checkbox"
                                                                            checked={checked}
                                                                            disabled={!ruleEnabled && !checked}
                                                                            onChange={e => toggleEditingRule(ruleName, e.target.checked)}
                                                                        />
                                                                        <span className="min-w-0 truncate font-mono text-xs">{ruleName}</span>
                                                                        {!ruleEnabled && (
                                                                            <Badge variant="outline" className="ml-auto h-5 shrink-0 rounded-full border-border/40 bg-muted/50 px-1.5 text-[10px] text-muted-foreground">
                                                                                {t('settings.rule_disabled')}
                                                                            </Badge>
                                                                        )}
                                                                    </label>
                                                                )
                                                            })}
                                                        </div>
                                                    )}
                                                </div>
                                            )}
                                        </AdvancedSettingsGroup>
                                    </div>

                                    <div className="border-t border-border/40 pt-5">
                                        <AdvancedSettingsGroup title={t('upstream_manager.usage_stats')}>
                                            <ToggleSetting
                                                label={t('upstream_manager.usage_enabled')}
                                                description={t('upstream_manager.usage_enabled_hint')}
                                                checked={editingUpstream.usageEnabled}
                                                onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, usageEnabled: checked } : current)}
                                            />
                                            {editingUpstream.usageEnabled && (
                                                <div className="space-y-2">
                                                    <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                        {t('upstream_manager.bound_usage_rules')}
                                                    </div>
                                                    {parsedUsageRules.length === 0 ? (
                                                        <div className="rounded-lg border border-dashed border-border/50 bg-background/50 px-3 py-4 text-xs text-muted-foreground">
                                                            {t('upstream_manager.no_usage_rules')}
                                                        </div>
                                                    ) : (
                                                        <div className="grid gap-2 sm:grid-cols-2">
                                                            {parsedUsageRules.map((ruleName, ruleIndex) => {
                                                                const rule = usageRuleObjects[ruleIndex]
                                                                const ruleEnabled = getOverrideRuleEnabled(rule)
                                                                const checked = editingUpstream.usageRuleNames.includes(ruleName)
                                                                return (
                                                                    <label
                                                                        key={`${ruleName}-${ruleIndex}`}
                                                                        className={cn(
                                                                            "flex items-center gap-2 rounded-lg border border-border/40 bg-background/50 px-3 py-2 text-sm",
                                                                            !ruleEnabled && "opacity-70"
                                                                        )}
                                                                    >
                                                                        <input
                                                                            type="checkbox"
                                                                            checked={checked}
                                                                            disabled={!ruleEnabled && !checked}
                                                                            onChange={e => toggleEditingUsageRule(ruleName, e.target.checked)}
                                                                        />
                                                                        <span className="min-w-0 truncate font-mono text-xs">{ruleName}</span>
                                                                        {!ruleEnabled && (
                                                                            <Badge variant="outline" className="ml-auto h-5 shrink-0 rounded-full border-border/40 bg-muted/50 px-1.5 text-[10px] text-muted-foreground">
                                                                                {t('settings.rule_disabled')}
                                                                            </Badge>
                                                                        )}
                                                                    </label>
                                                                )
                                                            })}
                                                        </div>
                                                    )}
                                                </div>
                                            )}
                                        </AdvancedSettingsGroup>
                                    </div>
                                </AdvancedSettings>
                            </div>

                            <div className="flex justify-end gap-2 border-t border-border/50 bg-card px-6 py-4">
                                <Button type="button" variant="ghost" onClick={() => setEditingUpstream(null)}>
                                    {t('common.cancel')}
                                </Button>
                                <Button type="button" onClick={handleSaveUpstreamEdit} disabled={saving}>
                                    <Save className="mr-2 h-4 w-4" />
                                    {t('common.save')}
                                </Button>
                            </div>
                        </>
                    )}
                </DialogContent>
            </Dialog>

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
                            <div className="animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                <div className="mx-auto max-w-7xl space-y-6">
                                    <SettingSection
                                        title={t('settings.access_title')}
                                        description={t('settings.access_description')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-8 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/40 text-sm font-medium shadow-sm transition-colors cursor-default"
                                                />
                                            </FieldBlock>
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                            <div className="flex items-center lg:pt-6">
                                                <ToggleSetting
                                                    label={t('settings.enable_path_routing')}
                                                    description={t('settings.enable_path_routing_hint')}
                                                    checked={enablePathRouting}
                                                    onCheckedChange={setEnablePathRouting}
                                                />
                                            </div>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.tabs.upstreams')}
                                        description={t('settings.upstreams_description')}
                                        action={
                                            <Button
                                                type="button"
                                                variant={showAddForm ? 'secondary' : 'default'}
                                                onClick={() => setShowAddForm(prev => !prev)}
                                                size="sm"
                                                className="h-9 rounded-lg font-medium shadow-sm transition-all whitespace-nowrap"
                                            >
                                                {!showAddForm && <Plus className="mr-1.5 h-4 w-4 shrink-0" />}
                                                {showAddForm ? t('common.cancel') : t('upstream_manager.add_new')}
                                            </Button>
                                        }
                                    >
                                    <div className="w-full">
                                        {showAddForm && (
                                            <div className="mb-8 w-full rounded-2xl bg-background/40 p-6 ring-1 ring-border/20 backdrop-blur-sm">
                                                <form onSubmit={handleAddUpstream} className="grid grid-cols-1 gap-6 md:grid-cols-12 md:items-end">
                                                    <div className="md:col-span-3">
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

                                                    <div className="md:col-span-5">
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

                                                    <div className="md:col-span-2">
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

                                                    <div className="md:col-span-2">
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

                                                    <div className="md:col-span-4">
                                                        <FieldBlock
                                                            label={t('upstream_manager.outbound_proxy')}
                                                            hint={t('upstream_manager.outbound_proxy_hint')}
                                                        >
                                                            <OutboundProxyControl
                                                                value={newOutboundProxy}
                                                                onChange={setNewOutboundProxy}
                                                                t={t}
                                                                className="h-11 bg-background/80 shadow-sm"
                                                            />
                                                        </FieldBlock>
                                                    </div>

                                                    <div className="md:col-span-12">
                                                        <AdvancedSettings>
                                                            <AdvancedSettingsGroup
                                                                title={t('upstream_manager.timeout_strategy')}
                                                                description={t('upstream_manager.timeout_strategy_hint')}
                                                            >
                                                                <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                                                                    <FieldBlock
                                                                        label={t('upstream_manager.response_header_timeout')}
                                                                        htmlFor="response-header-timeout"
                                                                        hint={t('upstream_manager.response_header_timeout_hint')}
                                                                    >
                                                                        <Input
                                                                            id="response-header-timeout"
                                                                            type="number"
                                                                            min="0"
                                                                            value={newResponseHeaderTimeout}
                                                                            onChange={e => setNewResponseHeaderTimeout(Number(e.target.value))}
                                                                            className="h-11 rounded-xl border-border/30 bg-background/80 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                                        />
                                                                    </FieldBlock>

                                                                    <FieldBlock
                                                                        label={t('upstream_manager.response_body_first_byte_timeout')}
                                                                        htmlFor="response-body-first-byte-timeout"
                                                                        hint={t('upstream_manager.response_body_first_byte_timeout_hint')}
                                                                    >
                                                                        <Input
                                                                            id="response-body-first-byte-timeout"
                                                                            type="number"
                                                                            min="0"
                                                                            value={newResponseBodyFirstByteTimeout}
                                                                            onChange={e => setNewResponseBodyFirstByteTimeout(Number(e.target.value))}
                                                                            className="h-11 rounded-xl border-border/30 bg-background/80 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                                        />
                                                                    </FieldBlock>

                                                                    <FieldBlock
                                                                        label={t('upstream_manager.response_body_idle_timeout')}
                                                                        htmlFor="response-body-idle-timeout"
                                                                        hint={t('upstream_manager.response_body_idle_timeout_hint')}
                                                                    >
                                                                        <Input
                                                                            id="response-body-idle-timeout"
                                                                            type="number"
                                                                            min="0"
                                                                            value={newResponseBodyIdleTimeout}
                                                                            onChange={e => setNewResponseBodyIdleTimeout(Number(e.target.value))}
                                                                            className="h-11 rounded-xl border-border/30 bg-background/80 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                                        />
                                                                    </FieldBlock>
                                                                </div>
                                                            </AdvancedSettingsGroup>

                                                            <div className="border-t border-border/40 pt-5">
                                                                <AdvancedSettingsGroup title={t('upstream_manager.request_logging')}>
                                                                    <ToggleSetting
                                                                        label={t('upstream_manager.logging_enabled')}
                                                                        description={t('upstream_manager.logging_enabled_hint')}
                                                                        checked={newLoggingEnabled}
                                                                        onCheckedChange={setNewLoggingEnabled}
                                                                    />
                                                                </AdvancedSettingsGroup>
                                                            </div>
                                                        </AdvancedSettings>
                                                    </div>

                                                    <div className="flex justify-end md:col-span-12">
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
                                                <div className="hidden grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)_180px_110px_96px] gap-5 border-b border-border/40 pb-3 px-2 lg:grid">
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
                                                            className="group grid gap-5 py-5 px-2 transition-colors hover:bg-muted/20 rounded-xl lg:-mx-2 lg:px-4 lg:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)_180px_110px_96px] lg:items-start lg:gap-5"
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
                                                                            {getBindingRuleNames(overrideBindings[upstream.name]).length
                                                                                ? ` · ${getBindingRuleNames(overrideBindings[upstream.name]).length}`
                                                                                : ''}
                                                                        </Badge>
                                                                    )}
                                                                    {upstream.logging_enabled === false && (
                                                                        <Badge variant="outline" className="rounded-full border-slate-500/30 bg-slate-500/10 px-2 py-0.5 text-[11px] font-medium text-slate-600 dark:text-slate-300">
                                                                            {t('upstream_manager.logging_disabled_badge')}
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
                                    </SettingSection>
                                </div>
                            </div>
                        )}

                        {activeTab === 'logging' && (
                            <div className="animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                <div className="mx-auto max-w-7xl space-y-6">
                                    <SettingSection
                                        title={t('settings.section_content_size')}
                                        description={t('settings.section_content_size_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-8 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                            <FieldBlock
                                                label={t('settings.body_preview_bytes')}
                                                hint={t('settings.body_preview_bytes_hint')}
                                                htmlFor="body-preview"
                                                unit="KB"
                                            >
                                                <Input
                                                    id="body-preview"
                                                    type="number"
                                                    min="0"
                                                    value={bodyPreview}
                                                    onChange={e => setBodyPreview(Number(e.target.value))}
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.section_retention')}
                                        description={t('settings.section_retention_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-8 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                            <FieldBlock
                                                label={t('settings.max_storage_bytes')}
                                                hint={t('settings.max_storage_bytes_hint')}
                                                htmlFor="max-storage"
                                                unit="GB"
                                            >
                                                <Input
                                                    id="max-storage"
                                                    type="number"
                                                    min="0"
                                                    step="0.1"
                                                    value={maxStorageGB}
                                                    onChange={e => setMaxStorageGB(Number(e.target.value))}
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.section_logging_behavior')}
                                        description={t('settings.section_logging_behavior_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-8 gap-y-6 sm:grid-cols-2">
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
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.section_sensitive_headers')}
                                        description={t('settings.section_sensitive_headers_desc')}
                                    >
                                        <Textarea
                                            value={sensitiveHeaders}
                                            onChange={e => setSensitiveHeaders(e.target.value)}
                                            rows={4}
                                            className="min-h-[112px] w-full rounded-xl border-border/30 bg-background/50 font-mono text-sm leading-relaxed shadow-sm transition-colors focus-visible:bg-background resize-y"
                                            placeholder="Authorization&#10;x-api-key&#10;api-key"
                                        />
                                    </SettingSection>
                                </div>
                            </div>
                        )}

                        {activeTab === 'system' && (
                            <div className="mx-auto max-w-7xl space-y-8 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
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
                                <div className="mx-auto max-w-7xl space-y-8">
                                    <div className="inline-flex rounded-xl border border-border/50 bg-muted/30 p-1">
                                        <button
                                            type="button"
                                            onClick={() => setActiveRuleTab('request_overrides')}
                                            className={cn(
                                                "rounded-lg px-4 py-2 text-sm font-semibold transition-colors",
                                                activeRuleTab === 'request_overrides'
                                                    ? "bg-background text-foreground shadow-sm"
                                                    : "text-muted-foreground hover:text-foreground"
                                            )}
                                        >
                                            {t('settings.rule_tab_request_overrides')}
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => setActiveRuleTab('usage_extraction')}
                                            className={cn(
                                                "rounded-lg px-4 py-2 text-sm font-semibold transition-colors",
                                                activeRuleTab === 'usage_extraction'
                                                    ? "bg-background text-foreground shadow-sm"
                                                    : "text-muted-foreground hover:text-foreground"
                                            )}
                                        >
                                            {t('settings.rule_tab_usage_extraction')}
                                        </button>
                                    </div>

                                    {activeRuleTab === 'request_overrides' && (
                                        <>
                                    <div className="flex items-start gap-3 rounded-xl border border-yellow-500/30 bg-yellow-500/10 px-4 py-3 text-sm text-yellow-800 dark:text-yellow-300">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                        <div className="space-y-1">
                                            <p className="font-semibold">{t('settings.request_overrides_warning_title')}</p>
                                            <p className="text-xs leading-6 opacity-90">{t('settings.request_overrides_warning')}</p>
                                        </div>
                                    </div>

                                    <SettingSection title={t('settings.request_overrides_config')}>
                                        <div className="grid grid-cols-1 gap-x-8 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
                                            <div className="flex items-center sm:pt-6">
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
                                                    className="h-11 w-full rounded-xl border-border/30 bg-background/50 text-sm shadow-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.request_overrides_rules')}
                                        description={t('settings.request_overrides_rules_hint')}
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
                                                        href="https://github.com/tidwall/gjson/blob/master/SYNTAX.md"
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
                                                    onClick={() => setOverrideRulesArray(JSON.parse(requestOverrideExample) as OverrideRuleObject[], 0)}
                                                    className="h-8 text-xs font-semibold"
                                                >
                                                    <Plus className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('settings.request_overrides_insert_example')}
                                                </Button>
                                            </div>
                                        </div>
                                        <div className="grid gap-4 xl:grid-cols-[minmax(280px,0.32fr)_minmax(0,1fr)]">
                                            <div className="rounded-xl border border-border/40 bg-background/50">
                                                <div className="flex items-center justify-between gap-2 border-b border-border/40 px-3 py-2">
                                                    <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                        {t('settings.request_override_rule_list')}
                                                    </span>
                                                    <Button
                                                        type="button"
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={handleAddOverrideRule}
                                                        className="h-7 px-2 text-xs"
                                                    >
                                                        <Plus className="mr-1 h-3.5 w-3.5" />
                                                        {t('common.add')}
                                                    </Button>
                                                </div>
                                                {overrideRulesParse.error ? (
                                                    <div className="px-3 py-8 text-center text-xs text-red-500">
                                                        {overrideRulesParse.error}
                                                    </div>
                                                ) : overrideRuleObjects.length === 0 ? (
                                                    <div className="px-3 py-8 text-center text-xs text-muted-foreground">
                                                        {t('settings.request_override_no_rules')}
                                                    </div>
                                                ) : (
                                                    <div className="max-h-[500px] divide-y divide-border/30 overflow-y-auto">
                                                        {overrideRuleObjects.map((rule, index) => {
                                                            const ruleName = getOverrideRuleName(rule, `rule-${index + 1}`)
                                                            const selected = selectedOverrideRuleIndex === index
                                                            const status = getRuleRuntimeStatus(rule, overrideBindings, requestOverridesEnabled)
                                                            const statusBadgeClass = cn(
                                                                "h-5 rounded-full px-2 text-[10px] font-semibold",
                                                                status.kind === 'active' && "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
                                                                status.kind === 'blocked' && "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400",
                                                                status.kind === 'unbound' && "border-border/40 bg-muted/50 text-muted-foreground",
                                                            )
                                                            const statusText =
                                                                status.kind === 'active'
                                                                    ? t('settings.rule_status_active')
                                                                    : status.kind === 'unbound'
                                                                        ? t('settings.rule_status_unbound')
                                                                        : status.reason === 'global'
                                                                            ? t('settings.rule_status_blocked_global')
                                                                            : status.reason === 'rule'
                                                                                ? t('settings.rule_status_blocked_rule')
                                                                                : t('settings.rule_status_blocked_bindings')
                                                            const detail =
                                                                status.kind === 'active'
                                                                    ? formatUpstreamList(status.enabledUpstreams, status.disabledUpstreams, t)
                                                                    : status.kind === 'blocked'
                                                                        ? formatUpstreamList(status.enabledUpstreams, status.disabledUpstreams, t)
                                                                        : ''
                                                            return (
                                                                <button
                                                                    key={`${ruleName}-${index}`}
                                                                    type="button"
                                                                    onClick={() => handleSelectOverrideRule(index)}
                                                                    className={cn(
                                                                        "flex w-full items-start gap-3 px-3 py-3 text-left transition-colors hover:bg-muted/40",
                                                                        selected && "bg-primary/10"
                                                                    )}
                                                                >
                                                                    <FileCode className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                                                                    <span className="min-w-0 flex-1 space-y-1">
                                                                        <span className="block truncate text-sm font-semibold text-foreground">
                                                                            {ruleName}
                                                                        </span>
                                                                        <span className="flex flex-wrap items-center gap-1.5">
                                                                            <Badge variant="outline" className={statusBadgeClass}>
                                                                                {statusText}
                                                                            </Badge>
                                                                            {detail && (
                                                                                <span className="min-w-0 truncate text-[10px] text-muted-foreground">
                                                                                    {detail}
                                                                                </span>
                                                                            )}
                                                                        </span>
                                                                    </span>
                                                                </button>
                                                            )
                                                        })}
                                                    </div>
                                                )}
                                            </div>

                                            <div className="rounded-xl border border-border/40 bg-background/50">
                                                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/40 px-3 py-2">
                                                    <div className="min-w-0">
                                                        <div className="truncate text-sm font-semibold text-foreground">
                                                            {overrideRuleObjects[selectedOverrideRuleIndex]
                                                                ? getOverrideRuleName(overrideRuleObjects[selectedOverrideRuleIndex], `rule-${selectedOverrideRuleIndex + 1}`)
                                                                : t('settings.request_override_no_rule_selected')}
                                                        </div>
                                                        <div className="text-[11px] text-muted-foreground">
                                                            {t('settings.request_override_single_rule_hint')}
                                                        </div>
                                                    </div>
                                                    {overrideRuleObjects[selectedOverrideRuleIndex] && (
                                                        <div className="flex shrink-0 items-center gap-1">
                                                            <Button
                                                                type="button"
                                                                variant={getOverrideRuleEnabled(overrideRuleObjects[selectedOverrideRuleIndex]) ? "ghost" : "outline"}
                                                                size="sm"
                                                                onClick={() => handleToggleOverrideRule(selectedOverrideRuleIndex, !getOverrideRuleEnabled(overrideRuleObjects[selectedOverrideRuleIndex]))}
                                                                className={cn(
                                                                    "h-8 px-3 text-xs font-semibold",
                                                                    getOverrideRuleEnabled(overrideRuleObjects[selectedOverrideRuleIndex])
                                                                        ? "text-muted-foreground hover:text-foreground"
                                                                        : "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 hover:bg-emerald-500/20 hover:text-emerald-700 dark:text-emerald-400 dark:hover:text-emerald-300"
                                                                )}
                                                            >
                                                                {getOverrideRuleEnabled(overrideRuleObjects[selectedOverrideRuleIndex])
                                                                    ? t('settings.rule_disable')
                                                                    : t('settings.rule_enable')}
                                                            </Button>
                                                            <Button
                                                                type="button"
                                                                variant="ghost"
                                                                size="icon"
                                                                onClick={() => handleDuplicateOverrideRule(selectedOverrideRuleIndex)}
                                                                className="h-8 w-8"
                                                                aria-label={t('settings.rule_duplicate')}
                                                            >
                                                                <Copy className="h-3.5 w-3.5" />
                                                            </Button>
                                                            <Button
                                                                type="button"
                                                                variant="ghost"
                                                                size="icon"
                                                                onClick={() => handleDeleteOverrideRule(selectedOverrideRuleIndex)}
                                                                className="h-8 w-8 text-muted-foreground hover:text-red-500"
                                                                aria-label={t('common.delete')}
                                                            >
                                                                <Trash2 className="h-3.5 w-3.5" />
                                                            </Button>
                                                        </div>
                                                    )}
                                                </div>
                                                {overrideRuleObjects[selectedOverrideRuleIndex] ? (
                                                    <div className="space-y-4 p-3">
                                                        <div className="space-y-2">
                                                            <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                                                                {t('settings.request_override_body_patch')}
                                                            </Label>
                                                            <Textarea
                                                                value={selectedOverrideRuleText}
                                                                onChange={e => handleOverrideRuleTextChange(e.target.value)}
                                                                rows={18}
                                                                spellCheck={false}
                                                                className="min-h-[420px] w-full resize-y rounded-lg border-border/30 bg-muted/20 font-mono text-xs leading-relaxed shadow-sm transition-colors focus-visible:bg-background"
                                                            />
                                                            {selectedOverrideRuleError && (
                                                                <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-400">
                                                                    {selectedOverrideRuleError}
                                                                </div>
                                                            )}
                                                        </div>

                                                        <div className="space-y-2">
                                                            <div className="flex items-center justify-between">
                                                                <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                                                                    {t('settings.request_override_headers')}
                                                                </Label>
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="sm"
                                                                    onClick={handleAddHeaderOp}
                                                                    className="h-7 px-2 text-xs"
                                                                >
                                                                    <Plus className="mr-1 h-3.5 w-3.5" />
                                                                    {t('common.add')}
                                                                </Button>
                                                            </div>
                                                            <p className="text-[11px] leading-5 text-muted-foreground">
                                                                {t('settings.request_override_headers_hint')}
                                                            </p>
                                                            {getSelectedRuleHeaders().length > 0 ? (
                                                                <div className="space-y-2">
                                                                    {getSelectedRuleHeaders().map((header, hIdx) => (
                                                                        <div key={hIdx} className="flex items-center gap-2">
                                                                            <Select
                                                                                value={header.op}
                                                                                onValueChange={v => handleUpdateHeaderOp(hIdx, 'op', v)}
                                                                            >
                                                                                <SelectTrigger className="h-9 w-[100px] shrink-0 rounded-lg border-border/30 bg-background/50 text-xs">
                                                                                    <SelectValue />
                                                                                </SelectTrigger>
                                                                                <SelectContent>
                                                                                    <SelectItem value="set">{t('settings.header_op_set')}</SelectItem>
                                                                                    <SelectItem value="remove">{t('settings.header_op_remove')}</SelectItem>
                                                                                </SelectContent>
                                                                            </Select>
                                                                            <Input
                                                                                value={header.name}
                                                                                onChange={e => handleUpdateHeaderOp(hIdx, 'name', e.target.value)}
                                                                                placeholder={t('settings.header_name_placeholder')}
                                                                                className="h-9 min-w-0 flex-1 rounded-lg border-border/30 bg-background/50 text-xs"
                                                                            />
                                                                            <Input
                                                                                value={header.value ?? ''}
                                                                                onChange={e => handleUpdateHeaderOp(hIdx, 'value', e.target.value)}
                                                                                placeholder={header.op === 'remove' ? '—' : t('settings.header_value_placeholder')}
                                                                                disabled={header.op === 'remove'}
                                                                                className="h-9 min-w-0 flex-[2] rounded-lg border-border/30 bg-background/50 text-xs disabled:opacity-40"
                                                                            />
                                                                            <Button
                                                                                type="button"
                                                                                variant="ghost"
                                                                                size="icon"
                                                                                onClick={() => handleRemoveHeaderOp(hIdx)}
                                                                                className="h-8 w-8 shrink-0 text-muted-foreground hover:text-red-500"
                                                                            >
                                                                                <Trash2 className="h-3.5 w-3.5" />
                                                                            </Button>
                                                                        </div>
                                                                    ))}
                                                                </div>
                                                            ) : (
                                                                <div className="rounded-lg border border-dashed border-border/40 px-3 py-4 text-center text-xs text-muted-foreground">
                                                                    {t('settings.request_override_no_headers')}
                                                                </div>
                                                            )}
                                                        </div>
                                                    </div>
                                                ) : (
                                                    <div className="px-4 py-16 text-center text-xs text-muted-foreground">
                                                        {t('settings.request_override_no_rule_selected')}
                                                    </div>
                                                )}
                                            </div>
                                        </div>

                                        <div className="mt-4 rounded-xl border border-border/40 bg-muted/20">
                                            <button
                                                type="button"
                                                onClick={() => setAdvancedRulesOpen(open => !open)}
                                                className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                                            >
                                                <span>{t('settings.request_override_advanced_json')}</span>
                                                <span>{advancedRulesOpen ? t('common.hide', '隐藏') : t('common.show', '显示')}</span>
                                            </button>
                                            {advancedRulesOpen && (
                                                <div className="border-t border-border/40 p-3">
                                                    <Textarea
                                                        value={overrideRulesText}
                                                        onChange={e => setOverrideRulesText(e.target.value)}
                                                        rows={12}
                                                        spellCheck={false}
                                                        className="min-h-[260px] w-full rounded-lg border-border/30 bg-background/50 font-mono text-xs leading-relaxed shadow-sm transition-colors focus-visible:bg-background resize-y"
                                                        placeholder={requestOverrideExample}
                                                    />
                                                </div>
                                            )}
                                        </div>
                                    </SettingSection>

                                        </>
                                    )}

                                    {activeRuleTab === 'usage_extraction' && (
                                        <>
                                    <SettingSection title={t('settings.usage_extraction_config')}>
                                        <ToggleSetting
                                            label={t('settings.usage_extraction_enable')}
                                            description={t('settings.usage_extraction_enable_hint')}
                                            checked={usageExtractionEnabled}
                                            onCheckedChange={setUsageExtractionEnabled}
                                        />
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.usage_extraction_rules')}
                                        description={t('settings.usage_extraction_rules_hint')}
                                    >
                                            <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/40 bg-background/50 px-3 py-2">
                                                <p className="text-xs leading-5 text-muted-foreground">
                                                    {t('settings.usage_extraction_scope_hint')}
                                                </p>
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    onClick={() => setUsageRulesArray(JSON.parse(usageExtractionExample) as OverrideRuleObject[], 0)}
                                                    className="h-8 text-xs font-semibold"
                                                >
                                                    <Plus className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('settings.usage_extraction_insert_example')}
                                                </Button>
                                            </div>

                                            <div className="grid gap-4 xl:grid-cols-[minmax(280px,0.32fr)_minmax(0,1fr)]">
                                                <div className="rounded-xl border border-border/40 bg-background/50">
                                                    <div className="flex items-center justify-between gap-2 border-b border-border/40 px-3 py-2">
                                                        <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                                            {t('settings.usage_extraction_rule_list')}
                                                        </span>
                                                        <Button
                                                            type="button"
                                                            variant="ghost"
                                                            size="sm"
                                                            onClick={handleAddUsageRule}
                                                            className="h-7 px-2 text-xs"
                                                        >
                                                            <Plus className="mr-1 h-3.5 w-3.5" />
                                                            {t('common.add')}
                                                        </Button>
                                                    </div>
                                                    {usageRulesParse.error ? (
                                                        <div className="px-3 py-8 text-center text-xs text-red-500">
                                                            {usageRulesParse.error}
                                                        </div>
                                                    ) : usageRuleObjects.length === 0 ? (
                                                        <div className="px-3 py-8 text-center text-xs text-muted-foreground">
                                                            {t('settings.usage_extraction_no_rules')}
                                                        </div>
                                                    ) : (
                                                        <div className="max-h-[420px] divide-y divide-border/30 overflow-y-auto">
                                                            {usageRuleObjects.map((rule, index) => {
                                                                const ruleName = getOverrideRuleName(rule, `usage-rule-${index + 1}`)
                                                                const boundCount = Object.values(usageBindings).filter(binding => getBindingRuleNames(binding).includes(ruleName)).length
                                                                const selected = selectedUsageRuleIndex === index
                                                                return (
                                                                    <button
                                                                        key={`${ruleName}-${index}`}
                                                                        type="button"
                                                                        onClick={() => handleSelectUsageRule(index)}
                                                                        className={cn(
                                                                            "flex w-full items-start gap-3 px-3 py-3 text-left transition-colors hover:bg-muted/40",
                                                                            selected && "bg-primary/10"
                                                                        )}
                                                                    >
                                                                        <FileCode className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                                                                        <span className="min-w-0 flex-1 space-y-1">
                                                                            <span className="block truncate text-sm font-semibold text-foreground">
                                                                                {ruleName}
                                                                            </span>
                                                                            <span className="flex flex-wrap items-center gap-1.5">
                                                                                <Badge
                                                                                    variant="outline"
                                                                                    className={cn(
                                                                                        "h-5 rounded-full px-1.5 text-[10px]",
                                                                                        getOverrideRuleEnabled(rule)
                                                                                            ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                                                                                            : "border-border/40 bg-muted/50 text-muted-foreground"
                                                                                    )}
                                                                                >
                                                                                    {getOverrideRuleEnabled(rule) ? t('settings.rule_enabled') : t('settings.rule_disabled')}
                                                                                </Badge>
                                                                                {boundCount > 0 && (
                                                                                    <Badge variant="outline" className="h-5 rounded-full border-border/40 bg-muted/50 px-1.5 text-[10px] text-muted-foreground">
                                                                                        {t('settings.rule_bound_count', { count: boundCount })}
                                                                                    </Badge>
                                                                                )}
                                                                            </span>
                                                                        </span>
                                                                    </button>
                                                                )
                                                            })}
                                                        </div>
                                                    )}
                                                </div>

                                                <div className="rounded-xl border border-border/40 bg-background/50">
                                                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/40 px-3 py-2">
                                                        <div className="min-w-0">
                                                            <div className="truncate text-sm font-semibold text-foreground">
                                                                {usageRuleObjects[selectedUsageRuleIndex]
                                                                    ? getOverrideRuleName(usageRuleObjects[selectedUsageRuleIndex], `usage-rule-${selectedUsageRuleIndex + 1}`)
                                                                    : t('settings.usage_extraction_no_rule_selected')}
                                                            </div>
                                                            <div className="text-[11px] text-muted-foreground">
                                                                {t('settings.usage_extraction_single_rule_hint')}
                                                            </div>
                                                        </div>
                                                        {usageRuleObjects[selectedUsageRuleIndex] && (
                                                            <div className="flex shrink-0 items-center gap-1">
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="sm"
                                                                    onClick={() => handleToggleUsageRule(selectedUsageRuleIndex, !getOverrideRuleEnabled(usageRuleObjects[selectedUsageRuleIndex]))}
                                                                    className="h-8 px-2 text-xs"
                                                                >
                                                                    {getOverrideRuleEnabled(usageRuleObjects[selectedUsageRuleIndex])
                                                                        ? t('settings.rule_disable')
                                                                        : t('settings.rule_enable')}
                                                                </Button>
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    onClick={() => handleDuplicateUsageRule(selectedUsageRuleIndex)}
                                                                    className="h-8 w-8"
                                                                    aria-label={t('settings.rule_duplicate')}
                                                                >
                                                                    <Copy className="h-3.5 w-3.5" />
                                                                </Button>
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    onClick={() => handleDeleteUsageRule(selectedUsageRuleIndex)}
                                                                    className="h-8 w-8 text-muted-foreground hover:text-red-500"
                                                                    aria-label={t('common.delete')}
                                                                >
                                                                    <Trash2 className="h-3.5 w-3.5" />
                                                                </Button>
                                                            </div>
                                                        )}
                                                    </div>
                                                    {usageRuleObjects[selectedUsageRuleIndex] ? (
                                                        <div className="space-y-2 p-3">
                                                            <Textarea
                                                                value={selectedUsageRuleText}
                                                                onChange={e => handleUsageRuleTextChange(e.target.value)}
                                                                rows={14}
                                                                spellCheck={false}
                                                                className="min-h-[320px] w-full resize-y rounded-lg border-border/30 bg-muted/20 font-mono text-xs leading-relaxed shadow-sm transition-colors focus-visible:bg-background"
                                                            />
                                                            {selectedUsageRuleError && (
                                                                <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-400">
                                                                    {selectedUsageRuleError}
                                                                </div>
                                                            )}
                                                        </div>
                                                    ) : (
                                                        <div className="px-4 py-16 text-center text-xs text-muted-foreground">
                                                            {t('settings.usage_extraction_no_rule_selected')}
                                                        </div>
                                                    )}
                                                </div>
                                            </div>

                                            <div className="mt-4 rounded-xl border border-border/40 bg-background/50">
                                                <button
                                                    type="button"
                                                    onClick={() => setUsageAdvancedRulesOpen(open => !open)}
                                                    className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                                                >
                                                    <span>{t('settings.usage_extraction_advanced_json')}</span>
                                                    <span>{usageAdvancedRulesOpen ? t('common.hide', '隐藏') : t('common.show', '显示')}</span>
                                                </button>
                                                {usageAdvancedRulesOpen && (
                                                    <div className="border-t border-border/40 p-3">
                                                        <Textarea
                                                            value={usageRulesText}
                                                            onChange={e => setUsageRulesText(e.target.value)}
                                                            rows={12}
                                                            spellCheck={false}
                                                            className="min-h-[260px] w-full rounded-lg border-border/30 bg-background/50 font-mono text-xs leading-relaxed shadow-sm transition-colors focus-visible:bg-background resize-y"
                                                            placeholder={usageExtractionExample}
                                                        />
                                                    </div>
                                                )}
                                            </div>
                                    </SettingSection>

                                        </>
                                    )}

                                </div>
                            </div>
                        )}
                    </div>
                    {(activeTab === 'routing' || activeTab === 'logging' || activeTab === 'overrides') && (
                        <div className="sticky bottom-0 z-30 -mx-4 border-t border-border/40 bg-background/85 px-4 py-3 backdrop-blur-md sm:-mx-10 sm:px-10">
                            <div className="mx-auto flex max-w-7xl items-center justify-end gap-4">
                                <Button
                                    type="button"
                                    onClick={handleSaveAll}
                                    disabled={saving}
                                    variant="default"
                                    size="lg"
                                    className="h-11 min-w-[160px] rounded-xl font-medium shadow-sm transition-all whitespace-nowrap"
                                >
                                    <Save className="mr-2 h-4 w-4 shrink-0" />
                                    {t('common.save')}
                                </Button>
                            </div>
                        </div>
                    )}
                </div>
        </div>
    )
}
