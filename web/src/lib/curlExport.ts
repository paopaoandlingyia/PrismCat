import type { RequestLog } from './api'

export function buildCurlCommand(log: RequestLog, body?: string): string {
    const lines = [`curl ${shellQuote(log.target_url)}`]
    const method = (log.method || 'GET').toUpperCase()

    if (method !== 'GET') {
        lines.push(`  -X ${shellQuote(method)}`)
    }

    const headers = log.request_headers ?? {}
    for (const [name, values] of Object.entries(headers)) {
        for (const value of values) {
            lines.push(`  -H ${shellQuote(`${name}: ${value}`)}`)
        }
    }

    const requestBody = body ?? log.request_body_final ?? log.request_body ?? ''
    if (requestBody !== '') {
        lines.push(`  --data-raw ${shellQuote(requestBody)}`)
    }

    return lines.join(' \\\n')
}

function shellQuote(value: string): string {
    return `'${value.replace(/'/g, `'\\''`)}'`
}
