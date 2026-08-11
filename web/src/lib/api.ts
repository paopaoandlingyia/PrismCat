// API 响应类型
export interface RequestLog {
    id: string
    created_at: string
    upstream: string
    upstream_target?: string
    target_url: string
    method: string
    path: string
    query?: string
    request_headers?: Record<string, string[]>
    request_body?: string
    request_body_original?: string
    request_body_final?: string
    request_body_ref?: string
    request_body_size: number
    status_code: number
    response_headers?: Record<string, string[]>
    response_body?: string
    response_body_ref?: string
    response_body_size: number
    streaming: boolean
    latency_ms: number
    error?: string
    truncated: boolean
    tag?: string
    trace_id?: string
    parent_log_id?: string
    trace_seq?: number
    request_override_applied?: boolean
    request_override_rules?: string[]
    request_override_error?: string
    request_header_override_applied?: boolean
    request_header_override_changes?: HeaderOverrideChange[]
    request_headers_original?: Record<string, string[]>
    usage_input_tokens?: number
    usage_output_tokens?: number
    usage_total_tokens?: number
    usage_raw?: string
    usage_source?: string
    annotation: LogAnnotation
}

export interface HeaderOverrideChange {
    op: string
    name: string
    value?: string
    old_value?: string
}

export type LogAnnotationStatus = 'none' | 'todo' | 'done'

export interface LogAnnotation {
    saved: boolean
    status: LogAnnotationStatus
    note?: string
    labels?: string[]
    created_at?: string
    updated_at?: string
}

export type LiveLogEvent =
    | {
        type: 'snapshot'
        log?: RequestLog
    }
    | {
        type: 'response_chunk'
        chunk?: string
        size_delta?: number
    }
    | {
        type: 'completed'
        log?: RequestLog
    }

export interface LogListResponse {
    logs: RequestLog[]
    total: number
    offset: number
    limit: number
}

export interface LogStats {
    total_requests: number
    success_count: number
    error_count: number
    streaming_count: number
    avg_latency_ms: number
    by_upstream: Record<string, number>
    by_status_code: Record<string, number>
}

export interface Upstream {
    name: string
    target: string
    timeout: number
    response_header_timeout: number
    response_body_first_byte_timeout: number
    response_body_idle_timeout: number
    order: number
    outbound_proxy: string
    logging_enabled: boolean
    active_target?: string
    targets?: Record<string, UpstreamTarget>
}

export interface RuleBinding {
    enabled: boolean
    rule_names: string[]
}

export interface UpstreamTarget {
    url: string
    timeout?: number
    response_header_timeout?: number
    response_body_first_byte_timeout?: number
    response_body_idle_timeout?: number
    outbound_proxy?: string
    request_overrides?: RuleBinding
    usage_extraction?: RuleBinding
}

// 查询过滤参数
export interface LogFilter {
    upstream?: string
    method?: string
    path?: string
    status_code?: number
    tag?: string
    trace_id?: string
    saved?: boolean
    annotation_status?: LogAnnotationStatus
    annotation_label?: string
    start_time?: string
    end_time?: string
    offset?: number
    limit?: number
}

// Trace 类型
export interface TraceSummary {
    trace_id: string
    request_count: number
    first_time: number
    last_time: number
    total_latency_ms: number
    error_count: number
    usage_input_tokens?: number
    usage_output_tokens?: number
    usage_total_tokens?: number
    upstreams: string[] | null
    tags: string[] | null
}

export interface TraceFilter {
    trace_id?: string
    upstream?: string
    tag?: string
    has_error?: boolean
    start_time?: string
    end_time?: string
    offset?: number
    limit?: number
}

export interface TraceListResponse {
    traces: TraceSummary[] | null
    total: number
    offset: number
    limit: number
}

export interface TraceDetail {
    trace_id: string
    requests: RequestLog[]
    summary: {
        request_count: number
        total_latency_ms: number
        error_count: number
        first_time: number
        last_time: number
        upstreams: string[]
        usage_input_tokens?: number
        usage_output_tokens?: number
        usage_total_tokens?: number
    }
}

// API 调用函数
const API_BASE = '/api'
export const DEFAULT_UPSTREAM_TIMEOUT_SECONDS = 120

export interface AuthStatus {
    authenticated: boolean
    auth_required: boolean
    setup_required: boolean
    session_expires_at?: string
}

