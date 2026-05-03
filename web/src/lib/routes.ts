export const logRequestDiffRoute = '/logs/:id/diff/request'

export function logRequestDiffPath(id: string) {
    return `/logs/${encodeURIComponent(id)}/diff/request`
}
