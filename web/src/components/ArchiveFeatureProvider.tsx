import type { ReactNode } from 'react'

import { ArchiveFeatureContext } from '@/lib/archiveFeature'

export function ArchiveFeatureProvider({ enabled, children }: { enabled: boolean; children: ReactNode }) {
    return <ArchiveFeatureContext.Provider value={enabled}>{children}</ArchiveFeatureContext.Provider>
}
