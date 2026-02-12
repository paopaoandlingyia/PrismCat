import { forwardRef } from 'react'
import DatePicker, { registerLocale } from 'react-datepicker'
import { zhCN } from 'date-fns/locale'
import { CalendarDays } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import 'react-datepicker/dist/react-datepicker.css'

// 注册中文 locale
registerLocale('zh', zhCN)

export interface DateTimeValue {
    startTime?: string
    endTime?: string
}

interface DateTimePickerProps {
    value: Date | null
    onChange: (date: Date | null) => void
    placeholder: string
    maxDate?: Date
    minDate?: Date
    placement?: 'bottom-start' | 'bottom-end'
}

// 单个日期时间选择器的自定义输入框
const DateTimeInput = forwardRef<HTMLButtonElement, { value?: string; onClick?: () => void; placeholder?: string }>(
    ({ value, onClick, placeholder }, ref) => (
        <Button
            ref={ref}
            onClick={onClick}
            variant="outline"
            type="button"
            className={cn(
                'flex items-center gap-2.5 px-3 h-10 rounded-lg text-sm transition-all min-w-[170px] border-border/50 bg-background/50',
                'hover:border-primary/50 hover:bg-accent/50',
                !value && 'text-muted-foreground'
            )}
        >
            <CalendarDays className="h-4 w-4 shrink-0 text-primary/60" />
            <span className="font-semibold truncate">
                {value || placeholder}
            </span>
        </Button>
    )
)
DateTimeInput.displayName = 'DateTimeInput'

// 单个日期时间选择器组件
function DateTimePicker({ value, onChange, placeholder, maxDate, minDate, placement = 'bottom-start' }: DateTimePickerProps) {
    const { i18n } = useTranslation()

    return (
        <DatePicker
            selected={value}
            onChange={onChange}
            locale={i18n.language === 'zh' ? 'zh' : undefined}
            dateFormat="yyyy-MM-dd HH:mm"
            timeFormat="HH:mm"
            showTimeSelect
            timeIntervals={15}
            maxDate={maxDate}
            minDate={minDate}
            placeholderText={placeholder}
            customInput={<DateTimeInput />}
            showPopperArrow={false}
            popperClassName="date-picker-popper"
            calendarClassName="date-picker-calendar"
            portalId="datepicker-portal"
            popperPlacement={placement}
        />
    )
}

interface DateRangePickerProps {
    value: DateTimeValue
    onChange: (value: DateTimeValue) => void
}

export function DateRangePicker({ value, onChange }: DateRangePickerProps) {
    const { t } = useTranslation()

    const startDate = value.startTime ? new Date(value.startTime) : null
    const endDate = value.endTime ? new Date(value.endTime) : null

    const handleStartChange = (date: Date | null) => {
        onChange({
            ...value,
            startTime: date ? date.toISOString() : undefined,
        })
    }

    const handleEndChange = (date: Date | null) => {
        onChange({
            ...value,
            endTime: date ? date.toISOString() : undefined,
        })
    }

    return (
        <div className="flex items-center gap-2">
            <DateTimePicker
                value={startDate}
                onChange={handleStartChange}
                placeholder={t('filters.start_time', '开始时间')}
                maxDate={endDate || new Date()}
                placement="bottom-start"
            />
            <span className="text-muted-foreground/30 text-sm font-bold mx-1">/</span>
            <DateTimePicker
                value={endDate}
                onChange={handleEndChange}
                placeholder={t('filters.end_time', '结束时间')}
                minDate={startDate || undefined}
                maxDate={new Date()}
                placement="bottom-end"
            />
        </div>
    )
}
