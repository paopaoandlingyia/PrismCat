import type { RequestLog } from './api'

export type LogBackupState = 'pending' | 'verified_cleanup' | 'verified_saved' | 'restored'

export function getLogBackupState(
    log: Pick<RequestLog, 'origin' | 'backup_verified_at' | 'annotation'>,
): LogBackupState {
    if (log.origin === 'archive_import') return 'restored'
    if (!log.backup_verified_at) return 'pending'
    return log.annotation?.saved ? 'verified_saved' : 'verified_cleanup'
}
