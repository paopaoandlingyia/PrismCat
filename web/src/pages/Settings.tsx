import {
    useEffect,
    useState,
    useCallback,
    useMemo,
    useId,
    useRef,
    type FormEvent,
    type ReactNode,
} from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { isSettingsTab, type SettingsTab } from '@/lib/routes'
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
    RefreshCw,
    Download,
    Database,
    Archive,
    FileCode,
    ChevronDown,
    Route,
    Eye,
    EyeOff,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { UpstreamLoggingPathFilter } from '@/components/UpstreamLoggingPathFilter'
import { LoggingRules, type LoggingRulesTab } from '@/pages/LoggingRules'
import { Switch } from '@/components/ui/switch'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import { DEFAULT_UPSTREAM_TIMEOUT_SECONDS, fetchUpstreams, addUpstream, removeUpstream, activateUpstreamTarget, fetchConfig, updateConfig, fetchSystemMetrics, fetchUpdateInfo, fetchStorageUsage, fetchModelPathTemplates, fetchArchives, fetchArchiveDeletionPreview, testArchiveConnection, runArchiveNow } from '@/lib/api'
import { notifyArchiveFeatureChanged } from '@/lib/archiveFeature'
import type { Upstream, UpstreamTarget, AppConfig, SystemMetrics, UpdateInfo, StorageUsage, LoggingPathFilter, ModelPathTemplate, ArchiveStatus } from '@/lib/api'
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
        name: 'Example: default max_tokens',
        enabled: false,
        match: {
            methods: ['POST'],
        },
        patch: [
            { op: 'default', path: 'max_tokens', value: 4096 },
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

function validateArchiveKeyPrefix(value: string): string {
    const prefix = value.trim()
    if (!prefix) return 'required'
    if (prefix.startsWith('/') || prefix.endsWith('/') || prefix.includes('\\') || prefix.includes('//')) return 'separator'
    if ([...prefix].some(character => {
        const code = character.charCodeAt(0)
        return code < 0x20 || code === 0x7f
    })) return 'control'
    const placeholders = [...prefix.matchAll(/\$\{([^}]*)\}/g)]
    if (placeholders.some(match => !['yyyy', 'MM', 'dd'].includes(match[1]))) return 'placeholder'
    const resolved = prefix.replaceAll('${yyyy}', '2026').replaceAll('${MM}', '08').replaceAll('${dd}', '17')
    if (resolved.includes('${') || resolved.includes('}')) return 'placeholder'
    if (resolved.split('/').some(part => !part || part === '.' || part === '..')) return 'segment'
    if (resolved.length + 128 > 1024) return 'length'
    return ''
}

function archiveDateParts(timezone: string) {
    try {
        const parts = new Intl.DateTimeFormat('en-US', {
            timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit',
        }).formatToParts(new Date())
        const get = (type: string) => parts.find(part => part.type === type)?.value ?? ''
        return { yyyy: get('year'), MM: get('month'), dd: get('day') }
    } catch {
        return { yyyy: '2026', MM: '08', dd: '17' }
    }
}

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
    loggingPathFilter: LoggingPathFilter
    overrideEnabled: boolean
    ruleNames: string[]
    usageEnabled: boolean
    usageRuleNames: string[]
    usesTargetPresets: boolean
    activeTarget: string
    selectedTarget: string
    targets: Record<string, UpstreamTarget>
}

type OutboundProxyMode = 'env' | 'direct' | 'custom'
type RuleTab = 'request_overrides' | 'usage_extraction'
type OverrideRuleObject = Record<string, unknown>
type HeaderOp = { op: string; name: string; value?: string }

const customProxyPlaceholder = 'http://127.0.0.1:7890'

function editingBufferAsTarget(upstream: EditingUpstream): UpstreamTarget {
    return {
        url: upstream.target,
        timeout: upstream.timeout,
        response_header_timeout: upstream.responseHeaderTimeout,
        response_body_first_byte_timeout: upstream.responseBodyFirstByteTimeout,
        response_body_idle_timeout: upstream.responseBodyIdleTimeout,
        outbound_proxy: upstream.outboundProxy,
        request_overrides: {
            enabled: upstream.overrideEnabled,
            rule_names: upstream.ruleNames,
        },
        usage_extraction: {
            enabled: upstream.usageEnabled,
            rule_names: upstream.usageRuleNames,
        },
    }
}

function commitSelectedTarget(upstream: EditingUpstream): EditingUpstream {
    if (!upstream.usesTargetPresets || !upstream.selectedTarget) return upstream
    return {
        ...upstream,
        targets: {
            ...upstream.targets,
            [upstream.selectedTarget]: editingBufferAsTarget(upstream),
        },
    }
}

function loadTargetBuffer(upstream: EditingUpstream, targetName: string): EditingUpstream {
    const target = upstream.targets[targetName]
    if (!target) return upstream
    return {
        ...upstream,
        selectedTarget: targetName,
        target: target.url || '',
        timeout: target.timeout || DEFAULT_UPSTREAM_TIMEOUT_SECONDS,
        responseHeaderTimeout: target.response_header_timeout || 0,
        responseBodyFirstByteTimeout: target.response_body_first_byte_timeout || 0,
        responseBodyIdleTimeout: target.response_body_idle_timeout || 0,
        outboundProxy: target.outbound_proxy || 'env',
        overrideEnabled: target.request_overrides?.enabled ?? false,
        ruleNames: target.request_overrides?.rule_names || [],
        usageEnabled: target.usage_extraction?.enabled ?? false,
        usageRuleNames: target.usage_extraction?.rule_names || [],
    }
}

function activeTargetConfig(upstream: Upstream): UpstreamTarget | undefined {
    if (!upstream.active_target) return undefined
    return upstream.targets?.[upstream.active_target]
}

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

// 高级设置面板「有改动才默认展开」的判据。记录日志不在其中 —— 它是上游级的,
// 已经移到面板外面了。
function hasAdvancedValues(source?: {
    response_header_timeout?: number
    response_body_first_byte_timeout?: number
    response_body_idle_timeout?: number
    request_overrides?: OverrideBinding
    usage_extraction?: OverrideBinding
}) {
    if (!source) return false
    return (source.response_header_timeout || 0) > 0
        || (source.response_body_first_byte_timeout || 0) > 0
        || (source.response_body_idle_timeout || 0) > 0
        || (source.request_overrides?.enabled ?? false)
        || (source.usage_extraction?.enabled ?? false)
        || getBindingRuleNames(source.request_overrides).length > 0
        || getBindingRuleNames(source.usage_extraction).length > 0
}

type RuleRuntimeStatus =
    | { kind: 'active'; enabledUpstreams: string[]; disabledUpstreams: string[]; inactiveTargets: string[] }
    | { kind: 'blocked'; reason: 'global' | 'rule' | 'bindings'; enabledUpstreams: string[]; disabledUpstreams: string[]; inactiveTargets: string[] }
    | { kind: 'inactive'; inactiveTargets: string[] }
    | { kind: 'unbound' }

type BindingKind = 'request_overrides' | 'usage_extraction'

function collectRuleBindingLocations(
    upstreams: Upstream[],
    legacyBindings: Record<string, OverrideBinding>,
    kind: BindingKind,
): { active: string[]; disabled: string[]; inactive: string[] } {
    const active: string[] = []
    const disabled: string[] = []
    const inactive: string[] = []
    for (const upstream of upstreams) {
        const targets = upstream.targets || {}
        if (Object.keys(targets).length === 0) {
            const binding = legacyBindings[upstream.name]
            if (!binding) continue
            const label = upstream.name
            if (binding.enabled) active.push(label)
            else disabled.push(label)
            continue
        }

        for (const [targetName, target] of Object.entries(targets)) {
            const binding = target[kind]
            if (!binding) continue
            const label = `${upstream.name}/${targetName}`
            if (targetName === upstream.active_target) {
                if (binding.enabled) active.push(label)
                else disabled.push(label)
            } else {
                inactive.push(label)
            }
        }
    }
    return { active, disabled, inactive }
}

function getRuleRuntimeStatus(
    rule: OverrideRuleObject,
    upstreams: Upstream[],
    bindings: Record<string, OverrideBinding>,
    globalEnabled: boolean,
): RuleRuntimeStatus {
    const ruleName = getOverrideRuleName(rule, '')
    if (!ruleName) return { kind: 'unbound' }

    const locations = collectRuleBindingLocations(upstreams, bindings, 'request_overrides')
    const enabledUpstreams = locations.active.filter(name => {
        const [upstreamName, targetName] = name.split('/')
        const binding = targetName
            ? upstreams.find(upstream => upstream.name === upstreamName)?.targets?.[targetName]?.request_overrides
            : bindings[upstreamName]
        return Boolean(binding && getBindingRuleNames(binding).includes(ruleName))
    })
    const disabledUpstreams = locations.disabled.filter(name => {
        const [upstreamName, targetName] = name.split('/')
        const binding = targetName
            ? upstreams.find(upstream => upstream.name === upstreamName)?.targets?.[targetName]?.request_overrides
            : bindings[upstreamName]
        return Boolean(binding && getBindingRuleNames(binding).includes(ruleName))
    })
    const inactiveTargets = locations.inactive.filter(name => {
        const [upstreamName, targetName] = name.split('/')
        const binding = upstreams.find(upstream => upstream.name === upstreamName)?.targets?.[targetName]?.request_overrides
        return Boolean(binding && getBindingRuleNames(binding).includes(ruleName))
    })

    if (enabledUpstreams.length + disabledUpstreams.length + inactiveTargets.length === 0) {
        return { kind: 'unbound' }
    }
    if (enabledUpstreams.length + disabledUpstreams.length === 0) {
        return { kind: 'inactive', inactiveTargets }
    }
    if (!globalEnabled) {
        return { kind: 'blocked', reason: 'global', enabledUpstreams, disabledUpstreams, inactiveTargets }
    }
    if (!getOverrideRuleEnabled(rule)) {
        return { kind: 'blocked', reason: 'rule', enabledUpstreams, disabledUpstreams, inactiveTargets }
    }
    if (enabledUpstreams.length === 0) {
        return { kind: 'blocked', reason: 'bindings', enabledUpstreams, disabledUpstreams, inactiveTargets }
    }
    return { kind: 'active', enabledUpstreams, disabledUpstreams, inactiveTargets }
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

function formatInactiveTargets(
    targets: string[],
    t: (key: string, options?: Record<string, unknown>) => string,
): string {
    if (targets.length === 0) return ''
    const summarize = targets.length <= 2 ? targets.join(', ') : `${targets.slice(0, 2).join(', ')} +${targets.length - 2}`
    return t('settings.rule_status_inactive_targets', { list: summarize })
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
                "relative flex h-9 overflow-hidden rounded-md border border-input bg-background transition-shadow focus-within:ring-2 focus-within:ring-ring/50",
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
                        "h-full rounded-none border-0 bg-transparent text-sm shadow-none",
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
                    className="inline-flex h-4 w-4 items-center justify-center rounded-md text-muted-foreground/85 transition-colors hover:text-foreground"
                    aria-label="More info"
                >
                    <CircleHelp className="h-3.5 w-3.5" />
                </button>
            </TooltipTrigger>
            <TooltipContent sideOffset={6} className="max-w-xs px-3 py-2 text-xs leading-6">
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
                    <span className="rounded-md bg-muted/60 px-2 py-0.5 text-xs font-medium text-muted-foreground">
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
}: {
    children: ReactNode
    open?: boolean
    onOpenChange?: (open: boolean) => void
    defaultOpen?: boolean
}) {
    const { t } = useTranslation()
    const [internalOpen, setInternalOpen] = useState(defaultOpen)
    const open = controlledOpen ?? internalOpen
    const setOpen = (nextOpen: boolean) => {
        if (controlledOpen === undefined) setInternalOpen(nextOpen)
        onOpenChange?.(nextOpen)
    }
    return (
        <section className="rounded-md border border-input bg-muted/20">
            <button
                type="button"
                aria-expanded={open}
                onClick={() => setOpen(!open)}
                className="flex w-full cursor-pointer items-center justify-between gap-4 px-4 py-3 text-left"
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
                <div className="space-y-6 border-t border-input px-4 py-5">
                    {children}
                </div>
            )}
        </section>
    )
}

function uniqueRuleName(baseName: string, rules: OverrideRuleObject[]) {
    const names = new Set(rules.map(rule => getOverrideRuleName(rule, '')))
    if (!names.has(baseName)) return baseName
    let suffix = 2
    while (names.has(`${baseName} ${suffix}`)) suffix += 1
    return `${baseName} ${suffix}`
}

function isSensitiveHeaderName(name: string, configuredHeaders: string) {
    const normalized = name.trim().toLowerCase()
    if (!normalized) return false
    const defaults = ['authorization', 'proxy-authorization', 'x-api-key', 'api-key']
    const configured = configuredHeaders
        .split('\n')
        .map(value => value.trim().toLowerCase())
        .filter(Boolean)
    return defaults.includes(normalized) || configured.includes(normalized)
}

function redactSensitiveRuleHeaders(rule: OverrideRuleObject, configuredHeaders: string) {
    const headers = Array.isArray(rule.headers) ? rule.headers : []
    return {
        ...rule,
        ...(headers.length > 0
            ? {
                headers: headers.map(header => {
                    if (!header || typeof header !== 'object' || Array.isArray(header)) return header
                    const item = header as Record<string, unknown>
                    const name = typeof item.name === 'string' ? item.name : ''
                    if (!isSensitiveHeaderName(name, configuredHeaders) || typeof item.value !== 'string') return item
                    return { ...item, value: '••••••••' }
                }),
            }
            : {}),
    }
}

