import { Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useEffect, useState } from 'react'

function getInitialDarkMode() {
    return typeof localStorage === 'undefined' || localStorage.getItem('theme') !== 'light'
}

function applyTheme(isDark: boolean) {
    document.documentElement.classList.toggle('dark', isDark)
    localStorage.setItem('theme', isDark ? 'dark' : 'light')
}

export function ThemeToggle() {
    const { t } = useTranslation()
    const [isDark, setIsDark] = useState(getInitialDarkMode)

    useEffect(() => {
        applyTheme(isDark)
    }, [isDark])

    const toggleTheme = () => {
        setIsDark((current) => !current)
    }

    const label = isDark ? t('common.theme_to_light') : t('common.theme_to_dark')

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    variant="ghost"
                    size="icon"
                    onClick={toggleTheme}
                    className="h-9 w-9 rounded-md hover:bg-accent"
                    aria-label={label}
                >
                    {isDark ? (
                        <Sun className="h-[1.2rem] w-[1.2rem]" />
                    ) : (
                        <Moon className="h-[1.2rem] w-[1.2rem]" />
                    )}
                </Button>
            </TooltipTrigger>
            <TooltipContent>{label}</TooltipContent>
        </Tooltip>
    )
}
