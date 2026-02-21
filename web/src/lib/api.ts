// API 响应类型
export interface RequestLog {
    id: string
    created_at: string
    upstream: string
    target_url: string
    method: string
    path: string
    query?: string
    request_headers?: Record<string, string[]>
    request_body?: string
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
}

// 查询过滤参数
export interface LogFilter {
    upstream?: string
    method?: string
    path?: string
    status_code?: number
    tag?: string
    start_time?: string
    end_time?: string
    offset?: number
    limit?: number
}

// API 调用函数
const API_BASE = '/api'

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

export async function addUpstream(name: string, target: string, timeout: number = 30): Promise<void> {
    const response = await fetch(`${API_BASE}/upstreams`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name, target, timeout }),
    })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '请求失败' }))
        throw new Error(error.error || '添加上游失败')
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
    }
    logging: {
        max_request_body: number
        max_response_body: number
        sensitive_headers: string[]
        detach_body_over_bytes: number
        body_preview_bytes: number
        store_base64: boolean
    }
    storage: {
        database: string
        retention_days: number
    }
}

export interface ConfigUpdate {
    logging?: {
        max_request_body?: number
        max_response_body?: number
        sensitive_headers?: string[]
        detach_body_over_bytes?: number
        body_preview_bytes?: number
        store_base64?: boolean
    }
    storage?: {
        retention_days?: number
    }
}

export async function fetchConfig(): Promise<AppConfig> {
    const response = await fetch(`${API_BASE}/config`)
    if (!response.ok) throw new Error('获取配置失败')
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

// Replay (Playground)
export interface ReplayRequest {
    upstream: string
    method: string
    path: string
    headers: Record<string, string>
    body: string
}

export interface ReplayResponse {
    status_code: number
    headers: Record<string, string[]>
    body: string
    truncated?: boolean
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
