export interface ArchivePagination {
    current: number
    pages: number
    previousOffset: number
    nextOffset: number
    hasPrevious: boolean
    hasNext: boolean
}

export function getArchivePagination(offset: number, limit: number, total: number): ArchivePagination {
    const safeLimit = Math.max(1, limit)
    const safeTotal = Math.max(0, total)
    const lastOffset = safeTotal === 0 ? 0 : Math.floor((safeTotal - 1) / safeLimit) * safeLimit
    const safeOffset = Math.min(Math.max(0, offset), lastOffset)
    return {
        current: safeTotal === 0 ? 0 : Math.floor(safeOffset / safeLimit) + 1,
        pages: safeTotal === 0 ? 0 : Math.ceil(safeTotal / safeLimit),
        previousOffset: Math.max(0, safeOffset - safeLimit),
        nextOffset: Math.min(lastOffset, safeOffset + safeLimit),
        hasPrevious: safeOffset > 0,
        hasNext: safeOffset < lastOffset,
    }
}
