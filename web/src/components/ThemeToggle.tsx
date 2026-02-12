import { Moon, Sun } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useEffect, useState } from 'react'

export function ThemeToggle() {
    const [isDark, setIsDark] = useState(true) // 默认暗色

    useEffect(() => {
        // 初始化
        const isDarkStored = localStorage.getItem('theme') !== 'light'
        setIsDark(isDarkStored)
        if (isDarkStored) {
            document.documentElement.classList.add('dark')
        } else {
            document.documentElement.classList.remove('dark')
        }
    }, [])

    const toggleTheme = () => {
        const newDark = !isDark
        setIsDark(newDark)
        if (newDark) {
            document.documentElement.classList.add('dark')
            localStorage.setItem('theme', 'dark')
        } else {
            document.documentElement.classList.remove('dark')
            localStorage.setItem('theme', 'light')
        }
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
