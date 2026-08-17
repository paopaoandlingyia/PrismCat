import { useState } from 'react'
import { WandSparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { LoggingPathFilter, LoggingPathMode, ModelPathTemplate } from '@/lib/api'
import { mergeLoggingPathRules } from '@/lib/loggingRules'
import { LoggingPathRuleEditor } from '@/components/LoggingPathRuleEditor'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'

interface UpstreamLoggingPathFilterProps {
  value: LoggingPathFilter
  onChange: (value: LoggingPathFilter) => void
  templates: ModelPathTemplate[]
  disabled?: boolean
}

export function UpstreamLoggingPathFilter({ value, onChange, templates, disabled = false }: UpstreamLoggingPathFilterProps) {
  const { t } = useTranslation()
  const [templateTag, setTemplateTag] = useState('')
  const selectedTemplateTag = templates.some(template => template.tag === templateTag)
    ? templateTag
    : templates[0]?.tag || ''

  const setMode = (mode: LoggingPathMode) => onChange({ ...value, mode })
  const fillTemplate = () => {
    const template = templates.find(item => item.tag === selectedTemplateTag)
    if (!template) return
    const merged = mergeLoggingPathRules(value.rules || [], template.rules)
    onChange({ ...value, rules: merged.rules })
    toast.success(t('logging_rules.fill_result', { added: merged.added, skipped: merged.skipped }))
  }

  return (
    <div className={cn('space-y-3', disabled && 'opacity-60')}>
      <div className="inline-flex max-w-full items-center gap-0.5 rounded-lg bg-muted p-[3px]" role="group" aria-label={t('upstream_manager.logging_path_mode')}>
        {(['all', 'allowlist', 'denylist'] as LoggingPathMode[]).map(mode => (
          <button
            key={mode}
            type="button"
            disabled={disabled}
            aria-pressed={value.mode === mode}
            onClick={() => setMode(mode)}
            className={cn(
              'min-w-[76px] rounded-md px-3 py-1.5 text-xs transition-colors',
              value.mode === mode ? 'bg-background font-medium text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {t(`upstream_manager.logging_mode_${mode}`)}
          </button>
        ))}
      </div>

      {value.mode !== 'all' && (
        <>
          {templates.length > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              <Select value={selectedTemplateTag} onValueChange={setTemplateTag} disabled={disabled}>
                <SelectTrigger className="h-8 w-[170px] text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>{templates.map(template => <SelectItem key={template.tag} value={template.tag}>{template.tag}</SelectItem>)}</SelectContent>
              </Select>
              <Button type="button" size="sm" variant="outline" disabled={disabled || !selectedTemplateTag} onClick={fillTemplate}>
                <WandSparkles />
                {t('logging_rules.fill')}
              </Button>
            </div>
          )}
          <LoggingPathRuleEditor rules={value.rules || []} onChange={rules => onChange({ ...value, rules })} disabled={disabled} compact />
        </>
      )}
    </div>
  )
}
