export interface Base64Detection {
    isBase64: boolean
    fileType: 'jpeg' | 'png' | 'gif' | 'webp' | 'pdf' | 'unknown' | null
    isImage: boolean
    mimeType?: string
    label: string
}

export type SmartTextSegment =
    | { type: 'text'; content: string }
    | { type: 'b64'; content: string; detection: Base64Detection; prefix?: string }

export const LARGE_TEXT_THRESHOLD = 50_000
export const LARGE_TEXT_PREVIEW_LENGTH = 4_000

const NO_B64: Base64Detection = { isBase64: false, fileType: null, isImage: false, label: '' }
const BASE64_HEAD_REGEX = /^[A-Za-z0-9+/_-]+$/
const BASE64_SEGMENT_REGEX = /(data:[^\s]+?;base64,)?([A-Za-z0-9+/_-]{200,}[=]{0,2})/g

function isBase64Url(value: string) {
    return /[-_]/.test(value)
}

export function normalizeBase64(value: string) {
    const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
    const remainder = normalized.length % 4
    if (remainder === 0 || remainder === 1) return normalized
    return normalized + '='.repeat(4 - remainder)
}

function unknownBase64Label(value: string) {
    if (value.startsWith('gAAAAA')) return 'Fernet Token'
    return isBase64Url(value) ? 'Base64URL' : 'Base64'
}

export function detectBase64(value: string): Base64Detection {
    if (value.length < 200) return NO_B64
    if (!BASE64_HEAD_REGEX.test(value.substring(0, 200))) return NO_B64
    try {
        const decoded = atob(normalizeBase64(value.substring(0, 16)))
        const b = new Uint8Array(decoded.length)
        for (let i = 0; i < decoded.length; i++) b[i] = decoded.charCodeAt(i)

        if (b[0] === 0xFF && b[1] === 0xD8 && b[2] === 0xFF)
            return { isBase64: true, fileType: 'jpeg', isImage: true, mimeType: 'image/jpeg', label: 'JPEG' }
        if (b[0] === 0x89 && b[1] === 0x50 && b[2] === 0x4E && b[3] === 0x47)
            return { isBase64: true, fileType: 'png', isImage: true, mimeType: 'image/png', label: 'PNG' }
        if (b[0] === 0x47 && b[1] === 0x49 && b[2] === 0x46)
            return { isBase64: true, fileType: 'gif', isImage: true, mimeType: 'image/gif', label: 'GIF' }
        if (b[0] === 0x52 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x46)
            return { isBase64: true, fileType: 'webp', isImage: true, mimeType: 'image/webp', label: 'WebP' }
        if (b[0] === 0x25 && b[1] === 0x50 && b[2] === 0x44 && b[3] === 0x46)
            return { isBase64: true, fileType: 'pdf', isImage: false, mimeType: 'application/pdf', label: 'PDF' }
        return { isBase64: true, fileType: 'unknown', isImage: false, label: unknownBase64Label(value) }
    } catch {
        return NO_B64
    }
}

export function parseDataUri(value: string) {
    if (!value.startsWith('data:')) return null
    const marker = ';base64,'
    const idx = value.indexOf(marker)
    if (idx < 0) return null
    const base64Data = value.substring(idx + marker.length)
    if (!base64Data || base64Data.length < 100) return null
    return {
        prefix: value.substring(0, idx + marker.length),
        base64Data,
        mimeType: value.substring(5, idx).split(';')[0],
    }
}

export function detectionFromMime(mimeType: string): Base64Detection {
    return {
        isBase64: true,
        fileType: 'unknown',
        isImage: mimeType.startsWith('image/'),
        mimeType,
        label: mimeType.split('/')[1]?.toUpperCase() || 'Base64',
    }
}

export function buildSmartTextSegments(text: string): SmartTextSegment[] | null {
    if (!text || text.length < 200 || text.length > LARGE_TEXT_THRESHOLD) return null
    const parts: SmartTextSegment[] = []
    let lastIndex = 0
    let found = false
    const regex = new RegExp(BASE64_SEGMENT_REGEX)
    let match

    while ((match = regex.exec(text)) !== null) {
        const prefix = match[1] || undefined
        const b64 = match[2]
        const detection = detectBase64(b64)
        if (!detection.isBase64) continue
        found = true
        if (match.index > lastIndex) parts.push({ type: 'text', content: text.substring(lastIndex, match.index) })
        if (prefix) parts.push({ type: 'text', content: prefix })
        parts.push({ type: 'b64', content: b64, detection, prefix })
        lastIndex = regex.lastIndex
    }

    if (!found) return null
    if (lastIndex < text.length) parts.push({ type: 'text', content: text.substring(lastIndex) })
    return parts
}

function getSmartTextSearchFragments(text: string): string[] {
    const segments = buildSmartTextSegments(text)
    if (!segments) return [text]
    return segments.flatMap((segment) => segment.type === 'text' ? [segment.content] : [])
}

export function getValueSearchFragments(value: unknown, isRoot: boolean): string[] {
    if (value === null) return ['null']
    if (typeof value === 'number' || typeof value === 'boolean') return [String(value)]
    if (typeof value !== 'string') return []
    if (isRoot) return getSmartTextSearchFragments(value)

    const dataUri = parseDataUri(value)
    if (dataUri) return [dataUri.prefix]
    if (detectBase64(value).isBase64) return []
    return [value]
}