function AdvancedSettingsGroup({
    title,
    description,
    children,
    card = false,
    divider = false,
}: {
    title: string
    description?: string
    children: ReactNode
    /** 独立成卡片。嵌在别的面板里时别用 —— 会叠出第三层边框 */
    card?: boolean
    /** 用一条分隔线起新段,替代再套一层边框 */
    divider?: boolean
}) {
    return (
        <div className={cn(
            "space-y-4",
            card && "rounded-md border border-input bg-background p-4",
            divider && "border-t border-input pt-6",
        )}>
            <div>
                <div className="text-xs font-semibold text-foreground/65">{title}</div>
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

function HeaderValueInput({
    value,
    onChange,
    placeholder,
    disabled,
    sensitive,
    showLabel,
    hideLabel,
}: {
    value: string
    onChange: (value: string) => void
    placeholder: string
    disabled: boolean
    sensitive: boolean
    showLabel: string
    hideLabel: string
}) {
    const [revealed, setRevealed] = useState(false)
    const masked = sensitive && !revealed

    return (
        <div className="relative min-w-0 flex-[2]">
            <Input
                type={masked ? 'password' : 'text'}
                value={value}
                onChange={event => onChange(event.target.value)}
                placeholder={placeholder}
                disabled={disabled}
                autoComplete="off"
                className={cn(
                    "h-9 w-full rounded-lg border-input bg-background text-xs disabled:opacity-40",
                    sensitive && !disabled && "pr-9",
                )}
            />
            {sensitive && !disabled && (
                <button
                    type="button"
                    onClick={() => setRevealed(current => !current)}
                    className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
                    aria-label={revealed ? hideLabel : showLabel}
                >
                    {revealed ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
            )}
        </div>
    )
}

function MetricCard({
    label,
    value,
    detail,
}: {
    label: string
    value: string
    detail?: string
}) {
    return (
        <div className="rounded-lg border border-border bg-card px-4 py-3">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="mt-1 text-xl tabular-nums text-foreground">{value}</div>
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
        <section className="overflow-hidden rounded-lg border border-input bg-card">
            <div className="flex items-start justify-between gap-4 border-b border-input px-6 py-4">
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
    const navigate = useNavigate()
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [config, setConfig] = useState<AppConfig | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showAddForm, setShowAddForm] = useState(false)
    // 分区由 URL 决定,导航入口只有侧边栏一处。/settings 不带分区时落到 routing,
    // 不做重定向,免得在保存栏有未提交改动时因为地址跳转丢状态。
    const { tab: tabParam, subtab: subtabParam } = useParams()
    const [searchParams] = useSearchParams()
    const deepLinkHandled = useRef(false)
    const activeTab: SettingsTab = isSettingsTab(tabParam) ? tabParam : 'routing'
    const loggingSection: 'general' | LoggingRulesTab = subtabParam === 'models' || subtabParam === 'ignored'
        ? subtabParam
        : 'general'
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
    const [newLoggingPathFilter, setNewLoggingPathFilter] = useState<LoggingPathFilter>({ mode: 'all', rules: [] })
    const [modelPathTemplates, setModelPathTemplates] = useState<ModelPathTemplate[]>([])
    const [editingUpstream, setEditingUpstream] = useState<EditingUpstream | null>(null)
    const [upstreamAdvancedOpen, setUpstreamAdvancedOpen] = useState(false)
    const [newTargetPresetName, setNewTargetPresetName] = useState('')
    const [switchingTarget, setSwitchingTarget] = useState('')

    const [enablePathRouting, setEnablePathRouting] = useState(false)
    const [pathRoutingPrefix, setPathRoutingPrefix] = useState('/_proxy')

    const [maxRequestBody, setMaxRequestBody] = useState(5120)
    const [maxResponseBody, setMaxResponseBody] = useState(32768)
    const [sensitiveHeaders, setSensitiveHeaders] = useState('')
    const [storeBase64, setStoreBase64] = useState(true)
    const [earlyRequestBodySnapshot, setEarlyRequestBodySnapshot] = useState(false)
    const [bodyCompressionAlgorithm, setBodyCompressionAlgorithm] = useState<'zstd' | 'none'>('zstd')
    const [bodyCompressionLevel, setBodyCompressionLevel] = useState(3)

    const [retentionDays, setRetentionDays] = useState(30)
    const [maxStorageGB, setMaxStorageGB] = useState(0)
    const [archiveEnabled, setArchiveEnabled] = useState(false)
    const [archiveEndpoint, setArchiveEndpoint] = useState('')
    const [archiveRegion, setArchiveRegion] = useState('')
    const [archiveBucket, setArchiveBucket] = useState('')
    const [archiveAccessKeyID, setArchiveAccessKeyID] = useState('')
    const [archiveSecretAccessKey, setArchiveSecretAccessKey] = useState('')
    const [archiveSecretConfigured, setArchiveSecretConfigured] = useState(false)
    const [archiveForcePathStyle, setArchiveForcePathStyle] = useState(false)
    const [archiveKeyPrefix, setArchiveKeyPrefix] = useState('backups/prismcat/${yyyy}/${MM}-${dd}')
    const [archiveScheduleTime, setArchiveScheduleTime] = useState('02:00')
    const [archiveTimezone, setArchiveTimezone] = useState('Asia/Shanghai')
    const [archiveZstdLevel, setArchiveZstdLevel] = useState(10)
    const [archiveLocalRetentionHours, setArchiveLocalRetentionHours] = useState(24)
    const [archiveImportRetentionHours, setArchiveImportRetentionHours] = useState(24)
    const [archiveDeletionPreview, setArchiveDeletionPreview] = useState(0)
    const [archiveStatus, setArchiveStatus] = useState<ArchiveStatus | null>(null)
    const [archiveAction, setArchiveAction] = useState<'test' | 'run' | ''>('')
    const archiveKeyInputRef = useRef<HTMLInputElement>(null)
    const [requestOverridesEnabled, setRequestOverridesEnabled] = useState(false)
    const [overrideMaxBodyKB, setOverrideMaxBodyKB] = useState(1024)
    const [overrideRulesText, setOverrideRulesText] = useState('')
    const [selectedOverrideRuleIndex, setSelectedOverrideRuleIndex] = useState(0)
    const [selectedOverrideRuleText, setSelectedOverrideRuleText] = useState('')
    const [selectedOverrideRuleName, setSelectedOverrideRuleName] = useState('')
    const [selectedOverrideMatchText, setSelectedOverrideMatchText] = useState('')
    const [selectedOverridePatchText, setSelectedOverridePatchText] = useState('')
    const [selectedRuleAdvancedOpen, setSelectedRuleAdvancedOpen] = useState(false)
    const [advancedRulesOpen, setAdvancedRulesOpen] = useState(false)
    const [overrideBindings, setOverrideBindings] = useState<Record<string, OverrideBinding>>({})
    const [usageExtractionEnabled, setUsageExtractionEnabled] = useState(false)
    const [usageRulesText, setUsageRulesText] = useState('')
    const [selectedUsageRuleIndex, setSelectedUsageRuleIndex] = useState(0)
    const [selectedUsageRuleText, setSelectedUsageRuleText] = useState('')
    const [usageAdvancedRulesOpen, setUsageAdvancedRulesOpen] = useState(false)
    const [usageBindings, setUsageBindings] = useState<Record<string, OverrideBinding>>({})
    const [dirtyTargetBindingUpstreams, setDirtyTargetBindingUpstreams] = useState<Set<string>>(new Set())

    const domainSuffix = config?.server?.proxy_domains?.[0] || 'localhost'
    const previewUpstreamName = upstreams[0]?.name || 'openai'
    const archiveKeyError = validateArchiveKeyPrefix(archiveKeyPrefix)
    const archiveKeySample = useMemo(() => {
        const parts = archiveDateParts(archiveTimezone)
        const resolved = archiveKeyPrefix.trim()
            .replaceAll('${yyyy}', parts.yyyy)
            .replaceAll('${MM}', parts.MM)
            .replaceAll('${dd}', parts.dd)
        return {
            date: `${parts.yyyy}-${parts.MM}-${parts.dd}`,
            key: `${resolved}/prismcat-${parts.yyyy}${parts.MM}${parts.dd}-<batch-id>.tar.zst`,
        }
    }, [archiveKeyPrefix, archiveTimezone])
    const archiveS3Ready = Boolean(
        archiveRegion.trim() && archiveBucket.trim() && archiveAccessKeyID.trim()
        && (archiveSecretConfigured || archiveSecretAccessKey.trim()),
    )
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
            const name = typeof parsed.name === 'string' ? parsed.name.trim() : ''
            if (!name) return t('settings.request_override_rule_name_required')
            if (overrideRuleObjects.some((rule, index) => (
                index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
            ))) {
                return t('settings.request_override_rule_name_duplicate')
            }
            return ''
        } catch {
            return t('settings.request_override_rule_invalid')
        }
    }, [overrideRuleObjects, selectedOverrideRuleIndex, selectedOverrideRuleText, t])
    const selectedOverrideRuleNameError = useMemo(() => {
        const name = selectedOverrideRuleName.trim()
        if (!name) return t('settings.request_override_rule_name_required')
        if (overrideRuleObjects.some((rule, index) => (
            index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
        ))) {
            return t('settings.request_override_rule_name_duplicate')
        }
        return ''
    }, [overrideRuleObjects, selectedOverrideRuleIndex, selectedOverrideRuleName, t])
    const selectedOverrideMatchError = useMemo(() => {
        try {
            const parsed = JSON.parse(selectedOverrideMatchText)
            return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
                ? ''
                : t('settings.request_override_match_must_be_object')
        } catch {
            return t('settings.request_override_match_invalid')
        }
    }, [selectedOverrideMatchText, t])
    const selectedOverridePatchError = useMemo(() => {
        try {
            return Array.isArray(JSON.parse(selectedOverridePatchText))
                ? ''
                : t('settings.request_override_patch_must_be_array')
        } catch {
            return t('settings.request_override_patch_invalid')
        }
    }, [selectedOverridePatchText, t])
    const selectedOverrideRulePreview = useMemo(() => {
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        return rule
            ? JSON.stringify(redactSensitiveRuleHeaders(rule, sensitiveHeaders), null, 2)
            : ''
    }, [overrideRuleObjects, selectedOverrideRuleIndex, sensitiveHeaders])
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

    // 列表里只显示 host,不显示 scheme:入口地址的协议恒等于面板自身的 origin,
    // 是常量,占宽度却不携带信息。复制出去的仍然是 getProxyUrl 的完整 URL。
    const getProxyHost = useCallback((name: string) => {
        return `${name}.${domainSuffix}${proxyBase.portSuffix}`
    }, [proxyBase, domainSuffix])

    const getProxyUrl = useCallback((name: string) => {
        return `${proxyBase.proto}//${name}.${domainSuffix}${proxyBase.portSuffix}`
    }, [proxyBase, domainSuffix])

    const normalizedPathPrefix = useMemo(() => {
        let prefix = pathRoutingPrefix.trim() || '/_proxy'
        if (!prefix.startsWith('/')) {
            prefix = `/${prefix}`
        }
        return prefix.replace(/\/+$/, '') || '/_proxy'
    }, [pathRoutingPrefix])

    const getPathProxyUrl = useCallback((name: string) => {
        return `${proxyBase.proto}//${proxyBase.hostname}${proxyBase.portSuffix}${normalizedPathPrefix}/${name}`
    }, [proxyBase, normalizedPathPrefix])

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
            setSelectedOverrideRuleName('')
            setSelectedOverrideMatchText('')
            setSelectedOverridePatchText('')
            return
        }

        const nextIndex = Math.min(selectedOverrideRuleIndex, overrideRuleObjects.length - 1)
        if (nextIndex !== selectedOverrideRuleIndex) {
            setSelectedOverrideRuleIndex(nextIndex)
        }
        const nextRule = overrideRuleObjects[nextIndex]
        setSelectedOverrideRuleText(JSON.stringify(nextRule, null, 2))
        setSelectedOverrideRuleName(getOverrideRuleName(nextRule, `rule-${nextIndex + 1}`))
        setSelectedOverrideMatchText(JSON.stringify(nextRule.match ?? {}, null, 2))
        setSelectedOverridePatchText(JSON.stringify(nextRule.patch ?? [], null, 2))
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
            const [upstreamsData, configData, modelPathData] = await Promise.all([
                fetchUpstreams(),
                fetchConfig(),
                fetchModelPathTemplates(),
            ])
            setUpstreams(upstreamsData || [])
            setDirtyTargetBindingUpstreams(new Set())
            setConfig(configData)
            setModelPathTemplates(modelPathData.templates || [])
            setShowAddForm(prev => prev || !upstreamsData?.length)
            const nextOrder = Math.max(0, ...(upstreamsData || []).map(item => item.order || 0)) + 10
            setNewOrder(nextOrder)

            setMaxRequestBody(Math.round(configData.logging.max_request_body / 1024))
            setMaxResponseBody(Math.round(configData.logging.max_response_body / 1024))
            setSensitiveHeaders(configData.logging.sensitive_headers.join('\n'))
            setStoreBase64(configData.logging.store_base64)
            setEarlyRequestBodySnapshot(configData.logging.early_request_body_snapshot)
            setBodyCompressionAlgorithm(configData.storage.body_compression?.algorithm ?? 'zstd')
            setBodyCompressionLevel(configData.storage.body_compression?.level ?? 3)
            setRetentionDays(configData.storage.retention_days)
            setMaxStorageGB(parseFloat((configData.storage.max_storage_bytes / (1024 * 1024 * 1024)).toFixed(2)))
            setArchiveEnabled(configData.archive?.enabled ?? false)
            setArchiveEndpoint(configData.archive?.s3?.endpoint ?? '')
            setArchiveRegion(configData.archive?.s3?.region ?? '')
            setArchiveBucket(configData.archive?.s3?.bucket ?? '')
            setArchiveAccessKeyID(configData.archive?.s3?.access_key_id ?? '')
            setArchiveSecretAccessKey('')
            setArchiveSecretConfigured(configData.archive?.s3?.secret_configured ?? false)
            setArchiveForcePathStyle(configData.archive?.s3?.force_path_style ?? false)
            setArchiveKeyPrefix(configData.archive?.key_prefix ?? 'backups/prismcat/${yyyy}/${MM}-${dd}')
            setArchiveScheduleTime(configData.archive?.schedule_time ?? '02:00')
            setArchiveTimezone(configData.archive?.timezone ?? 'Asia/Shanghai')
            setArchiveZstdLevel(configData.archive?.zstd_level ?? 10)
            setArchiveLocalRetentionHours(configData.archive?.local_retention_hours ?? 24)
            setArchiveImportRetentionHours(configData.archive?.import_retention_hours ?? 24)
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

    const refreshArchiveStatus = async () => {
        try {
            setArchiveStatus(await fetchArchives(false))
        } catch (err) {
            toast.error(getErrorMessage(err, t('settings.archive_status_failed')))
        }
    }

    const archiveConfigPayload = () => ({
        enabled: archiveEnabled,
        s3: {
            endpoint: archiveEndpoint,
            region: archiveRegion,
            bucket: archiveBucket,
            access_key_id: archiveAccessKeyID,
            secret_access_key: archiveSecretAccessKey,
            force_path_style: archiveForcePathStyle,
        },
        key_prefix: archiveKeyPrefix,
        schedule_time: archiveScheduleTime,
        timezone: archiveTimezone,
        zstd_level: archiveZstdLevel,
        local_retention_hours: archiveLocalRetentionHours,
        import_retention_hours: archiveImportRetentionHours,
    })

    const insertArchivePlaceholder = (token: string) => {
        const input = archiveKeyInputRef.current
        const start = input?.selectionStart ?? archiveKeyPrefix.length
        const end = input?.selectionEnd ?? start
        setArchiveKeyPrefix(`${archiveKeyPrefix.slice(0, start)}${token}${archiveKeyPrefix.slice(end)}`)
        window.setTimeout(() => {
            input?.focus()
            input?.setSelectionRange(start + token.length, start + token.length)
        }, 0)
    }

    const handleArchiveTest = async () => {
        setArchiveAction('test')
        try {
            await testArchiveConnection(archiveConfigPayload())
            toast.success(t('settings.archive_test_success'))
            await refreshArchiveStatus()
        } catch (err) {
            toast.error(getErrorMessage(err, t('settings.archive_test_failed')))
        } finally {
            setArchiveAction('')
        }
    }

    const handleArchiveRun = async () => {
        setArchiveAction('run')
        try {
            await runArchiveNow()
            toast.success(t('settings.archive_run_success'))
            await refreshArchiveStatus()
        } catch (err) {
            toast.error(getErrorMessage(err, t('settings.archive_run_failed')))
        } finally {
            setArchiveAction('')
        }
    }

    useEffect(() => {
        if (activeTab !== 'logging') return
        void refreshArchiveStatus()
        // Archive state is refreshed explicitly after commands; configuration
        // loading should not continuously poll S3 from the settings form.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeTab])

    useEffect(() => {
        if (activeTab !== 'logging' || !archiveStatus?.running) return
        const timer = window.setInterval(() => void refreshArchiveStatus(), 2000)
        return () => window.clearInterval(timer)
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeTab, archiveStatus?.running])

    useEffect(() => {
        if (activeTab !== 'logging' || archiveLocalRetentionHours < 1) return
        const timer = window.setTimeout(() => {
            void fetchArchiveDeletionPreview(archiveLocalRetentionHours)
                .then(setArchiveDeletionPreview)
                .catch(() => setArchiveDeletionPreview(0))
        }, 250)
        return () => window.clearTimeout(timer)
    }, [activeTab, archiveLocalRetentionHours])

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
                '',
                undefined,
                newLoggingPathFilter,
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
            setNewLoggingPathFilter({ mode: 'all', rules: [] })
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
        const selectedRuleExists = Boolean(overrideRuleObjects[selectedOverrideRuleIndex])
        const editorError = selectedRuleExists
            ? selectedOverrideRuleNameError || selectedOverrideMatchError || selectedOverridePatchError || selectedOverrideRuleError
            : ''
        if (editorError) {
            throw new Error(editorError)
        }
        const trimmedRules = overrideRulesText.trim()
        if (!trimmedRules) return []
        const parsed = JSON.parse(trimmedRules)
        if (!Array.isArray(parsed)) {
            throw new Error(t('settings.request_overrides_rules_must_be_array'))
        }
        const names = new Set<string>()
        for (const rule of parsed) {
            const name = getOverrideRuleName(rule, '')
            if (!name) throw new Error(t('settings.request_override_rule_name_required'))
            if (names.has(name)) throw new Error(t('settings.request_override_rule_name_duplicate'))
            names.add(name)
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
        const safeIndex = Math.max(0, Math.min(nextIndex, Math.max(0, rules.length - 1)))
        const selectedRule = rules[safeIndex]
        setOverrideRulesText(rules.length ? JSON.stringify(rules, null, 2) : '')
        setSelectedOverrideRuleIndex(safeIndex)
        setSelectedOverrideRuleText(selectedRule ? JSON.stringify(selectedRule, null, 2) : '')
        setSelectedOverrideRuleName(selectedRule ? getOverrideRuleName(selectedRule, `rule-${safeIndex + 1}`) : '')
        setSelectedOverrideMatchText(selectedRule ? JSON.stringify(selectedRule.match ?? {}, null, 2) : '')
        setSelectedOverridePatchText(selectedRule ? JSON.stringify(selectedRule.patch ?? [], null, 2) : '')
    }

    const handleActivateTarget = async (upstreamName: string, targetName: string) => {
        setSwitchingTarget(upstreamName)
        try {
            await activateUpstreamTarget(upstreamName, targetName)
            await loadData()
            toast.success(t('upstream_manager.target_switched', { target: targetName }))
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        } finally {
            setSwitchingTarget('')
        }
    }

    const handleSelectOverrideRule = (index: number) => {
        const rule = overrideRuleObjects[index]
        if (!rule) return
        setSelectedOverrideRuleIndex(index)
        setSelectedOverrideRuleText(JSON.stringify(rule, null, 2))
        setSelectedOverrideRuleName(getOverrideRuleName(rule, `rule-${index + 1}`))
        setSelectedOverrideMatchText(JSON.stringify(rule.match ?? {}, null, 2))
        setSelectedOverridePatchText(JSON.stringify(rule.patch ?? [], null, 2))
        setSelectedRuleAdvancedOpen(false)
    }

    const syncTargetBindingRuleNames = (
        kind: BindingKind,
        transform: (names: string[]) => string[],
    ) => {
        setUpstreams(current => {
            const changed = new Set<string>()
            const next = current.map(upstream => {
                if (!upstream.targets || Object.keys(upstream.targets).length === 0) return upstream
                let upstreamChanged = false
                const targets = Object.fromEntries(Object.entries(upstream.targets).map(([targetName, target]) => {
                    const binding = target[kind]
                    if (!binding) return [targetName, target]
                    const nextNames = transform(getBindingRuleNames(binding))
                    if (nextNames.join('\u0000') === getBindingRuleNames(binding).join('\u0000')) return [targetName, target]
                    upstreamChanged = true
                    return [targetName, {
                        ...target,
                        [kind]: { ...binding, rule_names: nextNames },
                    }]
                })) as Record<string, UpstreamTarget>
                if (!upstreamChanged) return upstream
                changed.add(upstream.name)
                return { ...upstream, targets }
            })
            if (changed.size > 0) {
                setDirtyTargetBindingUpstreams(previous => new Set([...previous, ...changed]))
            }
            return next
        })
    }

    const replaceSelectedOverrideRule = (nextRule: OverrideRuleObject, syncBindingName = false) => {
        const currentRule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!currentRule) return
        const previousName = getOverrideRuleName(currentRule, '')
        const nextName = getOverrideRuleName(nextRule, '')
        const nextRules = [...overrideRuleObjects]
        nextRules[selectedOverrideRuleIndex] = nextRule
        setOverrideRulesText(JSON.stringify(nextRules, null, 2))
        setSelectedOverrideRuleText(JSON.stringify(nextRule, null, 2))

        if (syncBindingName && previousName && nextName && previousName !== nextName) {
            setOverrideBindings(current => Object.fromEntries(
                Object.entries(current).map(([upstream, binding]) => [
                    upstream,
                    {
                        ...binding,
                        rule_names: getBindingRuleNames(binding).map(name => name === previousName ? nextName : name),
                    },
                ]),
            ))
            syncTargetBindingRuleNames('request_overrides', names => names.map(name => name === previousName ? nextName : name))
        }
    }

    const handleOverrideRuleNameChange = (value: string) => {
        setSelectedOverrideRuleName(value)
    }

    const handleCommitOverrideRuleName = () => {
        const name = selectedOverrideRuleName.trim()
        if (!name || overrideRuleObjects.some((rule, index) => (
            index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
        ))) return
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!rule) return
        setSelectedOverrideRuleName(name)
        replaceSelectedOverrideRule({ ...rule, name }, true)
    }

    const handleOverrideMatchTextChange = (value: string) => {
        setSelectedOverrideMatchText(value)
        try {
            const match = JSON.parse(value)
            if (!match || typeof match !== 'object' || Array.isArray(match)) return
            const rule = overrideRuleObjects[selectedOverrideRuleIndex]
            if (rule) replaceSelectedOverrideRule({ ...rule, match })
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleOverridePatchTextChange = (value: string) => {
        setSelectedOverridePatchText(value)
        try {
            const patch = JSON.parse(value)
            if (!Array.isArray(patch)) return
            const rule = overrideRuleObjects[selectedOverrideRuleIndex]
            if (rule) replaceSelectedOverrideRule({ ...rule, patch })
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleOverrideRuleTextChange = (value: string) => {
        setSelectedOverrideRuleText(value)
        try {
            const parsed = JSON.parse(value)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
            const nextRule = parsed as OverrideRuleObject
            const name = getOverrideRuleName(nextRule, '')
            if (!name || overrideRuleObjects.some((rule, index) => (
                index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
            ))) return
            replaceSelectedOverrideRule(nextRule, true)
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

    const handleAddOverrideExample = () => {
        if (overrideRulesParse.error) return
        const [template] = JSON.parse(requestOverrideExample) as OverrideRuleObject[]
        const nextRule = {
            ...template,
            name: uniqueRuleName(getOverrideRuleName(template, 'Example rule'), overrideRuleObjects),
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
        syncTargetBindingRuleNames('request_overrides', names => names.filter(name => name !== ruleName))
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
            const previousName = getOverrideRuleName(nextRules[selectedUsageRuleIndex], '')
            const nextName = getOverrideRuleName(parsed, '')
            nextRules[selectedUsageRuleIndex] = parsed as OverrideRuleObject
            setUsageRulesText(JSON.stringify(nextRules, null, 2))
            if (previousName && nextName && previousName !== nextName) {
                setUsageBindings(current => Object.fromEntries(
                    Object.entries(current).map(([upstream, binding]) => [
                        upstream,
                        {
                            ...binding,
                            rule_names: getBindingRuleNames(binding).map(name => name === previousName ? nextName : name),
                        },
                    ]),
                ))
                syncTargetBindingRuleNames('usage_extraction', names => names.map(name => name === previousName ? nextName : name))
            }
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

    const handleMergeDefaultUsageRules = () => {
        if (usageRulesParse.error) return
        const defaults = JSON.parse(usageExtractionExample) as OverrideRuleObject[]
        const existingNames = new Set(usageRuleObjects.map(rule => getOverrideRuleName(rule, '')))
        const additions = defaults.filter(rule => !existingNames.has(getOverrideRuleName(rule, '')))
        if (additions.length === 0) {
            toast.info(t('settings.usage_extraction_defaults_complete'))
            return
        }
        setUsageRulesArray([...usageRuleObjects, ...additions], usageRuleObjects.length)
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
        syncTargetBindingRuleNames('usage_extraction', names => names.filter(name => name !== ruleName))
        setUsageRulesArray(nextRules, Math.min(index, nextRules.length - 1))
    }

    const handleToggleUsageRule = (index: number, enabled: boolean) => {
        const nextRules = [...usageRuleObjects]
        nextRules[index] = { ...nextRules[index], enabled }
        setUsageRulesArray(nextRules, index)
    }

    const handleEditUpstream = useCallback((upstream: Upstream) => {
        const binding = overrideBindings[upstream.name]
        const usageBinding = usageBindings[upstream.name]
        const targets = upstream.targets || {}
        const usesTargetPresets = Object.keys(targets).length > 0
        const selectedTarget = usesTargetPresets
            ? (upstream.active_target && targets[upstream.active_target] ? upstream.active_target : Object.keys(targets)[0])
            : ''
        // 必须从实际存值的地方读:启用了目标预设的上游,这些字段在 targets[名字] 里,
        // 上游级一律是 0,绑定还会被 delete 掉 —— 原来的判断对它们永远为假
        setUpstreamAdvancedOpen(hasAdvancedValues(
            usesTargetPresets
                ? targets[selectedTarget]
                : {
                    response_header_timeout: upstream.response_header_timeout,
                    response_body_first_byte_timeout: upstream.response_body_first_byte_timeout,
                    response_body_idle_timeout: upstream.response_body_idle_timeout,
                    request_overrides: binding,
                    usage_extraction: usageBinding,
                },
        ))
        const editing: EditingUpstream = {
            name: upstream.name,
            target: upstream.target,
            timeout: upstream.timeout,
            responseHeaderTimeout: upstream.response_header_timeout || 0,
            responseBodyFirstByteTimeout: upstream.response_body_first_byte_timeout || 0,
            responseBodyIdleTimeout: upstream.response_body_idle_timeout || 0,
            order: upstream.order || 0,
            outboundProxy: upstream.outbound_proxy || 'env',
            loggingEnabled: upstream.logging_enabled !== false,
            loggingPathFilter: upstream.logging_path_filter || { mode: 'all', rules: [] },
            overrideEnabled: usesTargetPresets ? false : (binding?.enabled ?? false),
            ruleNames: usesTargetPresets ? [] : getBindingRuleNames(binding),
            usageEnabled: usesTargetPresets ? false : (usageBinding?.enabled ?? false),
            usageRuleNames: usesTargetPresets ? [] : getBindingRuleNames(usageBinding),
            usesTargetPresets,
            activeTarget: upstream.active_target || selectedTarget,
            selectedTarget,
            targets,
        }
        setNewTargetPresetName('')
        setEditingUpstream(usesTargetPresets ? loadTargetBuffer(editing, selectedTarget) : editing)
    }, [overrideBindings, usageBindings])

    useEffect(() => {
        if (deepLinkHandled.current || loading || activeTab !== 'routing') return
        const requested = searchParams.get('edit')
        if (!requested) return
        const upstream = upstreams.find(item => item.name === requested)
        if (!upstream) return
        deepLinkHandled.current = true
        handleEditUpstream(upstream)
    }, [activeTab, handleEditUpstream, loading, searchParams, upstreams])

    const handleEnableTargetPresets = () => {
        setEditingUpstream(current => {
            if (!current || current.usesTargetPresets) return current
            const next = {
                ...current,
                usesTargetPresets: true,
                activeTarget: 'default',
                selectedTarget: 'default',
                targets: { default: editingBufferAsTarget(current) },
            }
            return next
        })
    }

    const handleSelectTargetPreset = (targetName: string) => {
        // 切到一个有高级设置的预设时把面板展开,否则那些非默认值是看不见的。
        // 只展开不收起 —— 自动关掉用户手动打开的面板更烦人。
        // commitSelectedTarget 只回写当前选中的那个,所以这里读切换前的状态等价
        if (hasAdvancedValues(editingUpstream?.targets[targetName])) {
            setUpstreamAdvancedOpen(true)
        }
        setEditingUpstream(current => {
            if (!current) return current
            return loadTargetBuffer(commitSelectedTarget(current), targetName)
        })
    }

    const handleAddTargetPreset = () => {
        const name = newTargetPresetName.trim().toLowerCase()
        if (!name) return
        setEditingUpstream(current => {
            if (!current) return current
            const committed = commitSelectedTarget(current)
            if (committed.targets[name]) {
                toast.error(t('upstream_manager.target_name_duplicate'))
                return current
            }
            const target: UpstreamTarget = {
                url: '',
                timeout: current.timeout,
                response_header_timeout: current.responseHeaderTimeout,
                response_body_first_byte_timeout: current.responseBodyFirstByteTimeout,
                response_body_idle_timeout: current.responseBodyIdleTimeout,
                outbound_proxy: current.outboundProxy,
                request_overrides: { enabled: false, rule_names: [] },
                usage_extraction: {
                    enabled: false,
                    rule_names: [],
                },
            }
            const next = { ...committed, targets: { ...committed.targets, [name]: target } }
            return loadTargetBuffer(next, name)
        })
        setNewTargetPresetName('')
    }

    const handleRemoveTargetPreset = () => {
        setEditingUpstream(current => {
            if (!current || !current.usesTargetPresets || Object.keys(current.targets).length <= 1) return current
            const committed = commitSelectedTarget(current)
            const targets = { ...committed.targets }
            delete targets[committed.selectedTarget]
            const nextName = Object.keys(targets)[0]
            const activeTarget = committed.activeTarget === committed.selectedTarget ? nextName : committed.activeTarget
            return loadTargetBuffer({ ...committed, targets, activeTarget }, nextName)
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
            const committed = commitSelectedTarget(editingUpstream)
            const overrideRules = parseOverrideRules()
            const usageRules = parseUsageRules()
            const nextBindings = { ...overrideBindings }
            const nextUsageBindings = { ...usageBindings }
            if (committed.usesTargetPresets) {
                delete nextBindings[committed.name]
                delete nextUsageBindings[committed.name]
                if (Object.values(committed.targets).some(target => !target.url.trim())) {
                    throw new Error(t('upstream_manager.target_url_required'))
                }
            } else {
                nextBindings[committed.name] = {
                    enabled: committed.overrideEnabled,
                    rule_names: committed.ruleNames,
                }
                nextUsageBindings[committed.name] = {
                    enabled: committed.usageEnabled,
                    rule_names: committed.usageRuleNames,
                }
            }
            await addUpstream(
                committed.name,
                committed.usesTargetPresets ? '' : committed.target,
                committed.usesTargetPresets ? 0 : committed.timeout,
                committed.usesTargetPresets ? 0 : committed.responseHeaderTimeout,
                committed.usesTargetPresets ? 0 : committed.responseBodyFirstByteTimeout,
                committed.usesTargetPresets ? 0 : committed.responseBodyIdleTimeout,
                committed.order,
                committed.usesTargetPresets ? '' : normalizedOutboundProxy(committed.outboundProxy),
                committed.loggingEnabled,
                committed.usesTargetPresets ? committed.activeTarget : '',
                committed.usesTargetPresets ? committed.targets : undefined,
                committed.loggingPathFilter,
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

    const saveDirtyTargetBindings = async () => {
        for (const upstream of upstreams) {
            if (!dirtyTargetBindingUpstreams.has(upstream.name) || !upstream.targets || Object.keys(upstream.targets).length === 0) continue
            await addUpstream(
                upstream.name,
                '',
                0,
                0,
                0,
                0,
                upstream.order || 0,
                '',
                upstream.logging_enabled !== false,
                upstream.active_target || '',
                upstream.targets,
            )
        }
    }

    const handleSaveAll = async () => {
		if (archiveKeyError) {
			toast.error(t(`settings.archive_key_error_${archiveKeyError}`))
			return
		}
		if (archiveEnabled && !archiveS3Ready) {
			toast.error(t('settings.archive_s3_required'))
			return
		}
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
                    store_base64: storeBase64,
                    early_request_body_snapshot: earlyRequestBodySnapshot,
                },
                storage: {
                    retention_days: retentionDays,
                    max_storage_bytes: Math.round(maxStorageGB * 1024 * 1024 * 1024),
                    body_compression: {
                        algorithm: bodyCompressionAlgorithm,
                        level: bodyCompressionLevel,
                    },
                },
                archive: archiveConfigPayload(),
                request_overrides: buildOverridesPayload(overrideBindings, overrideRules),
                usage_extraction: buildUsageExtractionPayload(usageBindings, usageRules),
            })
            await saveDirtyTargetBindings()
            notifyArchiveFeatureChanged(archiveEnabled)
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
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
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
                    "max-h-[calc(100vh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border border-input bg-card p-0 transition-[max-width] duration-200",
                    upstreamAdvancedOpen ? "max-w-2xl lg:max-w-5xl" : "max-w-2xl",
                )}>
                    <DialogHeader className="border-b border-input px-6 py-5">
                        <DialogTitle className="text-base font-semibold">
                            {editingUpstream ? t('upstream_manager.edit_title', { name: editingUpstream.name }) : t('common.edit')}
                        </DialogTitle>
                        <DialogDescription className="sr-only">
                            {t('upstream_manager.edit_description')}
                        </DialogDescription>
                    </DialogHeader>
                    {editingUpstream && (
                        <>
                            <div className="min-h-0 space-y-6 overflow-y-auto px-6 py-5">
                                {/* 上游级字段:这三个不属于任何预设,所以留在预设面板外面。
                                    记录日志原来混在右栏的预设级分组里,看着像能按预设分别关,
                                    实际 loggingEnabled 不在 editingBufferAsTarget 里 */}
                                <section className="space-y-4">
                                    <div className="text-sm font-semibold text-foreground">
                                        {t('upstream_manager.scope_upstream')}
                                    </div>
                                    <div className="grid grid-cols-1 gap-5 sm:grid-cols-[minmax(0,1fr)_120px]">
                                        <FieldBlock label={t('upstream_manager.name')}>
                                            <Input
                                                value={editingUpstream.name}
                                                readOnly
                                                className="h-9 rounded-md border-input bg-muted/50 font-mono text-sm"
                                            />
                                        </FieldBlock>
                                        <FieldBlock label={t('upstream_manager.order')}>
                                            <Input
                                                type="number"
                                                min={0}
                                                value={editingUpstream.order}
                                                onChange={e => setEditingUpstream(current => current ? { ...current, order: Number(e.target.value) } : current)}
                                                className="h-9 rounded-md border-input bg-background text-sm"
                                            />
                                        </FieldBlock>
                                    </div>
                                    <ToggleSetting
                                        label={t('upstream_manager.logging_enabled')}
                                        description={t('upstream_manager.logging_enabled_hint')}
                                        checked={editingUpstream.loggingEnabled}
                                        onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, loggingEnabled: checked } : current)}
                                    />
                                    <div className="space-y-2 border-l-2 border-border pl-4">
                                        <div>
                                            <div className="text-sm font-medium text-foreground">
                                                {t('upstream_manager.logging_path_mode')}
                                            </div>
                                            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                                                {t('upstream_manager.logging_path_mode_hint')}
                                            </p>
                                        </div>
                                        <UpstreamLoggingPathFilter
                                            value={editingUpstream.loggingPathFilter}
                                            onChange={loggingPathFilter => setEditingUpstream(current => current ? { ...current, loggingPathFilter } : current)}
                                            templates={modelPathTemplates}
                                            disabled={!editingUpstream.loggingEnabled}
                                        />
                                    </div>
                                </section>

                                {/* 预设级字段:切换器在面板上沿,面板边框包住的正是它支配的范围。
                                    原来边框只圈住选择器本身,被支配的八组字段全在框外面 */}
                                <section>
                                    <div className="mb-3 flex flex-wrap items-center gap-3">
                                        <div className="flex items-center gap-2.5">
                                            <span className="text-sm font-semibold text-foreground">
                                                {editingUpstream.usesTargetPresets
                                                    ? t('upstream_manager.target_presets')
                                                    : t('upstream_manager.scope_target')}
                                            </span>
                                            <InfoTooltip content={t('upstream_manager.target_presets_hint')} />
                                        </div>
                                        {editingUpstream.usesTargetPresets ? (
                                            <>
                                                {/* 用 aria-pressed 的按钮组而不是伪造 tab 语义:
                                                    不必自己实现方向键导航,可达性也不打折 */}
                                                <div
                                                    role="group"
                                                    aria-label={t('upstream_manager.target_presets')}
                                                    className="inline-flex items-center gap-0.5 rounded-lg bg-muted p-[3px]"
                                                >
                                                    {Object.keys(editingUpstream.targets).map(name => (
                                                        <button
                                                            key={name}
                                                            type="button"
                                                            aria-pressed={name === editingUpstream.selectedTarget}
                                                            onClick={() => handleSelectTargetPreset(name)}
                                                            className={cn(
                                                                'rounded-md px-2.5 py-1 text-xs transition-colors',
                                                                name === editingUpstream.selectedTarget
                                                                    ? 'bg-background text-foreground shadow-sm'
                                                                    : 'text-muted-foreground hover:text-foreground',
                                                            )}
                                                        >
                                                            {name}
                                                            {name === editingUpstream.activeTarget && (
                                                                <span className="ml-1.5 text-muted-foreground">
                                                                    ·{t('upstream_manager.active_target')}
                                                                </span>
                                                            )}
                                                        </button>
                                                    ))}
                                                </div>
                                                {/* 不用 ml-auto:宽屏时它会把这组控件顶到最右边,
                                                    和左边的切换器之间留出一大片空,窄屏时又变成
                                                    单独一行右对齐的孤儿。紧跟着切换器排就行 */}
                                                <div className="flex items-center gap-1.5">
                                                    <Input
                                                        value={newTargetPresetName}
                                                        onChange={event => setNewTargetPresetName(event.target.value)}
                                                        onKeyDown={event => {
                                                            if (event.key === 'Enter') {
                                                                event.preventDefault()
                                                                handleAddTargetPreset()
                                                            }
                                                        }}
                                                        placeholder={t('upstream_manager.new_target_name')}
                                                        className="h-8 w-[200px] rounded-md border-input bg-background text-xs"
                                                    />
                                                    <Button
                                                        type="button"
                                                        variant="outline"
                                                        size="sm"
                                                        className="h-8"
                                                        onClick={handleAddTargetPreset}
                                                        disabled={!newTargetPresetName.trim()}
                                                    >
                                                        <Plus className="mr-1 h-3.5 w-3.5" />
                                                        {t('common.add')}
                                                    </Button>
                                                </div>
                                            </>
                                        ) : (
                                            <Button
                                                type="button"
                                                variant="outline"
                                                size="sm"
                                                className="ml-auto h-8"
                                                onClick={handleEnableTargetPresets}
                                            >
                                                <Plus className="mr-1.5 h-3.5 w-3.5" />
                                                {t('upstream_manager.enable_target_presets')}
                                            </Button>
                                        )}
                                    </div>

                                    <div className={cn(
                                        'space-y-5',
                                        editingUpstream.usesTargetPresets && 'rounded-md border border-input bg-muted/15 p-4',
                                    )}>
                                        {/* 基础字段整宽排、高级设置整宽排在下面。原来是左右两栏,
                                            左栏只有三个字段、右栏是一大块面板,右边一展开左下就空一片 */}
                                        <div className="space-y-5">
                                            <div className="space-y-5">
                                                <FieldBlock label={t('upstream_manager.target')}>
                                                    <Input
                                                        value={editingUpstream.target}
                                                        onChange={e => setEditingUpstream(current => current ? { ...current, target: e.target.value } : current)}
                                                        className="h-9 max-w-2xl rounded-md border-input bg-background font-mono text-sm"
                                                    />
                                                </FieldBlock>
                                                <div className="grid grid-cols-1 gap-5 sm:grid-cols-[140px_minmax(0,1fr)] sm:max-w-2xl">
                                                    <FieldBlock label={t('upstream_manager.timeout')}>
                                                        <Input
                                                            type="number"
                                                            min="1"
                                                            value={editingUpstream.timeout}
                                                            onChange={e => setEditingUpstream(current => current ? { ...current, timeout: Number(e.target.value) } : current)}
                                                            className="h-9 rounded-md border-input bg-background text-sm"
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
                                            >
                                                <AdvancedSettingsGroup
                                                    title={t('upstream_manager.timeout_strategy')}
                                                    description={t('upstream_manager.timeout_strategy_hint')}
                                                >
                                                    <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
                                                        <FieldBlock
                                                            label={t('upstream_manager.response_header_timeout')}
                                                            hint={t('upstream_manager.response_header_timeout_hint')}
                                                        >
                                                            <Input
                                                                type="number"
                                                                min="0"
                                                                value={editingUpstream.responseHeaderTimeout}
                                                                onChange={e => setEditingUpstream(current => current ? { ...current, responseHeaderTimeout: Number(e.target.value) } : current)}
                                                                className="h-9 rounded-md border-input bg-background text-sm"
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
                                                                className="h-9 rounded-md border-input bg-background text-sm"
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
                                                                className="h-9 rounded-md border-input bg-background text-sm"
                                                            />
                                                        </FieldBlock>
                                                    </div>
                                                </AdvancedSettingsGroup>

                                                <AdvancedSettingsGroup title={t('upstream_manager.request_overrides')} divider>
                                                        <ToggleSetting
                                                            label={t('upstream_manager.override_enabled')}
                                                            description={t('upstream_manager.override_enabled_hint')}
                                                            checked={editingUpstream.overrideEnabled}
                                                            onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, overrideEnabled: checked } : current)}
                                                        />
                                                        {editingUpstream.overrideEnabled && (
                                                            <div className="space-y-2">
                                                                <div className="text-xs font-semibold text-muted-foreground">
                                                                    {t('upstream_manager.bound_rules')}
                                                                </div>
                                                                {parsedOverrideRules.length === 0 ? (
                                                                    <div className="rounded-lg border border-dashed border-input bg-background px-3 py-4 text-xs text-muted-foreground">
                                                                        {t('upstream_manager.no_rules')}
                                                                    </div>
                                                                ) : (
                                                                    <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                                                                        {parsedOverrideRules.map((ruleName, ruleIndex) => {
                                                                            const rule = overrideRuleObjects[ruleIndex]
                                                                            const ruleEnabled = getOverrideRuleEnabled(rule)
                                                                            const checked = editingUpstream.ruleNames.includes(ruleName)
                                                                            return (
                                                                                <label
                                                                                    key={`${ruleName}-${ruleIndex}`}
                                                                                    className={cn(
                                                                                        "flex items-center gap-2 rounded-lg border border-input bg-background px-3 py-2 text-sm",
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
                                                                                        <Badge variant="outline" className="ml-auto h-5 shrink-0 rounded-md border-input bg-muted/50 px-1.5 text-xs text-muted-foreground">
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

                                                <AdvancedSettingsGroup title={t('upstream_manager.usage_stats')} divider>
                                                        <ToggleSetting
                                                            label={t('upstream_manager.usage_enabled')}
                                                            description={t('upstream_manager.usage_enabled_hint')}
                                                            checked={editingUpstream.usageEnabled}
                                                            onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, usageEnabled: checked } : current)}
                                                        />
                                                        {editingUpstream.usageEnabled && (
                                                            <div className="space-y-2">
                                                                <div className="text-xs font-semibold text-muted-foreground">
                                                                    {t('upstream_manager.bound_usage_rules')}
                                                                </div>
                                                                {parsedUsageRules.length === 0 ? (
                                                                    <div className="rounded-lg border border-dashed border-input bg-background px-3 py-4 text-xs text-muted-foreground">
                                                                        {t('upstream_manager.no_usage_rules')}
                                                                    </div>
                                                                ) : (
                                                                    <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                                                                        {parsedUsageRules.map((ruleName, ruleIndex) => {
                                                                            const rule = usageRuleObjects[ruleIndex]
                                                                            const ruleEnabled = getOverrideRuleEnabled(rule)
                                                                            const checked = editingUpstream.usageRuleNames.includes(ruleName)
                                                                            return (
                                                                                <label
                                                                                    key={`${ruleName}-${ruleIndex}`}
                                                                                    className={cn(
                                                                                        "flex items-center gap-2 rounded-lg border border-input bg-background px-3 py-2 text-sm",
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
                                                                                        <Badge variant="outline" className="ml-auto h-5 shrink-0 rounded-md border-input bg-muted/50 px-1.5 text-xs text-muted-foreground">
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
                                            </AdvancedSettings>
                                        </div>

                                        {/* 不满足条件就不渲染:原来这两个按钮在只有一个预设时恒为
                                            disabled,是两个永远点不动的控件 */}
                                        {editingUpstream.usesTargetPresets && (
                                            editingUpstream.selectedTarget !== editingUpstream.activeTarget
                                            || Object.keys(editingUpstream.targets).length > 1
                                        ) && (
                                            <div className="flex flex-wrap items-center justify-end gap-2 border-t border-input pt-4">
                                                {editingUpstream.selectedTarget !== editingUpstream.activeTarget && (
                                                    <Button
                                                        type="button"
                                                        variant="outline"
                                                        size="sm"
                                                        className="h-8"
                                                        onClick={() => setEditingUpstream(current => current ? { ...commitSelectedTarget(current), activeTarget: current.selectedTarget } : current)}
                                                    >
                                                        {t('upstream_manager.set_active_target')}
                                                    </Button>
                                                )}
                                                {Object.keys(editingUpstream.targets).length > 1 && (
                                                    <Button
                                                        type="button"
                                                        variant="ghost"
                                                        size="sm"
                                                        className="h-8 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                                        onClick={handleRemoveTargetPreset}
                                                    >
                                                        <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                                                        {t('upstream_manager.remove_target')}
                                                    </Button>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                </section>
                            </div>

                            <div className="flex justify-end gap-2 border-t border-input bg-card px-6 py-4">
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

                <div className="relative z-10 w-full space-y-6 pb-16 animate-fade-in">

                    {/* Content Area */}
                    <div>
                        {activeTab === 'routing' && (
                            <div className="animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                {/* 这个 tab 的主体是一张八列数据表,不是表单,所以吃满外壳的 1600
                                    而不是收到 max-w-7xl —— 宽度跟着内容类型走 */}
                                <div className="w-full space-y-6">
                                    <SettingSection
                                        title={t('settings.access_title')}
                                        description={t('settings.access_description')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
                                            <FieldBlock
                                                label={t('settings.proxy_domain_suffix')}
                                                hint={t('settings.proxy_domain_suffix_hint', {
                                                    name: previewUpstreamName,
                                                    suffix: domainSuffix,
                                                })}
                                            >
                                                {/* 容器放宽后给输入框封顶:装 ".localhost" 的框子没必要跟着拉到 500px */}
                                                <Input
                                                    value={`.${domainSuffix}`}
                                                    readOnly
                                                    className="h-9 w-full max-w-md rounded-md border-input bg-background text-sm font-medium transition-colors cursor-default"
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
                                                    className="h-9 w-full max-w-md rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                        // 一屏只留一个实心强调色按钮,这里让位给底部的保存
                                        action={
                                            <Button
                                                type="button"
                                                variant="outline"
                                                onClick={() => setShowAddForm(prev => !prev)}
                                                size="sm"
                                                className="h-8 whitespace-nowrap border border-input bg-background font-medium text-foreground hover:bg-accent"
                                            >
                                                {!showAddForm && <Plus className="mr-1.5 h-4 w-4 shrink-0" />}
                                                {showAddForm ? t('common.cancel') : t('upstream_manager.add_new')}
                                            </Button>
                                        }
                                    >
                                    <div className="w-full">
                                        {showAddForm && (
                                            <div className="mb-8 w-full rounded-lg bg-background p-6 border border-border">
                                                <form onSubmit={handleAddUpstream} className="grid grid-cols-1 gap-6 md:grid-cols-12 md:items-end">
                                                    <div className="md:col-span-3">
                                                        <FieldBlock label={t('upstream_manager.name')} htmlFor="name">
                                                            <div className="relative">
                                                                <Input
                                                                    id="name"
                                                                    value={newName}
                                                                    onChange={e => setNewName(e.target.value)}
                                                                    placeholder="openai"
                                                                    className="h-9 rounded-md border-input bg-background pr-20 text-sm transition-colors focus-visible:bg-background"
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
                                                                className="h-9 rounded-md border-input bg-background font-mono text-sm transition-colors focus-visible:bg-background"
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
                                                                className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                                className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                                className="h-9 bg-background"
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
                                                                            className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                                            className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                                            className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                                                        />
                                                                    </FieldBlock>
                                                                </div>
                                                            </AdvancedSettingsGroup>

                                                            <AdvancedSettingsGroup title={t('upstream_manager.request_logging')} divider>
                                                                <ToggleSetting
                                                                    label={t('upstream_manager.logging_enabled')}
                                                                    description={t('upstream_manager.logging_enabled_hint')}
                                                                    checked={newLoggingEnabled}
                                                                    onCheckedChange={setNewLoggingEnabled}
                                                                />
                                                                <div className="space-y-2 border-l-2 border-border pl-4">
                                                                    <div>
                                                                        <div className="text-sm font-medium text-foreground">
                                                                            {t('upstream_manager.logging_path_mode')}
                                                                        </div>
                                                                        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                                                                            {t('upstream_manager.logging_path_mode_hint')}
                                                                        </p>
                                                                    </div>
                                                                    <UpstreamLoggingPathFilter
                                                                        value={newLoggingPathFilter}
                                                                        onChange={setNewLoggingPathFilter}
                                                                        templates={modelPathTemplates}
                                                                        disabled={!newLoggingEnabled}
                                                                    />
                                                                </div>
                                                            </AdvancedSettingsGroup>
                                                        </AdvancedSettings>
                                                    </div>

                                                    <div className="flex justify-end md:col-span-12">
                                                        <Button type="submit" variant="default" size="lg" className="h-9 rounded-md min-w-[120px] font-medium whitespace-nowrap shrink-0">
                                                            <Save className="mr-1.5 h-4 w-4 shrink-0" />
                                                            {t('common.save')}
                                                        </Button>
                                                    </div>
                                                </form>
                                            </div>
                                        )}

                                        {upstreams.length === 0 ? (
                                            <div className="rounded-lg border border-dashed border-input bg-muted/10 px-6 py-20 text-center">
                                                <Upload className="mx-auto mb-4 h-9 w-10 text-muted-foreground/30" />
                                                <p className="text-sm text-foreground/75">
                                                    {t('upstream_manager.no_upstreams')}
                                                </p>
                                            </div>
                                        ) : (
                                            <Table className="table-fixed">
                                                <TableHeader className="bg-muted">
                                                    <TableRow>
                                                        {/* 列宽按最长的真实值定,留给英文界面余量:英文标签普遍比中文长
                                                            一倍,标记列 250px 才装得下 "Request Override ×1 · Pass-through" */}
                                                        <TableHead className="w-[120px]">{t('upstream_manager.name')}</TableHead>
                                                        <TableHead className="w-[280px]">{t('upstream_manager.list_entry')}</TableHead>
                                                        <TableHead className="w-[130px]">{t('upstream_manager.target_presets')}</TableHead>
                                                        <TableHead>{t('upstream_manager.target')}</TableHead>
                                                        <TableHead className="w-[200px]">{t('upstream_manager.outbound_proxy')}</TableHead>
                                                        <TableHead className="w-[80px]">{t('upstream_manager.list_timeout')}</TableHead>
                                                        <TableHead className="w-[250px]">{t('upstream_manager.list_flags')}</TableHead>
                                                        <TableHead className="w-[76px]">{t('upstream_manager.actions')}</TableHead>
                                                    </TableRow>
                                                </TableHeader>
                                                <TableBody>
                                                    {sortedUpstreams.map(upstream => {
                                                        const overrideConfig = activeTargetConfig(upstream)?.request_overrides || overrideBindings[upstream.name]
                                                        const overrideEnabled = activeTargetConfig(upstream)?.request_overrides?.enabled || overrideBindings[upstream.name]?.enabled
                                                        const overrideRuleCount = getBindingRuleNames(overrideConfig).length
                                                        const targetNames = upstream.targets ? Object.keys(upstream.targets) : []
                                                        const activeTarget = upstream.active_target || targetNames[0]
                                                        const proxyIsCustom = outboundProxyMode(upstream.outbound_proxy) === 'custom'
                                                        const flagLabels: string[] = []
                                                        if (overrideEnabled) {
                                                            flagLabels.push(t('log_detail.request_override') + (overrideRuleCount ? ' ×' + overrideRuleCount : ''))
                                                        }
                                                        if (upstream.logging_enabled === false) {
                                                            flagLabels.push(t('upstream_manager.logging_disabled_badge'))
                                                        } else if (upstream.logging_path_filter && upstream.logging_path_filter.mode !== 'all') {
                                                            flagLabels.push(t(`upstream_manager.logging_mode_${upstream.logging_path_filter.mode}`) + ` ×${upstream.logging_path_filter.rules?.length || 0}`)
                                                        }

                                                        return (
                                                            <TableRow key={upstream.name}>
                                                                <TableCell className="font-medium text-foreground">
                                                                    <div className="truncate" title={upstream.name}>{upstream.name}</div>
                                                                </TableCell>

                                                                {/* 路径路由前缀是全局设置,每行都是同一个值;右对齐让它自成一列结构,
                                                                    而不是跟着入口地址的长度左右漂 */}
                                                                <TableCell>
                                                                    <div className="flex items-center gap-2">
                                                                        <button
                                                                            type="button"
                                                                            onClick={() => handleCopy(getProxyUrl(upstream.name))}
                                                                            className="flex min-w-0 flex-1 items-center gap-1.5 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
                                                                            title={getProxyUrl(upstream.name)}
                                                                        >
                                                                            <Copy className="h-3.5 w-3.5 shrink-0 opacity-40" />
                                                                            {/* 只显示 host:协议恒等于面板自身的 origin,是常量;复制出去仍是完整 URL */}
                                                                            <span className="min-w-0 truncate font-mono">{getProxyHost(upstream.name)}</span>
                                                                        </button>
                                                                        {enablePathRouting && (
                                                                            <Tooltip>
                                                                                <TooltipTrigger asChild>
                                                                                    <button
                                                                                        type="button"
                                                                                        onClick={() => handleCopy(getPathProxyUrl(upstream.name))}
                                                                                        className="shrink-0 rounded-md p-1 text-muted-foreground/70 transition-colors hover:bg-accent hover:text-foreground"
                                                                                        aria-label={t('settings.copy_path_proxy_url')}
                                                                                    >
                                                                                        <Route className="h-3.5 w-3.5" />
                                                                                    </button>
                                                                                </TooltipTrigger>
                                                                                {/* tooltip 同时说明了「这是什么」和「点了会怎样」:
                                                                                    图标只对动作成立,状态标签必须留文字 */}
                                                                                <TooltipContent>{t('settings.copy_path_proxy_url')}</TooltipContent>
                                                                            </Tooltip>
                                                                        )}
                                                                    </div>
                                                                </TableCell>

                                                                {/* 静止时是一段标签,hover 才浮出边框告诉你可以点 ——
                                                                    表格主体全是只读文本,一个实心表单控件会独自抢走视线 */}
                                                                <TableCell>
                                                                    {targetNames.length > 1 && activeTarget ? (
                                                                        <Select
                                                                            value={activeTarget}
                                                                            onValueChange={value => handleActivateTarget(upstream.name, value)}
                                                                            disabled={switchingTarget === upstream.name}
                                                                        >
                                                                            <SelectTrigger className="h-6 w-full gap-1 border-transparent bg-transparent px-1.5 text-xs text-muted-foreground shadow-none hover:border-input hover:bg-background hover:text-foreground data-[state=open]:border-input data-[state=open]:bg-background dark:bg-transparent dark:hover:bg-background">
                                                                                <SelectValue />
                                                                            </SelectTrigger>
                                                                            <SelectContent>
                                                                                {targetNames.map(name => (
                                                                                    <SelectItem key={name} value={name}>{name}</SelectItem>
                                                                                ))}
                                                                            </SelectContent>
                                                                        </Select>
                                                                    ) : (
                                                                        <div className="truncate px-1.5 text-xs text-muted-foreground">
                                                                            {activeTarget || <span className="opacity-50">—</span>}
                                                                        </div>
                                                                    )}
                                                                </TableCell>

                                                                <TableCell>
                                                                    <button
                                                                        type="button"
                                                                        onClick={() => handleCopy(upstream.target)}
                                                                        className="flex w-full min-w-0 items-center gap-1.5 text-left text-xs text-foreground transition-colors hover:text-primary"
                                                                        title={upstream.target}
                                                                    >
                                                                        <Copy className="h-3.5 w-3.5 shrink-0 opacity-40" />
                                                                        {/* 目标地址保留 scheme:那是填进去的、会变的(有的上游是 http) */}
                                                                        <span className="min-w-0 truncate font-mono">{upstream.target}</span>
                                                                    </button>
                                                                </TableCell>

                                                                <TableCell className="text-xs text-muted-foreground">
                                                                    <div
                                                                        className={cn('truncate', proxyIsCustom && 'font-mono')}
                                                                        title={proxyIsCustom ? upstream.outbound_proxy : undefined}
                                                                    >
                                                                        {formatOutboundProxy(upstream.outbound_proxy)}
                                                                    </div>
                                                                </TableCell>

                                                                <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                                                                    {upstream.timeout}s
                                                                </TableCell>

                                                                {/* badge 数量是会变的,给它专属一列 —— 挂在名称后面会把后面每一列
                                                                    的起点推到每行不同的位置,那正是原来看着乱的原因。
                                                                    不用药丸:列边界已经把它框住了,再加边框和底色是重复的 */}
                                                                <TableCell className="text-xs text-muted-foreground">
                                                                    <div className="truncate" title={flagLabels.join(' · ') || undefined}>
                                                                        {flagLabels.length ? flagLabels.join(' · ') : <span className="opacity-50">—</span>}
                                                                    </div>
                                                                </TableCell>

                                                                {/* 常驻而不是 hover 才出现:右边缘每行都有内容,这一列才立得住 */}
                                                                <TableCell>
                                                                    <div className="flex items-center gap-0.5">
                                                                        <Tooltip>
                                                                            <TooltipTrigger asChild>
                                                                                <Button
                                                                                    type="button"
                                                                                    variant="ghost"
                                                                                    size="icon"
                                                                                    onClick={() => handleEditUpstream(upstream)}
                                                                                    className="h-7 w-7 text-muted-foreground/70 hover:bg-accent hover:text-foreground"
                                                                                    aria-label={t('common.edit')}
                                                                                >
                                                                                    <Pencil className="h-3.5 w-3.5" />
                                                                                </Button>
                                                                            </TooltipTrigger>
                                                                            <TooltipContent>{t('common.edit')}</TooltipContent>
                                                                        </Tooltip>
                                                                        <Tooltip>
                                                                            <TooltipTrigger asChild>
                                                                                <Button
                                                                                    type="button"
                                                                                    variant="ghost"
                                                                                    size="icon"
                                                                                    onClick={() => handleRemoveUpstream(upstream.name)}
                                                                                    className="h-7 w-7 text-muted-foreground/70 hover:bg-destructive/10 hover:text-destructive"
                                                                                    aria-label={t('common.delete')}
                                                                                >
                                                                                    <Trash2 className="h-3.5 w-3.5" />
                                                                                </Button>
                                                                            </TooltipTrigger>
                                                                            <TooltipContent>{t('common.delete')}</TooltipContent>
                                                                        </Tooltip>
                                                                    </div>
                                                                </TableCell>
                                                            </TableRow>
                                                        )
                                                    })}
                                                </TableBody>
                                            </Table>
                                        )}
                                    </div>
                                    </SettingSection>
                                </div>
                            </div>
                        )}

                        {activeTab === 'logging' && (
                            <div className="animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                <div className="mx-auto max-w-7xl space-y-6">
                                    <Tabs
                                        value={loggingSection}
                                        onValueChange={value => navigate(`/settings/logging/${value}`)}
                                    >
                                        <TabsList>
                                            <TabsTrigger value="general">{t('logging_rules.tabs.general')}</TabsTrigger>
                                            <TabsTrigger value="models">{t('logging_rules.tabs.models')}</TabsTrigger>
                                            <TabsTrigger value="ignored">{t('logging_rules.tabs.ignored')}</TabsTrigger>
                                        </TabsList>
                                    </Tabs>

                                    {loggingSection === 'general' ? (
                                    <>
                                    <SettingSection
                                        title={t('settings.section_content_size')}
                                        description={t('settings.section_content_size_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2">
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
                                                    className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                    className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.body_storage_title')}
                                        description={t('settings.body_storage_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2">
                                            <FieldBlock label={t('settings.body_compression_algorithm')} htmlFor="body-compression">
                                                <Select value={bodyCompressionAlgorithm} onValueChange={value => setBodyCompressionAlgorithm(value as 'zstd' | 'none')}>
                                                    <SelectTrigger id="body-compression" className="h-9 w-full rounded-md border-input bg-background">
                                                        <SelectValue />
                                                    </SelectTrigger>
                                                    <SelectContent>
                                                        <SelectItem value="zstd">Zstd</SelectItem>
                                                        <SelectItem value="none">{t('settings.body_compression_none')}</SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.body_compression_level')} hint={t('settings.body_compression_level_hint')} htmlFor="body-compression-level">
                                                <Input
                                                    id="body-compression-level"
                                                    type="number"
                                                    min="1"
                                                    max="19"
                                                    value={bodyCompressionLevel}
                                                    disabled={bodyCompressionAlgorithm === 'none'}
                                                    onChange={e => setBodyCompressionLevel(Number(e.target.value))}
                                                    className="h-9 w-full rounded-md border-input bg-background text-sm"
                                                />
                                            </FieldBlock>
                                        </div>
                                        <div className="mt-5 grid grid-cols-1 gap-3 border-t border-input pt-5 sm:grid-cols-3">
                                            <div><div className="text-xs text-muted-foreground">{t('settings.storage_usage_database')}</div><div className="mt-1 font-mono text-sm">{formatBytes(storageUsage?.database_bytes)}</div></div>
                                            <div><div className="text-xs text-muted-foreground">{t('settings.storage_usage_blobs')}</div><div className="mt-1 font-mono text-sm">{formatBytes(storageUsage?.blob_bytes)}</div></div>
                                            <div className="flex items-end"><Button type="button" variant="outline" size="sm" onClick={handleCalculateStorage} disabled={storageLoading}><RefreshCw className={cn('mr-2 h-3.5 w-3.5', storageLoading && 'animate-spin')} />{t('settings.storage_usage_calculate')}</Button></div>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.section_retention')}
                                        description={t('settings.section_retention_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
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
                                                    className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                    className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.archive_title')}
                                        description={t('settings.archive_desc')}
                                        action={<Switch checked={archiveEnabled} onCheckedChange={setArchiveEnabled} aria-label={t('settings.archive_enabled')} />}
                                    >
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
                                            <FieldBlock label={t('settings.archive_endpoint')} hint={t('settings.archive_endpoint_hint')} htmlFor="archive-endpoint">
                                                <Input id="archive-endpoint" value={archiveEndpoint} onChange={e => setArchiveEndpoint(e.target.value)} placeholder="https://s3.example.com" className="h-9 font-mono text-xs" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_region')} htmlFor="archive-region">
                                                <Input id="archive-region" value={archiveRegion} onChange={e => setArchiveRegion(e.target.value)} placeholder="cn-northwest-1" className="h-9 font-mono text-xs" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_bucket')} htmlFor="archive-bucket">
                                                <Input id="archive-bucket" value={archiveBucket} onChange={e => setArchiveBucket(e.target.value)} className="h-9 font-mono text-xs" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_access_key')} htmlFor="archive-access-key">
                                                <Input id="archive-access-key" value={archiveAccessKeyID} onChange={e => setArchiveAccessKeyID(e.target.value)} autoComplete="off" className="h-9 font-mono text-xs" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_secret_key')} hint={archiveSecretConfigured ? t('settings.archive_secret_configured') : undefined} htmlFor="archive-secret-key">
                                                <Input id="archive-secret-key" type="password" value={archiveSecretAccessKey} onChange={e => setArchiveSecretAccessKey(e.target.value)} autoComplete="new-password" placeholder={archiveSecretConfigured ? t('settings.archive_secret_unchanged') : ''} className="h-9 font-mono text-xs" />
                                            </FieldBlock>
                                            <div className="flex min-h-16 items-center justify-between gap-4 border-b border-input pb-3">
                                                <div><Label>{t('settings.archive_force_path_style')}</Label><p className="mt-1 text-xs text-muted-foreground">{t('settings.archive_force_path_style_hint')}</p></div>
                                                <Switch checked={archiveForcePathStyle} onCheckedChange={setArchiveForcePathStyle} />
                                            </div>
                                            <div className="sm:col-span-2 lg:col-span-3">
                                                <FieldBlock label={t('settings.archive_key_prefix')} htmlFor="archive-prefix">
                                                    <Input ref={archiveKeyInputRef} id="archive-prefix" value={archiveKeyPrefix} onChange={e => setArchiveKeyPrefix(e.target.value)} placeholder="backups/prismcat/${yyyy}/${MM}-${dd}" aria-invalid={Boolean(archiveKeyError)} className={cn('h-9 font-mono text-xs', archiveKeyError && 'border-destructive')} />
                                                </FieldBlock>
                                                <div className="mt-2 space-y-1.5 text-xs text-muted-foreground">
                                                    <p>{t('settings.archive_key_tokens')}</p>
                                                    {[
                                                        { token: '${yyyy}', description: t('settings.archive_key_token_year') },
                                                        { token: '${MM}', description: t('settings.archive_key_token_month') },
                                                        { token: '${dd}', description: t('settings.archive_key_token_day') },
                                                    ].map(item => (
                                                        <div key={item.token} className="flex min-h-7 flex-wrap items-center gap-2">
                                                            <Button type="button" variant="outline" size="sm" className="h-7 w-20 px-2 font-mono text-xs" onClick={() => insertArchivePlaceholder(item.token)}>{item.token}</Button>
                                                            <span>{item.description}</span>
                                                        </div>
                                                    ))}
                                                </div>
                                                <p className={cn('mt-2 break-all font-mono text-xs', archiveKeyError ? 'text-destructive' : 'text-muted-foreground')}>
                                                    {archiveKeyError ? t(`settings.archive_key_error_${archiveKeyError}`) : `${t('settings.archive_key_sample', { date: archiveKeySample.date })}: ${archiveKeySample.key}`}
                                                </p>
                                            </div>
                                            <FieldBlock label={t('settings.archive_schedule_time')} htmlFor="archive-time">
                                                <Input id="archive-time" type="time" value={archiveScheduleTime} onChange={e => setArchiveScheduleTime(e.target.value)} className="h-9" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_timezone')} htmlFor="archive-timezone">
                                                <Input id="archive-timezone" value={archiveTimezone} onChange={e => setArchiveTimezone(e.target.value)} className="h-9 font-mono text-xs" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_zstd_level')} hint={t('settings.archive_zstd_level_hint')} htmlFor="archive-level">
                                                <Input id="archive-level" type="number" min="1" max="19" value={archiveZstdLevel} onChange={e => setArchiveZstdLevel(Number(e.target.value))} className="h-9" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_local_retention')} hint={t('settings.archive_local_retention_hint', { count: archiveDeletionPreview })} unit={t('settings.hours')} htmlFor="archive-local-retention">
                                                <Input id="archive-local-retention" type="number" min="1" value={archiveLocalRetentionHours} onChange={e => setArchiveLocalRetentionHours(Number(e.target.value))} className="h-9" />
                                            </FieldBlock>
                                            <FieldBlock label={t('settings.archive_import_retention')} unit={t('settings.hours')} htmlFor="archive-retention">
                                                <Input id="archive-retention" type="number" min="0" value={archiveImportRetentionHours} onChange={e => setArchiveImportRetentionHours(Number(e.target.value))} className="h-9" />
                                            </FieldBlock>
                                        </div>
                                        <div className="mt-5 flex flex-wrap items-center gap-2 border-t border-input pt-5">
                                            <Button type="button" variant="outline" size="sm" onClick={handleArchiveTest} disabled={archiveAction !== '' || Boolean(archiveKeyError) || !archiveS3Ready}>
                                                <RefreshCw className={cn('mr-2 h-3.5 w-3.5', archiveAction === 'test' && 'animate-spin')} />{t('settings.archive_test')}
                                            </Button>
                                            <Button type="button" variant="outline" size="sm" onClick={handleArchiveRun} disabled={archiveAction !== '' || !archiveEnabled || Boolean(archiveKeyError) || !archiveS3Ready}>
                                                <Upload className="mr-2 h-3.5 w-3.5" />{t('settings.archive_run_now')}
                                            </Button>
                                            <span className="text-xs text-muted-foreground">
                                                {archiveStatus?.running
                                                    ? t('settings.archive_running')
                                                    : archiveStatus?.jobs?.[0]
                                                        ? `${t(`archives.status_${archiveStatus.jobs[0].status}`, archiveStatus.jobs[0].status)} · ${archiveStatus.jobs[0].log_count} ${t('archives.logs')}${archiveStatus.jobs[0].error ? ` · ${archiveStatus.jobs[0].error}` : ''}`
                                                        : t('settings.archive_never_run')}
                                            </span>
                                            <span className="text-xs text-muted-foreground">
                                                {t('settings.archive_pending_delete', { count: archiveStatus?.pending_delete_count ?? 0 })}
                                                {archiveStatus?.earliest_delete_at
                                                    ? ` · ${t('settings.archive_earliest_delete', { time: new Date(archiveStatus.earliest_delete_at).toLocaleString() })}`
                                                    : ''}
                                            </span>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.section_logging_behavior')}
                                        description={t('settings.section_logging_behavior_desc')}
                                    >
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2">
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
                                            className="min-h-[112px] w-full rounded-md border-input bg-background font-mono text-sm leading-relaxed transition-colors focus-visible:bg-background resize-y"
                                            placeholder="Authorization&#10;x-api-key&#10;api-key"
                                        />
                                    </SettingSection>
                                    </>
                                    ) : (
                                        <LoggingRules tab={loggingSection} embedded />
                                    )}
                                </div>
                            </div>
                        )}

                        {activeTab === 'system' && (
                            <div className="mx-auto max-w-7xl space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
                                {metricsError && (
                                    <div className="flex items-start gap-3 rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                        <span>{metricsError}</span>
                                    </div>
                                )}

                                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                                    <MetricCard
                                        label={t('settings.system_process_memory')}
                                        value={formatBytes(metrics?.process.rss_bytes)}
                                        detail={t('settings.system_heap_detail', {
                                            alloc: formatBytes(metrics?.process.heap_alloc_bytes),
                                            sys: formatBytes(metrics?.process.heap_sys_bytes),
                                        })}
                                    />
                                    <MetricCard
                                        label={t('settings.system_process_cpu')}
                                        value={formatPercent(metrics?.process.cpu_percent)}
                                        detail={t('settings.system_cpu_detail', {
                                            seconds: metrics?.process.cpu_seconds?.toFixed(1) ?? '-',
                                        })}
                                    />
                                    <MetricCard
                                        label={t('settings.system_total_memory')}
                                        value={formatPercent(memoryUsedPercent)}
                                        detail={t('settings.system_memory_detail', {
                                            used: formatBytes(metrics?.memory.used_bytes),
                                            total: formatBytes(metrics?.memory.total_bytes),
                                            source: metrics?.memory.source || '-',
                                        })}
                                    />
                                    <MetricCard
                                        label={t('settings.system_uptime')}
                                        value={formatDuration(metrics?.runtime.uptime_seconds)}
                                        detail={t('settings.system_runtime_detail', {
                                            goroutines: metrics?.runtime.goroutines ?? '-',
                                            cpu: metrics?.runtime.num_cpu ?? '-',
                                        })}
                                    />
                                </div>

                                <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                                    <div className="rounded-md border border-input bg-background px-5 py-4">
                                        <div className="mb-3 text-xs font-semibold text-muted-foreground">
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
                                    <div className="rounded-md border border-input bg-background px-5 py-4">
                                        <div className="mb-3 flex items-center justify-between gap-3">
                                            <div className="text-xs font-semibold text-muted-foreground">
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

                                <div className="rounded-md border border-input bg-background px-5 py-4">
                                    <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                                        <div>
                                            <div className="text-xs font-semibold text-muted-foreground">
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
                                            className="h-9 rounded-md"
                                        >
                                            <RefreshCw className={cn("mr-2 h-4 w-4", storageLoading && "animate-spin")} />
                                            {t('settings.storage_usage_calculate')}
                                        </Button>
                                    </div>

                                    {storageError && (
                                        <div className="mb-4 flex items-start gap-3 rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                            <span>{storageError}</span>
                                        </div>
                                    )}

                                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                                        <div className="rounded-lg border border-input bg-background px-4 py-3">
                                            <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
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
                                        <div className="rounded-lg border border-input bg-background px-4 py-3">
                                            <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
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
                                        <div className="rounded-lg border border-input bg-background px-4 py-3">
                                            <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
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

                                <div className="rounded-md border border-input bg-background px-5 py-4">
                                    <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                                        <div>
                                            <div className="text-xs font-semibold text-muted-foreground">
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
                                            className="h-9 rounded-md"
                                        >
                                            <RefreshCw className={cn("mr-2 h-4 w-4", updateLoading && "animate-spin")} />
                                            {t('settings.update_check')}
                                        </Button>
                                    </div>

                                    {updateError && (
                                        <div className="mb-4 flex items-start gap-3 rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
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
                                                        "rounded-md px-3 py-1 text-xs font-semibold",
                                                        updateInfo.update_available
                                                            ? "border-warning/30 bg-warning/10 text-warning"
                                                            : "border-success/30 bg-success/10 text-success"
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
                                                        <Button asChild className="h-9 rounded-md">
                                                            <a href={updateInfo.matching_asset.download_url} target="_blank" rel="noreferrer noopener">
                                                                <Download className="mr-2 h-4 w-4" />
                                                                {t('settings.update_download_asset', {
                                                                    size: formatBytes(updateInfo.matching_asset.size),
                                                                })}
                                                            </a>
                                                        </Button>
                                                    )}
                                                    <Button asChild variant="outline" className="h-9 rounded-md">
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
                                <div className="mx-auto max-w-7xl space-y-6">
                                    <div className="inline-flex rounded-md border border-input bg-muted/30 p-1">
                                        <button
                                            type="button"
                                            onClick={() => setActiveRuleTab('request_overrides')}
                                            className={cn(
                                                "rounded-lg px-4 py-2 text-sm font-semibold transition-colors",
                                                activeRuleTab === 'request_overrides'
                                                    ? "bg-background text-foreground"
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
                                                    ? "bg-background text-foreground"
                                                    : "text-muted-foreground hover:text-foreground"
                                            )}
                                        >
                                            {t('settings.rule_tab_usage_extraction')}
                                        </button>
                                    </div>

                                    {activeRuleTab === 'request_overrides' && (
                                        <>
                                    <div className="flex items-start gap-3 rounded-md border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-warning">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                        <div className="space-y-1">
                                            <p className="font-semibold">{t('settings.request_overrides_warning_title')}</p>
                                            <p className="text-xs leading-6 opacity-90">{t('settings.request_overrides_warning')}</p>
                                        </div>
                                    </div>

                                    <SettingSection title={t('settings.request_overrides_config')}>
                                        <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
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
                                                    className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                                />
                                            </FieldBlock>
                                        </div>
                                    </SettingSection>

                                    <SettingSection
                                        title={t('settings.request_overrides_rules')}
                                        description={t('settings.request_overrides_rules_hint')}
                                    >
                                        <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-input bg-muted/30 px-3 py-2">
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
                                                        href={t('settings.request_overrides_docs_url')}
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
                                                    onClick={handleAddOverrideExample}
                                                    disabled={Boolean(overrideRulesParse.error)}
                                                    className="h-8 text-xs font-semibold"
                                                >
                                                    <Plus className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('settings.request_overrides_insert_example')}
                                                </Button>
                                            </div>
                                        </div>
                                        <div className="grid gap-4 xl:grid-cols-[minmax(280px,0.32fr)_minmax(0,1fr)]">
                                            <div className="rounded-md border border-input bg-background">
                                                <div className="flex items-center justify-between gap-2 border-b border-input px-3 py-2">
                                                    <span className="text-xs font-semibold text-muted-foreground">
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
                                                    <div className="px-3 py-8 text-center text-xs text-danger">
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
                                                            const status = getRuleRuntimeStatus(rule, upstreams, overrideBindings, requestOverridesEnabled)
                                                            const statusBadgeClass = cn(
                                                                "h-5 rounded-md px-2 text-xs font-semibold",
                                                                status.kind === 'active' && "border-success/40 bg-success/10 text-success",
                                                                status.kind === 'blocked' && "border-warning/40 bg-warning/10 text-warning",
                                                                status.kind === 'inactive' && "border-warning/40 bg-warning/10 text-warning",
                                                                status.kind === 'unbound' && "border-input bg-muted/50 text-muted-foreground",
                                                            )
                                                            const statusText =
                                                                status.kind === 'active'
                                                                    ? t('settings.rule_status_active')
                                                                    : status.kind === 'unbound'
                                                                        ? t('settings.rule_status_unbound')
                                                                        : status.kind === 'inactive'
                                                                            ? t('settings.rule_status_inactive')
                                                                        : status.reason === 'global'
                                                                            ? t('settings.rule_status_blocked_global')
                                                                            : status.reason === 'rule'
                                                                                ? t('settings.rule_status_blocked_rule')
                                                                                : t('settings.rule_status_blocked_bindings')
                                                            const detail =
                                                                    status.kind === 'active'
                                                                    ? [
                                                                        formatUpstreamList(status.enabledUpstreams, status.disabledUpstreams, t),
                                                                        formatInactiveTargets(status.inactiveTargets, t),
                                                                    ].filter(Boolean).join('  ·  ')
                                                                    : status.kind === 'blocked'
                                                                        ? [
                                                                            formatUpstreamList(status.enabledUpstreams, status.disabledUpstreams, t),
                                                                            formatInactiveTargets(status.inactiveTargets, t),
                                                                        ].filter(Boolean).join('  ·  ')
                                                                        : status.kind === 'inactive'
                                                                            ? formatInactiveTargets(status.inactiveTargets, t)
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
                                                                                <span className="min-w-0 truncate text-xs text-muted-foreground">
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

                                            <div className="rounded-md border border-input bg-background">
                                                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-input px-3 py-2">
                                                    <div className="min-w-0">
                                                        <div className="truncate text-sm font-semibold text-foreground">
                                                            {overrideRuleObjects[selectedOverrideRuleIndex]
                                                                ? getOverrideRuleName(overrideRuleObjects[selectedOverrideRuleIndex], `rule-${selectedOverrideRuleIndex + 1}`)
                                                                : t('settings.request_override_no_rule_selected')}
                                                        </div>
                                                        <div className="text-xs text-muted-foreground">
                                                            {t('settings.request_override_single_rule_hint')}
                                                        </div>
                                                    </div>
                                                    {overrideRuleObjects[selectedOverrideRuleIndex] && (
                                                        <div className="flex shrink-0 items-center gap-1">
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
                                                                className="h-8 w-8 text-muted-foreground hover:text-danger"
                                                                aria-label={t('common.delete')}
                                                            >
                                                                <Trash2 className="h-3.5 w-3.5" />
                                                            </Button>
                                                        </div>
                                                    )}
                                                </div>
                                                {overrideRuleObjects[selectedOverrideRuleIndex] ? (
                                                    <div className="space-y-4 p-3">
                                                        <div className="grid gap-4 rounded-md border border-input bg-muted/20 p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
                                                            <FieldBlock label={t('settings.request_override_rule_name')}>
                                                                <Input
                                                                    value={selectedOverrideRuleName}
                                                                    onChange={event => handleOverrideRuleNameChange(event.target.value)}
                                                                    onBlur={handleCommitOverrideRuleName}
                                                                    onKeyDown={event => {
                                                                        if (event.key === 'Enter') event.currentTarget.blur()
                                                                    }}
                                                                    className="h-9 rounded-md border-input bg-background font-mono text-sm"
                                                                />
                                                                {selectedOverrideRuleNameError && (
                                                                    <div className="text-xs text-danger">
                                                                        {selectedOverrideRuleNameError}
                                                                    </div>
                                                                )}
                                                            </FieldBlock>
                                                            <div className="flex h-9 items-center gap-2 rounded-md border border-input bg-background px-3">
                                                                <Switch
                                                                    id="selected-override-rule-enabled"
                                                                    checked={getOverrideRuleEnabled(overrideRuleObjects[selectedOverrideRuleIndex])}
                                                                    onCheckedChange={checked => handleToggleOverrideRule(selectedOverrideRuleIndex, checked)}
                                                                    className="shrink-0 data-[state=unchecked]:bg-border/60"
                                                                />
                                                                <Label htmlFor="selected-override-rule-enabled" className="cursor-pointer text-sm font-medium">
                                                                    {t('settings.rule_enabled')}
                                                                </Label>
                                                            </div>
                                                        </div>

                                                        <div className="grid gap-4 lg:grid-cols-2">
                                                            <div className="space-y-3 rounded-md border border-input bg-muted/20 p-4">
                                                                <div>
                                                                    <Label className="text-xs font-semibold text-muted-foreground">
                                                                        {t('settings.request_override_match')}
                                                                    </Label>
                                                                    <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                                                        {t('settings.request_override_match_hint')}
                                                                    </p>
                                                                </div>
                                                                <Textarea
                                                                    value={selectedOverrideMatchText}
                                                                    onChange={event => handleOverrideMatchTextChange(event.target.value)}
                                                                    rows={9}
                                                                    spellCheck={false}
                                                                    className="min-h-[220px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                                                />
                                                                {selectedOverrideMatchError && (
                                                                    <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                                        {selectedOverrideMatchError}
                                                                    </div>
                                                                )}
                                                            </div>

                                                            <div className="space-y-3 rounded-md border border-input bg-muted/20 p-4">
                                                                <div>
                                                                    <Label className="text-xs font-semibold text-muted-foreground">
                                                                        {t('settings.request_override_body_patch')}
                                                                    </Label>
                                                                    <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                                                        {t('settings.request_override_body_patch_hint')}
                                                                    </p>
                                                                </div>
                                                                <Textarea
                                                                    value={selectedOverridePatchText}
                                                                    onChange={event => handleOverridePatchTextChange(event.target.value)}
                                                                    rows={9}
                                                                    spellCheck={false}
                                                                    className="min-h-[220px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                                                />
                                                                {selectedOverridePatchError && (
                                                                    <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                                        {selectedOverridePatchError}
                                                                    </div>
                                                                )}
                                                            </div>
                                                        </div>

                                                        <div className="space-y-3 rounded-md border border-input bg-muted/20 p-4">
                                                            <div className="flex items-start justify-between gap-3">
                                                                <div>
                                                                    <Label className="text-xs font-semibold text-muted-foreground">
                                                                        {t('settings.request_override_headers')}
                                                                    </Label>
                                                                    <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                                                        {t('settings.request_override_headers_hint')}
                                                                    </p>
                                                                </div>
                                                                <Button
                                                                    type="button"
                                                                    variant="ghost"
                                                                    size="sm"
                                                                    onClick={handleAddHeaderOp}
                                                                    className="h-7 shrink-0 px-2 text-xs"
                                                                >
                                                                    <Plus className="mr-1 h-3.5 w-3.5" />
                                                                    {t('common.add')}
                                                                </Button>
                                                            </div>
                                                            {getSelectedRuleHeaders().length > 0 ? (
                                                                <div className="space-y-2">
                                                                    {getSelectedRuleHeaders().map((header, hIdx) => (
                                                                        <div key={hIdx} className="grid grid-cols-1 items-center gap-2 sm:grid-cols-[100px_minmax(140px,1fr)_minmax(180px,2fr)_32px]">
                                                                            <Select
                                                                                value={header.op}
                                                                                onValueChange={value => handleUpdateHeaderOp(hIdx, 'op', value)}
                                                                            >
                                                                                <SelectTrigger className="h-9 w-full rounded-lg border-input bg-background text-xs">
                                                                                    <SelectValue />
                                                                                </SelectTrigger>
                                                                                <SelectContent>
                                                                                    <SelectItem value="set">{t('settings.header_op_set')}</SelectItem>
                                                                                    <SelectItem value="remove">{t('settings.header_op_remove')}</SelectItem>
                                                                                </SelectContent>
                                                                            </Select>
                                                                            <Input
                                                                                value={header.name}
                                                                                onChange={event => handleUpdateHeaderOp(hIdx, 'name', event.target.value)}
                                                                                placeholder={t('settings.header_name_placeholder')}
                                                                                className="h-9 min-w-0 rounded-lg border-input bg-background text-xs"
                                                                            />
                                                                            <HeaderValueInput
                                                                                value={header.value ?? ''}
                                                                                onChange={value => handleUpdateHeaderOp(hIdx, 'value', value)}
                                                                                placeholder={header.op === 'remove' ? '—' : t('settings.header_value_placeholder')}
                                                                                disabled={header.op === 'remove'}
                                                                                sensitive={isSensitiveHeaderName(header.name, sensitiveHeaders)}
                                                                                showLabel={t('settings.show_sensitive_value')}
                                                                                hideLabel={t('settings.hide_sensitive_value')}
                                                                            />
                                                                            <Button
                                                                                type="button"
                                                                                variant="ghost"
                                                                                size="icon"
                                                                                onClick={() => handleRemoveHeaderOp(hIdx)}
                                                                                className="h-8 w-8 shrink-0 text-muted-foreground hover:text-danger"
                                                                            >
                                                                                <Trash2 className="h-3.5 w-3.5" />
                                                                            </Button>
                                                                        </div>
                                                                    ))}
                                                                </div>
                                                            ) : (
                                                                <div className="rounded-lg border border-dashed border-input px-3 py-4 text-center text-xs text-muted-foreground">
                                                                    {t('settings.request_override_no_headers')}
                                                                </div>
                                                            )}
                                                        </div>

                                                        <details className="group rounded-md border border-input bg-muted/20">
                                                            <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-xs font-semibold text-muted-foreground marker:content-none hover:text-foreground [&::-webkit-details-marker]:hidden">
                                                                <span>{t('settings.request_override_final_preview')}</span>
                                                                <ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
                                                            </summary>
                                                            <div className="space-y-2 border-t border-input p-3">
                                                                <p className="text-xs leading-5 text-muted-foreground">
                                                                    {t('settings.request_override_final_preview_hint')}
                                                                </p>
                                                                <Textarea
                                                                    value={selectedOverrideRulePreview}
                                                                    readOnly
                                                                    rows={12}
                                                                    spellCheck={false}
                                                                    className="min-h-[260px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                                                />
                                                                <div className="flex justify-end">
                                                                    <Button type="button" variant="outline" size="sm" onClick={() => handleCopy(selectedOverrideRulePreview)} className="h-8 text-xs">
                                                                        <Copy className="mr-1.5 h-3.5 w-3.5" />
                                                                        {t('common.copy')}
                                                                    </Button>
                                                                </div>
                                                            </div>
                                                        </details>

                                                        <div className="rounded-md border border-input bg-muted/10">
                                                            <button
                                                                type="button"
                                                                onClick={() => setSelectedRuleAdvancedOpen(open => !open)}
                                                                className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                                                            >
                                                                <span>{t('settings.request_override_raw_rule')}</span>
                                                                <span>{selectedRuleAdvancedOpen ? t('common.hide') : t('common.show')}</span>
                                                            </button>
                                                            {selectedRuleAdvancedOpen && (
                                                                <div className="space-y-2 border-t border-input p-3">
                                                                    <Textarea
                                                                        value={selectedOverrideRuleText}
                                                                        onChange={event => handleOverrideRuleTextChange(event.target.value)}
                                                                        rows={14}
                                                                        spellCheck={false}
                                                                        className="min-h-[320px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                                                    />
                                                                    {selectedOverrideRuleError && (
                                                                <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                                    {selectedOverrideRuleError}
                                                                </div>
                                                            )}
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

                                        <div className="mt-4 rounded-md border border-input bg-muted/20">
                                            <button
                                                type="button"
                                                onClick={() => setAdvancedRulesOpen(open => !open)}
                                                className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                                            >
                                                <span>{t('settings.request_override_advanced_json')}</span>
                                                <span>{advancedRulesOpen ? t('common.hide') : t('common.show')}</span>
                                            </button>
                                            {advancedRulesOpen && (
                                                <div className="border-t border-input p-3">
                                                    <Textarea
                                                        value={overrideRulesText}
                                                        onChange={e => setOverrideRulesText(e.target.value)}
                                                        rows={12}
                                                        spellCheck={false}
                                                        className="min-h-[260px] w-full rounded-lg border-input bg-background font-mono text-xs leading-relaxed transition-colors focus-visible:bg-background resize-y"
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
                                            <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-input bg-background px-3 py-2">
                                                <p className="text-xs leading-5 text-muted-foreground">
                                                    {t('settings.usage_extraction_scope_hint')}
                                                </p>
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    onClick={handleMergeDefaultUsageRules}
                                                    disabled={Boolean(usageRulesParse.error)}
                                                    className="h-8 text-xs font-semibold"
                                                >
                                                    <Plus className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('settings.usage_extraction_insert_example')}
                                                </Button>
                                            </div>

                                            <div className="grid gap-4 xl:grid-cols-[minmax(280px,0.32fr)_minmax(0,1fr)]">
                                                <div className="rounded-md border border-input bg-background">
                                                    <div className="flex items-center justify-between gap-2 border-b border-input px-3 py-2">
                                                        <span className="text-xs font-semibold text-muted-foreground">
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
                                                        <div className="px-3 py-8 text-center text-xs text-danger">
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
                                                                    + upstreams.reduce((count, upstream) => count + Object.values(upstream.targets || {}).filter(target => (
                                                                        target.usage_extraction && getBindingRuleNames(target.usage_extraction).includes(ruleName)
                                                                    )).length, 0)
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
                                                                                        "h-5 rounded-md px-1.5 text-xs",
                                                                                        getOverrideRuleEnabled(rule)
                                                                                            ? "border-success/30 bg-success/10 text-success"
                                                                                            : "border-input bg-muted/50 text-muted-foreground"
                                                                                    )}
                                                                                >
                                                                                    {getOverrideRuleEnabled(rule) ? t('settings.rule_enabled') : t('settings.rule_disabled')}
                                                                                </Badge>
                                                                                {boundCount > 0 && (
                                                                                    <Badge variant="outline" className="h-5 rounded-md border-input bg-muted/50 px-1.5 text-xs text-muted-foreground">
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

                                                <div className="rounded-md border border-input bg-background">
                                                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-input px-3 py-2">
                                                        <div className="min-w-0">
                                                            <div className="truncate text-sm font-semibold text-foreground">
                                                                {usageRuleObjects[selectedUsageRuleIndex]
                                                                    ? getOverrideRuleName(usageRuleObjects[selectedUsageRuleIndex], `usage-rule-${selectedUsageRuleIndex + 1}`)
                                                                    : t('settings.usage_extraction_no_rule_selected')}
                                                            </div>
                                                            <div className="text-xs text-muted-foreground">
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
                                                                    className="h-8 w-8 text-muted-foreground hover:text-danger"
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
                                                                className="min-h-[320px] w-full resize-y rounded-lg border-input bg-muted/20 font-mono text-xs leading-relaxed transition-colors focus-visible:bg-background"
                                                            />
                                                            {selectedUsageRuleError && (
                                                                <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
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

                                            <div className="mt-4 rounded-md border border-input bg-background">
                                                <button
                                                    type="button"
                                                    onClick={() => setUsageAdvancedRulesOpen(open => !open)}
                                                    className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                                                >
                                                    <span>{t('settings.usage_extraction_advanced_json')}</span>
                                                    <span>{usageAdvancedRulesOpen ? t('common.hide') : t('common.show')}</span>
                                                </button>
                                                {usageAdvancedRulesOpen && (
                                                    <div className="border-t border-input p-3">
                                                        <Textarea
                                                            value={usageRulesText}
                                                            onChange={e => setUsageRulesText(e.target.value)}
                                                            rows={12}
                                                            spellCheck={false}
                                                            className="min-h-[260px] w-full rounded-lg border-input bg-background font-mono text-xs leading-relaxed transition-colors focus-visible:bg-background resize-y"
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
                    {(activeTab === 'routing' || (activeTab === 'logging' && loggingSection === 'general') || activeTab === 'overrides') && (
                        <div className="sticky bottom-0 z-30 -mx-4 border-t border-input bg-background px-4 py-3 sm:-mx-10 sm:px-10">
                            <div className="mx-auto flex max-w-7xl items-center justify-end gap-4">
                                <Button
                                    type="button"
                                    onClick={handleSaveAll}
                                    disabled={saving || Boolean(archiveKeyError) || (archiveEnabled && !archiveS3Ready)}
                                    variant="default"
                                    size="lg"
                                    className="h-9 min-w-[160px] rounded-md font-medium transition-all whitespace-nowrap"
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
