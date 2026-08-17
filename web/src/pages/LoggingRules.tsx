import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  FilePlus2,
  ListFilter,
  Plus,
  RefreshCw,
  Save,
  Search,
  Trash2,
  WandSparkles,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import {
  addUpstream,
  deleteIgnoredPaths,
  fetchIgnoredPaths,
  fetchModelPathTemplates,
  fetchUpstreams,
  saveModelPathTemplates,
  type IgnoredPathFilter,
  type IgnoredPathRecord,
  type ModelPathTemplate,
  type SystemModelPathTemplate,
  type Upstream,
} from '@/lib/api'
import { mergeLoggingPathRules } from '@/lib/loggingRules'
import { copyText } from '@/lib/clipboard'
import { LoggingPathRuleEditor } from '@/components/LoggingPathRuleEditor'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export type LoggingRulesTab = 'models' | 'ignored'

interface LoggingRulesProps {
  tab?: LoggingRulesTab
  embedded?: boolean
}

function rowKey(record: IgnoredPathRecord) {
  return `${record.upstream}\u0000${record.path}`
}

export function LoggingRules({ tab = 'models', embedded = false }: LoggingRulesProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const activeTab = tab

  const [templates, setTemplates] = useState<ModelPathTemplate[]>([])
  const [systemDefaults, setSystemDefaults] = useState<SystemModelPathTemplate[]>([])
  const [upstreams, setUpstreams] = useState<Upstream[]>([])
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [systemTag, setSystemTag] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const [ignored, setIgnored] = useState<IgnoredPathRecord[]>([])
  const [ignoredTotal, setIgnoredTotal] = useState(0)
  const [ignoredRequests, setIgnoredRequests] = useState(0)
  const [ignoredLoading, setIgnoredLoading] = useState(false)
  const [ignoredFilter, setIgnoredFilter] = useState<IgnoredPathFilter>({ offset: 0, limit: 50, sort: 'last_seen', order: 'desc' })
  const [ignoredDraft, setIgnoredDraft] = useState<IgnoredPathFilter>({ path: '', upstream: '' })
  const [selectedIgnored, setSelectedIgnored] = useState<Set<string>>(new Set())
  const [ignoredTemplateTag, setIgnoredTemplateTag] = useState('')

  const loadLibraries = useCallback(async () => {
    setLoading(true)
    try {
      const [library, upstreamData] = await Promise.all([fetchModelPathTemplates(), fetchUpstreams()])
      setTemplates(library.templates || [])
      setSystemDefaults(library.system_defaults || [])
      setUpstreams(upstreamData || [])
      setSelectedIndex(index => Math.min(index, Math.max(0, (library.templates?.length || 1) - 1)))
      setSystemTag(current => current || library.system_defaults?.[0]?.tag || '')
      setIgnoredTemplateTag(current => current || library.templates?.[0]?.tag || '')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('common.error'))
    } finally {
      setLoading(false)
    }
  }, [t])

  const loadIgnored = useCallback(async () => {
    setIgnoredLoading(true)
    try {
      const result = await fetchIgnoredPaths(ignoredFilter)
      setIgnored(result.paths || [])
      setIgnoredTotal(result.total || 0)
      setIgnoredRequests(result.total_requests || 0)
      setSelectedIgnored(new Set())
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('common.error'))
    } finally {
      setIgnoredLoading(false)
    }
  }, [ignoredFilter, t])

  useEffect(() => { void loadLibraries() }, [loadLibraries])
  useEffect(() => {
    if (activeTab === 'ignored') void loadIgnored()
  }, [activeTab, loadIgnored])

  const selectedTemplate = templates[selectedIndex]
  const exactSystemDefault = useMemo(() => {
    if (!selectedTemplate?.tag) return undefined
    return systemDefaults.find(template => template.tag.toLowerCase() === selectedTemplate.tag.trim().toLowerCase())
  }, [selectedTemplate?.tag, systemDefaults])
  const selectedSystemDefault = useMemo(
    () => systemDefaults.find(template => template.tag === systemTag),
    [systemDefaults, systemTag],
  )
  const systemDefaultGroups = useMemo(() => {
    const groups = new Map<string, SystemModelPathTemplate[]>()
    for (const template of systemDefaults) {
      const group = groups.get(template.provider) || []
      group.push(template)
      groups.set(template.provider, group)
    }
    return Array.from(groups.entries())
  }, [systemDefaults])

  const updateSelectedTemplate = (update: Partial<ModelPathTemplate>) => {
    setTemplates(current => current.map((template, index) => index === selectedIndex ? { ...template, ...update } : template))
  }

  const handleAddTemplate = () => {
    const used = new Set(templates.map(template => template.tag.toLowerCase()))
    let index = templates.length + 1
    let tag = `model-${index}`
    while (used.has(tag)) {
      index += 1
      tag = `model-${index}`
    }
    setTemplates(current => [...current, { tag, rules: [] }])
    setSelectedIndex(templates.length)
  }

  const handleDeleteTemplate = () => {
    if (!selectedTemplate || !confirm(t('logging_rules.confirm_delete_template', { tag: selectedTemplate.tag }))) return
    setTemplates(current => current.filter((_, index) => index !== selectedIndex))
    setSelectedIndex(index => Math.max(0, index - 1))
  }

  const mergeSystemTemplate = (tag: string) => {
    const systemTemplate = systemDefaults.find(template => template.tag === tag)
    if (!selectedTemplate || !systemTemplate) return
    const merged = mergeLoggingPathRules(selectedTemplate.rules, systemTemplate.rules)
    updateSelectedTemplate({ rules: merged.rules })
    toast.success(t('logging_rules.fill_result', { added: merged.added, skipped: merged.skipped }))
  }

  const persistTemplates = async (nextTemplates = templates, notifySaved = true): Promise<boolean> => {
    setSaving(true)
    try {
      const normalizedTemplates = nextTemplates.map(template => ({
        ...template,
        tag: template.tag.trim(),
        rules: mergeLoggingPathRules([], template.rules).rules,
      }))
      await saveModelPathTemplates(normalizedTemplates)
      setTemplates(normalizedTemplates)
      if (notifySaved) toast.success(t('logging_rules.templates_saved'))
      return true
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('common.error'))
      return false
    } finally {
      setSaving(false)
    }
  }

  const applyIgnoredSearch = () => {
    setIgnoredFilter(current => ({ ...current, path: ignoredDraft.path || '', upstream: ignoredDraft.upstream || '', offset: 0 }))
  }

  const resetIgnoredSearch = () => {
    setIgnoredDraft({ path: '', upstream: '' })
    setIgnoredFilter({ offset: 0, limit: 50, sort: 'last_seen', order: 'desc' })
  }

  const handleDeleteIgnored = async (upstream = '', path = '') => {
    if (!confirm(t(path ? 'logging_rules.confirm_delete_ignored' : 'logging_rules.confirm_clear_ignored'))) return
    try {
      await deleteIgnoredPaths(upstream, path)
      await loadIgnored()
      toast.success(t('logging_rules.ignored_deleted'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('common.error'))
    }
  }

  const handleAddSelectedToTemplate = async () => {
    const templateIndex = templates.findIndex(template => template.tag === ignoredTemplateTag)
    if (templateIndex < 0 || selectedIgnored.size === 0) return
    const incoming = ignored
      .filter(record => selectedIgnored.has(rowKey(record)))
      .map(record => ({ matcher: 'ant' as const, pattern: record.path }))
    const merged = mergeLoggingPathRules(templates[templateIndex].rules, incoming)
    const next = templates.map((template, index) => index === templateIndex ? { ...template, rules: merged.rules } : template)
    if (await persistTemplates(next, false)) {
      toast.success(t('logging_rules.fill_result', { added: merged.added, skipped: merged.skipped }))
    }
  }

  const handleAddToAllowlist = async (record: IgnoredPathRecord) => {
    const upstream = upstreams.find(item => item.name === record.upstream)
    if (!upstream) return
    if (upstream.logging_path_filter?.mode !== 'allowlist') {
      navigate(`/settings/routing?edit=${encodeURIComponent(upstream.name)}&focus=logging`)
      return
    }
    const merged = mergeLoggingPathRules(upstream.logging_path_filter.rules || [], [{ matcher: 'ant', pattern: record.path }])
    const filter = { ...upstream.logging_path_filter, rules: merged.rules }
    const usesTargets = Boolean(upstream.targets && Object.keys(upstream.targets).length)
    try {
      await addUpstream(
        upstream.name,
        usesTargets ? '' : upstream.target,
        usesTargets ? 0 : upstream.timeout,
        usesTargets ? 0 : upstream.response_header_timeout,
        usesTargets ? 0 : upstream.response_body_first_byte_timeout,
        usesTargets ? 0 : upstream.response_body_idle_timeout,
        upstream.order,
        usesTargets ? '' : upstream.outbound_proxy,
        upstream.logging_enabled,
        usesTargets ? upstream.active_target || '' : '',
        usesTargets ? upstream.targets : undefined,
        filter,
      )
      setUpstreams(current => current.map(item => item.name === upstream.name ? { ...item, logging_path_filter: filter } : item))
      toast.success(t('logging_rules.fill_result', { added: merged.added, skipped: merged.skipped }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('common.error'))
    }
  }

  if (loading) {
    return <div className="flex h-80 items-center justify-center text-sm text-muted-foreground">{t('common.loading')}</div>
  }

  const currentPage = Math.floor((ignoredFilter.offset || 0) / (ignoredFilter.limit || 50)) + 1
  const pageCount = Math.max(1, Math.ceil(ignoredTotal / (ignoredFilter.limit || 50)))

  return (
    <Tabs value={activeTab} onValueChange={value => navigate(`/settings/logging/${value}`)} className="space-y-4">
      {!embedded && (
        <TabsList>
          <TabsTrigger value="models"><FilePlus2 />{t('logging_rules.tabs.models')}</TabsTrigger>
          <TabsTrigger value="ignored"><ListFilter />{t('logging_rules.tabs.ignored')}</TabsTrigger>
        </TabsList>
      )}

      {activeTab === 'models' && (
        <div className="grid min-h-[560px] grid-cols-1 overflow-hidden rounded-lg border border-border bg-card lg:grid-cols-[240px_minmax(0,1fr)]">
          <aside className="border-b border-border bg-muted/20 p-3 lg:border-r lg:border-b-0">
            <div className="mb-3 flex items-center justify-between gap-2">
              <span className="text-xs font-semibold text-foreground">{t('logging_rules.model_types')}</span>
              <Button size="icon-xs" variant="outline" onClick={handleAddTemplate} aria-label={t('logging_rules.add_model')}><Plus /></Button>
            </div>
            <div className="flex gap-1 overflow-x-auto lg:block lg:space-y-1">
              {templates.map((template, index) => (
                <button
                  key={`${template.tag}-${index}`}
                  type="button"
                  onClick={() => setSelectedIndex(index)}
                  className={`min-w-[140px] rounded-md px-2.5 py-2 text-left text-sm transition-colors lg:w-full ${index === selectedIndex ? 'bg-accent font-medium text-foreground' : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'}`}
                >
                  <span className="block truncate">{template.tag}</span>
                  <span className="mt-0.5 block text-xs text-muted-foreground">{t('logging_rules.path_count', { count: template.rules.length })}</span>
                </button>
              ))}
            </div>
          </aside>

          <section className="min-w-0 p-4 sm:p-6">
            {selectedTemplate ? (
              <div className="mx-auto max-w-4xl space-y-6">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
                  <label className="min-w-0 flex-1 space-y-1.5">
                    <span className="text-xs font-medium text-muted-foreground">{t('logging_rules.tag')}</span>
                    <Input value={selectedTemplate.tag} onChange={event => updateSelectedTemplate({ tag: event.target.value })} className="font-mono" />
                  </label>
                  <Button variant="ghost" size="icon" onClick={handleDeleteTemplate} aria-label={t('common.delete')} className="text-muted-foreground hover:text-destructive"><Trash2 /></Button>
                </div>

                {exactSystemDefault && (
                  <div className="flex flex-wrap items-center justify-between gap-3 border-l-2 border-primary/60 bg-muted/30 px-4 py-3">
                    <span className="text-sm text-foreground">{t('logging_rules.system_match', { tag: exactSystemDefault.tag })}</span>
                    <Button size="sm" variant="outline" onClick={() => mergeSystemTemplate(exactSystemDefault.tag)}><WandSparkles />{t('logging_rules.fill_defaults')}</Button>
                  </div>
                )}

                <div className="space-y-3">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <h2 className="text-sm font-semibold text-foreground">{t('logging_rules.paths')}</h2>
                      <p className="mt-1 text-xs text-muted-foreground">{t('logging_rules.paths_hint')}</p>
                    </div>
                    <div className="min-w-0 sm:max-w-[420px]">
                      <div className="flex items-center gap-2">
                        <Select value={systemTag} onValueChange={setSystemTag}>
                          <SelectTrigger className="h-8 min-w-0 flex-1 text-xs sm:w-[260px]"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            {systemDefaultGroups.map(([provider, providerTemplates]) => (
                              <SelectGroup key={provider}>
                                <SelectLabel>{provider}</SelectLabel>
                                {providerTemplates.map(template => <SelectItem key={template.tag} value={template.tag}>{template.display_name} ({template.tag})</SelectItem>)}
                              </SelectGroup>
                            ))}
                          </SelectContent>
                        </Select>
                        <Button size="sm" variant="outline" onClick={() => mergeSystemTemplate(systemTag)} disabled={!systemTag}><WandSparkles />{t('logging_rules.fill')}</Button>
                      </div>
                      {selectedSystemDefault && (
                        <p className="mt-1.5 text-xs text-muted-foreground">
                          <span className="font-medium text-foreground/80">{selectedSystemDefault.provider} / {selectedSystemDefault.category}</span>
                          {' - '}{selectedSystemDefault.description}
                        </p>
                      )}
                    </div>
                  </div>
                  <LoggingPathRuleEditor rules={selectedTemplate.rules} onChange={rules => updateSelectedTemplate({ rules })} />
                </div>

                <div className="flex justify-end border-t border-border pt-4">
                  <Button onClick={() => void persistTemplates()} disabled={saving}><Save />{t('common.save')}</Button>
                </div>
              </div>
            ) : (
              <div className="flex h-full min-h-80 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
                <FilePlus2 className="h-9 w-9 opacity-40" />
                <span>{t('logging_rules.no_templates')}</span>
                <Button size="sm" variant="outline" onClick={handleAddTemplate}><Plus />{t('logging_rules.add_model')}</Button>
              </div>
            )}
          </section>
        </div>
      )}

      {activeTab === 'ignored' && (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border sm:w-[420px]">
            <div className="bg-card px-4 py-3"><div className="text-xs text-muted-foreground">{t('logging_rules.unique_paths')}</div><div className="mt-1 text-xl font-semibold">{ignoredTotal.toLocaleString()}</div></div>
            <div className="bg-card px-4 py-3"><div className="text-xs text-muted-foreground">{t('logging_rules.ignored_requests')}</div><div className="mt-1 text-xl font-semibold">{ignoredRequests.toLocaleString()}</div></div>
          </div>

          <div className="space-y-2 rounded-lg border border-border bg-card p-3">
            <div className="flex flex-col gap-2 md:flex-row md:items-center">
              <Input value={ignoredDraft.path || ''} onChange={event => setIgnoredDraft(current => ({ ...current, path: event.target.value }))} onKeyDown={event => event.key === 'Enter' && applyIgnoredSearch()} placeholder={t('logging_rules.search_path')} className="h-8 min-w-0 flex-1 text-xs" />
              <Select value={ignoredDraft.upstream || '_all'} onValueChange={value => setIgnoredDraft(current => ({ ...current, upstream: value === '_all' ? '' : value }))}>
                <SelectTrigger className="h-8 w-full text-xs md:w-[170px]"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="_all">{t('common.all')}</SelectItem>{upstreams.map(upstream => <SelectItem key={upstream.name} value={upstream.name}>{upstream.name}</SelectItem>)}</SelectContent>
              </Select>
              <Select value={ignoredFilter.sort || 'last_seen'} onValueChange={value => setIgnoredFilter(current => ({ ...current, sort: value as 'last_seen' | 'count', offset: 0 }))}>
                <SelectTrigger className="h-8 w-full text-xs md:w-[150px]"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="last_seen">{t('logging_rules.sort_recent')}</SelectItem><SelectItem value="count">{t('logging_rules.sort_count')}</SelectItem></SelectContent>
              </Select>
              <div className="flex gap-1.5">
                <Button size="icon-sm" variant="outline" onClick={applyIgnoredSearch} aria-label={t('common.search')}><Search /></Button>
                <Button size="icon-sm" variant="outline" onClick={resetIgnoredSearch} aria-label={t('common.reset')}><X /></Button>
                <Button size="icon-sm" variant="outline" onClick={() => void loadIgnored()} aria-label={t('common.refresh')}><RefreshCw /></Button>
                <Button size="icon-sm" variant="outline" onClick={() => void handleDeleteIgnored(ignoredFilter.upstream || '')} aria-label={t('logging_rules.clear')} className="text-muted-foreground hover:text-destructive"><Trash2 /></Button>
              </div>
            </div>
            {selectedIgnored.size > 0 && templates.length > 0 && (
              <div className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
                <Badge variant="secondary">{t('logging_rules.selected_count', { count: selectedIgnored.size })}</Badge>
                <Select value={ignoredTemplateTag} onValueChange={setIgnoredTemplateTag}>
                  <SelectTrigger className="h-8 w-[160px] text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>{templates.map(template => <SelectItem key={template.tag} value={template.tag}>{template.tag}</SelectItem>)}</SelectContent>
                </Select>
                <Button size="sm" variant="outline" onClick={() => void handleAddSelectedToTemplate()}><FilePlus2 />{t('logging_rules.add_to_template')}</Button>
              </div>
            )}
          </div>

          <div className="overflow-hidden rounded-lg border border-border bg-card">
            <div className="hidden grid-cols-[40px_140px_minmax(260px,1fr)_120px_180px_112px] items-center border-b border-border bg-muted/30 px-3 py-2 text-xs font-medium text-muted-foreground xl:grid">
              <span />
              <span>{t('log_table.upstream')}</span>
              <span>{t('log_table.path')}</span>
              <span className="text-right">{t('logging_rules.count')}</span>
              <span className="text-right">{t('logging_rules.last_seen')}</span>
              <span />
            </div>
            {ignoredLoading ? (
              <div className="py-20 text-center text-sm text-muted-foreground">{t('common.loading')}</div>
            ) : ignored.length === 0 ? (
              <div className="py-20 text-center text-sm text-muted-foreground">{t('logging_rules.no_ignored')}</div>
            ) : ignored.map(record => {
              const key = rowKey(record)
              const upstream = upstreams.find(item => item.name === record.upstream)
              const canAllow = upstream?.logging_path_filter?.mode === 'allowlist'
              return (
                <div key={key} className="grid grid-cols-[32px_minmax(0,1fr)_auto] gap-x-2 gap-y-1.5 border-b border-border px-3 py-3 last:border-b-0 xl:grid-cols-[40px_140px_minmax(260px,1fr)_120px_180px_112px] xl:items-center xl:gap-y-2 xl:py-2.5">
                  <input type="checkbox" checked={selectedIgnored.has(key)} onChange={event => setSelectedIgnored(current => { const next = new Set(current); if (event.target.checked) next.add(key); else next.delete(key); return next })} className="col-start-1 row-start-1 h-4 w-4 accent-primary xl:col-auto xl:row-auto" aria-label={t('logging_rules.select_path', { path: record.path })} />
                  <span className="col-start-2 row-start-1 truncate text-xs font-medium xl:col-auto xl:row-auto">{record.upstream}</span>
                  <span className="col-span-2 col-start-2 row-start-2 truncate font-mono text-xs xl:col-auto xl:row-auto xl:col-span-1" title={record.path}>{record.path}</span>
                  <span className="col-start-3 row-start-1 text-right text-xs tabular-nums xl:col-auto xl:row-auto">{record.request_count.toLocaleString()}</span>
                  <span className="col-start-2 row-start-3 text-xs text-muted-foreground xl:col-auto xl:row-auto xl:text-right">{new Date(record.last_seen).toLocaleString()}</span>
                  <div className="col-start-3 row-start-3 flex justify-end gap-1 xl:col-auto xl:row-auto">
                    <Button size="icon-xs" variant="ghost" onClick={async () => { if (await copyText(record.path)) toast.success(t('log_detail.copy_success')) }} aria-label={t('json_viewer.copy')}><Copy /></Button>
                    <Button size="icon-xs" variant="ghost" onClick={() => void handleAddToAllowlist(record)} aria-label={canAllow ? t('logging_rules.add_to_allowlist') : t('logging_rules.edit_upstream_rules')}><Plus /></Button>
                    <Button size="icon-xs" variant="ghost" onClick={() => void handleDeleteIgnored(record.upstream, record.path)} aria-label={t('common.delete')} className="hover:text-destructive"><Trash2 /></Button>
                  </div>
                </div>
              )
            })}
          </div>

          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>{t('logging_rules.total_count', { count: ignoredTotal })}</span>
            <div className="flex items-center gap-2">
              <Button size="icon-xs" variant="ghost" disabled={currentPage <= 1} onClick={() => setIgnoredFilter(current => ({ ...current, offset: Math.max(0, (current.offset || 0) - (current.limit || 50)) }))}><ChevronLeft /></Button>
              <span>{currentPage} / {pageCount}</span>
              <Button size="icon-xs" variant="ghost" disabled={currentPage >= pageCount} onClick={() => setIgnoredFilter(current => ({ ...current, offset: (current.offset || 0) + (current.limit || 50) }))}><ChevronRight /></Button>
            </div>
          </div>
        </div>
      )}
    </Tabs>
  )
}
