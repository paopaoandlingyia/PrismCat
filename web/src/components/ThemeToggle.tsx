import { Moon, Sun } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useEffect, useState } from 'react'

function getInitialDarkMode() {
    return typeof localStorage === 'undefined' || localStorage.getItem('theme') !== 'light'
}

function applyTheme(isDark: boolean) {
    document.documentElement.classList.toggle('dark', isDark)
    localStorage.setItem('theme', isDark ? 'dark' : 'light')
}

export function ThemeToggle() {
    const [isDark, setIsDark] = useState(getInitialDarkMode)

    useEffect(() => {
        applyTheme(isDark)
    }, [isDark])

    const toggleTheme = () => {
        setIsDark((current) => !current)
    }

    return (
        <Button
            variant="ghost"
            size="icon"
            onClick={toggleTheme}
            className="rounded-full w-9 h-9 hover:bg-white/10"
            title={isDark ? '切换到亮色模式' : '切换到暗色模式'}
        >
            {isDark ? (
                <Sun className="h-[1.2rem] w-[1.2rem] text-yellow-500" />
            ) : (
                <Moon className="h-[1.2rem] w-[1.2rem] text-blue-500" />
            )}
        </Button>
    )
}
