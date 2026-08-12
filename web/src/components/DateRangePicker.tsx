import { forwardRef } from 'react'
import DatePicker, { registerLocale } from 'react-datepicker'
import { zhCN } from 'date-fns/locale'
import { CalendarDays } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import 'react-datepicker/dist/react-datepicker.css'

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

const DateTimeInput = forwardRef<HTMLButtonElement, { value?: string; onClick?: () => void; placeholder?: string }>(
    ({ value, onClick, placeholder }, ref) => (
        <Button
            ref={ref}
            onClick={onClick}
            variant="outline"
            type="button"
            className={cn(
                'flex w-full items-center justify-start gap-2 px-2.5 h-8 rounded-md text-xs transition-colors min-w-0 sm:min-w-[150px] border-input bg-background',
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
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
            <DateTimePicker
                value={startDate}
                onChange={handleStartChange}
                placeholder={t('filters.start_time')}
                maxDate={endDate || new Date()}
                placement="bottom-start"
            />
            <span className="hidden text-muted-foreground/30 text-sm font-medium mx-1 sm:inline">/</span>
            <DateTimePicker
                value={endDate}
                onChange={handleEndChange}
                placeholder={t('filters.end_time')}
                minDate={startDate || undefined}
                maxDate={new Date()}
                placement="bottom-end"
            />
        </div>
    )
}
