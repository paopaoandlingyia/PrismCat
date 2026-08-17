import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { LoggingPathRule, PathMatcher } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

interface LoggingPathRuleEditorProps {
    rules: LoggingPathRule[]
    onChange: (rules: LoggingPathRule[]) => void
    disabled?: boolean
    compact?: boolean
}

export function LoggingPathRuleEditor({ rules, onChange, disabled = false, compact = false }: LoggingPathRuleEditorProps) {
    const { t } = useTranslation()

    const updateRule = (index: number, update: Partial<LoggingPathRule>) => {
        onChange(rules.map((rule, current) => current === index ? { ...rule, ...update } : rule))
    }

    return (
        <div className="space-y-2">
            {rules.map((rule, index) => (
                <div key={`${index}-${rule.matcher}`} className="flex min-w-0 items-center gap-2">
                    <Select
                        value={rule.matcher}
                        onValueChange={value => updateRule(index, { matcher: value as PathMatcher })}
                        disabled={disabled}
                    >
                        <SelectTrigger className={compact ? 'h-8 w-[92px] text-xs' : 'h-9 w-[104px] text-xs'}>
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="ant">Ant</SelectItem>
                            <SelectItem value="regex">Regex</SelectItem>
                        </SelectContent>
                    </Select>
                    <Input
                        value={rule.pattern}
                        onChange={event => updateRule(index, { pattern: event.target.value })}
                        placeholder={rule.matcher === 'regex' ? '^/v1/.+$' : '/v1/**'}
                        disabled={disabled}
                        className={`${compact ? 'h-8' : 'h-9'} min-w-0 flex-1 font-mono text-xs`}
                    />
                    <Button
                        type="button"
                        variant="ghost"
                        size={compact ? 'icon-xs' : 'icon-sm'}
                        disabled={disabled}
                        aria-label={t('logging_rules.remove_rule')}
                        onClick={() => onChange(rules.filter((_, current) => current !== index))}
                        className="text-muted-foreground hover:text-destructive"
                    >
                        <Trash2 />
                    </Button>
                </div>
            ))}
            <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={() => onChange([...rules, { matcher: 'ant', pattern: '' }])}
            >
                <Plus className="h-3.5 w-3.5" />
                {t('logging_rules.add_path')}
            </Button>
        </div>
    )
}