export async function fetchAuthStatus(): Promise<AuthStatus> {
    const response = await fetch(`${API_BASE}/auth/me`)
    if (!response.ok) throw new Error('获取登录状态失败')
    return response.json()
}

export async function login(password: string): Promise<AuthStatus> {
    const response = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '登录失败')
    }
    return response.json()
}

export async function setupPassword(password: string): Promise<AuthStatus> {
    const response = await fetch(`${API_BASE}/auth/setup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '初始化密码失败')
    }
    return response.json()
}

export async function logout(): Promise<void> {
    const response = await fetch(`${API_BASE}/auth/logout`, {
        method: 'POST',
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '退出登录失败')
    }
}

export async function fetchLogs(filter: LogFilter = {}): Promise<LogListResponse> {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (value !== undefined && value !== '') {
            params.append(key, String(value))
        }
    })

    const response = await fetch(`${API_BASE}/logs?${params}`)
    if (!response.ok) throw new Error('获取日志列表失败')
    return response.json()
}

export function buildLogsExportUrl(filter: LogFilter = {}, includeBody = true): string {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (key === 'offset' || key === 'limit') return
        if (value !== undefined && value !== '') {
            params.append(key, String(value))
        }
    })
    params.set('format', 'jsonl')
    params.set('include_body', String(includeBody))
    return `${API_BASE}/logs/export?${params}`
}

export async function fetchLog(id: string): Promise<RequestLog> {
    const response = await fetch(`${API_BASE}/logs/${id}`)
    if (!response.ok) throw new Error('获取日志详情失败')
    return response.json()
}

export async function fetchStats(since?: string): Promise<LogStats> {
    const params = since ? `?since=${since}` : ''
    const response = await fetch(`${API_BASE}/stats${params}`)
    if (!response.ok) throw new Error('获取统计数据失败')
    return response.json()
}

export async function fetchUpstreams(): Promise<Upstream[]> {
    const response = await fetch(`${API_BASE}/upstreams`)
    if (!response.ok) throw new Error('获取上游配置失败')
    return response.json()
}

export async function fetchTraces(filter: TraceFilter = {}): Promise<TraceListResponse> {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (value !== undefined && value !== '') {
            params.append(key, String(value))
        }
    })
    const response = await fetch(`${API_BASE}/traces?${params}`)
    if (!response.ok) throw new Error('获取 Trace 列表失败')
    return response.json()
}

export async function fetchTraceDetail(traceId: string): Promise<TraceDetail> {
    const response = await fetch(`${API_BASE}/traces/${encodeURIComponent(traceId)}`)
    if (!response.ok) throw new Error('获取 Trace 详情失败')
    return response.json()
}

export async function addUpstream(
    name: string,
    target: string,
    timeout: number = DEFAULT_UPSTREAM_TIMEOUT_SECONDS,
    response_header_timeout: number = 0,
    response_body_first_byte_timeout: number = 0,
    response_body_idle_timeout: number = 0,
    order: number = 0,
    outbound_proxy: string = 'env',
    logging_enabled: boolean = true,
    active_target: string = '',
    targets?: Record<string, UpstreamTarget>,
): Promise<void> {
    const response = await fetch(`${API_BASE}/upstreams`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name, target, timeout, response_header_timeout, response_body_first_byte_timeout, response_body_idle_timeout, order, outbound_proxy, logging_enabled, active_target, targets }),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '添加上游失败')
    }
}

export async function activateUpstreamTarget(upstream: string, target: string): Promise<void> {
    const response = await fetch(`${API_BASE}/upstreams/active-target`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ upstream, target }),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '切换目标失败')
    }
}

export async function removeUpstream(name: string): Promise<void> {
    const response = await fetch(`${API_BASE}/upstreams?name=${encodeURIComponent(name)}`, {
        method: 'DELETE',
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '删除上游失败')
    }
}

// 应用配置类型
export interface AppConfig {
    version: string
    server: {
        proxy_domains: string[]
        enable_path_routing: boolean
        path_routing_prefix: string
    }
    logging: {
        max_request_body: number
        max_response_body: number
        sensitive_headers: string[]
        early_request_body_snapshot: boolean
        detach_body_over_bytes: number
        body_preview_bytes: number
        store_base64: boolean
    }
    storage: {
        database: string
        retention_days: number
        max_storage_bytes: number
    }
    request_overrides: {
        enabled: boolean
        max_body_bytes: number
        upstreams: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules: unknown[]
    }
    usage_extraction: {
        enabled: boolean
        upstreams: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules: unknown[]
    }
}

export async function updateLogAnnotation(
    id: string,
    update: Partial<Pick<LogAnnotation, 'saved' | 'status' | 'note' | 'labels'>>,
): Promise<LogAnnotation> {
    const response = await fetch(`${API_BASE}/logs/${encodeURIComponent(id)}/annotation`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(update),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '保存日志标记失败')
    }
    return response.json()
}

export interface ConfigUpdate {
    server?: {
        enable_path_routing?: boolean
        path_routing_prefix?: string
    }
    logging?: {
        max_request_body?: number
        max_response_body?: number
        sensitive_headers?: string[]
        early_request_body_snapshot?: boolean
        detach_body_over_bytes?: number
        body_preview_bytes?: number
        store_base64?: boolean
    }
    storage?: {
        retention_days?: number
        max_storage_bytes?: number
    }
    request_overrides?: {
        enabled?: boolean
        max_body_bytes?: number
        upstreams?: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules?: unknown[]
    }
    usage_extraction?: {
        enabled?: boolean
        upstreams?: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules?: unknown[]
    }
}

export async function fetchConfig(): Promise<AppConfig> {
    const response = await fetch(`${API_BASE}/config`)
    if (!response.ok) throw new Error('获取配置失败')
    return response.json()
}

export interface SystemMetrics {
    timestamp: string
    platform: string
    runtime: {
        go_version: string
        num_cpu: number
        goroutines: number
        uptime_seconds: number
    }
    process: {
        pid: number
        rss_bytes?: number
        heap_alloc_bytes: number
        heap_sys_bytes: number
        cpu_seconds?: number
        cpu_percent?: number
    }
    memory: {
        total_bytes?: number
        used_bytes?: number
        available_bytes?: number
        source: string
    }
}

export async function fetchSystemMetrics(): Promise<SystemMetrics> {
    const response = await fetch(`${API_BASE}/system/metrics`)
    if (!response.ok) throw new Error('获取资源占用失败')
    return response.json()
}

export interface StorageUsage {
    calculated_at: string
    blob_store: string
    database_bytes: number
    database_files: number
    blob_bytes: number
    blob_files: number
    total_bytes: number
}

export async function fetchStorageUsage(): Promise<StorageUsage> {
    const response = await fetch(`${API_BASE}/system/storage`)
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '计算存储占用失败')
    }
    return response.json()
}

export interface UpdateAsset {
    name: string
    download_url: string
    size: number
}

export interface UpdateInfo {
    current_version: string
    latest_version: string
    latest_tag: string
    update_available: boolean
    release_url: string
    published_at: string
    platform: string
    arch: string
    assets: UpdateAsset[]
    matching_asset?: UpdateAsset
}

export async function fetchUpdateInfo(): Promise<UpdateInfo> {
    const response = await fetch(`${API_BASE}/system/update`)
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '检查更新失败')
    }
    return response.json()
}

export async function updateConfig(update: ConfigUpdate): Promise<void> {
    const response = await fetch(`${API_BASE}/config`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(update),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '更新配置失败')
    }
}

export async function fetchBlob(ref: string): Promise<string> {
    const response = await fetch(`${API_BASE}/blobs/${encodeURIComponent(ref)}`)
    if (!response.ok) throw new Error('获取 Blob 失败')
    return response.text()
}

export interface LogBodyResponse {
    body: string
    truncated?: boolean
    body_decoded?: boolean
    body_decoded_from?: string
    decode_failed?: boolean
}

export async function fetchLogBody(id: string, part: 'request' | 'response'): Promise<LogBodyResponse> {
    const params = new URLSearchParams({ part })
    const response = await fetch(`${API_BASE}/logs/${encodeURIComponent(id)}/body?${params}`)
    if (!response.ok) throw new Error('获取 Body 失败')
    return response.json()
}

// Replay (Playground)
export interface ReplayRequest {
    upstream?: string
    target_url?: string
    method: string
    path?: string
    headers: Record<string, string>
    body: string
}

export interface ReplayResponse {
    status_code: number
    headers: Record<string, string[]>
    body: string
    truncated?: boolean
    body_decoded?: boolean
    body_decoded_from?: string
}

export async function sendReplay(req: ReplayRequest): Promise<ReplayResponse> {
    const response = await fetch(`${API_BASE}/replay`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '重放请求失败')
    }
    return response.json()
}
