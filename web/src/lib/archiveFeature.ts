import { createContext, useContext } from 'react'

export const ArchiveFeatureContext = createContext(false)
const archiveFeatureChangedEvent = 'prismcat:archive-feature-changed'

export function useArchiveFeatureEnabled(): boolean {
    return useContext(ArchiveFeatureContext)
}

export function notifyArchiveFeatureChanged(enabled: boolean): void {
    window.dispatchEvent(new CustomEvent<boolean>(archiveFeatureChangedEvent, { detail: enabled }))
}

export function listenForArchiveFeatureChanges(onChange: (enabled: boolean) => void): () => void {
    const listener = (event: Event) => onChange((event as CustomEvent<boolean>).detail)
    window.addEventListener(archiveFeatureChangedEvent, listener)
    return () => window.removeEventListener(archiveFeatureChangedEvent, listener)
}
