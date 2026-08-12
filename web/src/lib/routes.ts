export const logRequestDiffRoute = '/logs/:id/diff/request'

// 设置页的分区由 URL 驱动,这样每个分区可以直接分享链接,侧边栏也能挂子项。
// 四个分区共用同一份配置草稿与保存栏,所以只换渲染内容,状态仍留在 Settings 里。
export const settingsTabs = ['routing', 'logging', 'overrides', 'system'] as const

export type SettingsTab = (typeof settingsTabs)[number]

export const settingsTabLabelKeys: Record<SettingsTab, string> = {
    routing: 'settings.tabs.upstreams',
    logging: 'settings.tabs.logging',
    overrides: 'settings.tabs.overrides',
    system: 'settings.tabs.system',
}

export function settingsTabPath(tab: SettingsTab) {
    return `/settings/${tab}`
}

export function isSettingsTab(value: unknown): value is SettingsTab {
    return typeof value === 'string' && (settingsTabs as readonly string[]).includes(value)
}

export function logRequestDiffPath(id: string) {
    return `/logs/${encodeURIComponent(id)}/diff/request`
}
