import type { RequestLog } from './api'

export interface LogBodyStatus {
    requestTruncated: boolean
    responseTruncated: boolean
    responseInterrupted: boolean
}

export function getLogBodyStatus(
    log: Pick<RequestLog, 'bodies' | 'truncated' | 'body_storage_error'>,
): LogBodyStatus {
    const requestTruncated = log.bodies?.some(body => body.part !== 'response' && body.truncated) ?? false
    const responseTruncated = log.bodies?.some(body => body.part === 'response' && body.truncated) ?? false

    return {
        requestTruncated,
        responseTruncated,
        // Capture/storage failures are represented by per-part metadata. The
        // remaining record-level flag means the client ended response transfer.
        responseInterrupted: log.truncated && !requestTruncated && !responseTruncated && !log.body_storage_error,
    }
}
