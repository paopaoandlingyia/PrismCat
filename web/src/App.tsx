import { BrowserRouter, Routes, Route, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { Globe, LayoutDashboard, LogOut, Network, Settings as SettingsIcon, Zap } from 'lucide-react'
import { PrismCatLogo } from '@/components/PrismCatLogo'
import { useTranslation } from 'react-i18next'
import { Dashboard } from '@/pages/Dashboard'
import { Traces } from '@/pages/Traces'
import { cn } from '@/lib/utils'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeToggle } from '@/components/ThemeToggle'
import { Toaster } from '@/components/ui/sonner'
import { Suspense, lazy, useState, useEffect } from 'react'
import { fetchAuthStatus, fetchConfig, login, logout as logoutRequest, setupPassword } from '@/lib/api'
import { logRequestDiffRoute } from '@/lib/routes'
import { Login } from '@/pages/Login'

const PlaygroundPage = lazy(async () => {
  const module = await import('@/pages/Playground')
  return { default: module.Playground }
})

const SettingsPage = lazy(async () => {
  const module = await import('@/pages/Settings')
  return { default: module.Settings }
})

const LogDiffPage = lazy(async () => {
  const module = await import('@/pages/LogDiff')
  return { default: module.LogDiff }
})

const TraceDetailPage = lazy(async () => {
  const module = await import('@/pages/TraceDetail')
  return { default: module.TraceDetail }
})

interface AppLayoutProps {
  onSignOut: () => void
}

function AppLayout({ onSignOut }: AppLayoutProps) {
  const { t, i18n } = useTranslation()
  const location = useLocation()
  const [version, setVersion] = useState<string>('v1.4.0') // 初始显式 v1.4.0，直到接口返回

  useEffect(() => {
    fetchConfig()
      .then(cfg => {
        if (cfg.version) {
          setVersion(cfg.version.startsWith('v') ? cfg.version : `v${cfg.version}`)
        }
      })
      .catch(err => console.error('Failed to fetch version:', err))
  }, [])

  useEffect(() => {
    window.scrollTo({ top: 0, left: 0 })
  }, [location.pathname])

  const navItems = [
    { to: '/', labelKey: 'nav.dashboard', icon: LayoutDashboard },
    { to: '/traces', labelKey: 'nav.traces', icon: Network },
    { to: '/playground', labelKey: 'nav.playground', icon: Zap },
    { to: '/settings', labelKey: 'nav.settings', icon: SettingsIcon },
  ]

  const routeFallback = (
    <div className="flex min-h-[40vh] flex-col items-center justify-center gap-4">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      <div className="text-sm font-medium text-muted-foreground">
        {t('common.loading')}
      </div>
    </div>
  )

  return (
    <div className="relative isolate flex min-h-screen flex-col bg-background">
      {/* 头部 */}
      <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur-md">
        <div className="mx-auto w-full max-w-[1600px] px-4 py-3 sm:px-6 sm:py-4">
          <div className="flex items-start justify-between gap-3 sm:items-center">
            <div className="flex min-w-0 items-center gap-3 sm:gap-6">
              {/* Logo */}
              <a
                href="https://github.com/paopaoandlingyia/PrismCat"
                target="_blank"
                rel="noopener noreferrer"
                className="flex min-w-0 items-center gap-2.5 transition-opacity hover:opacity-80 sm:gap-3"
              >
                <div className="relative">
                  <PrismCatLogo className="h-9 w-9" />
                </div>
                <h1 className="truncate text-base font-semibold tracking-tight text-foreground">
                  {t('app.title')}
                </h1>
              </a>

              {/* 导航 */}
              <nav className="hidden md:flex items-center gap-1 ml-4">
                {navItems.map((item) => {
                  const isActive = item.to === '/'
                    ? location.pathname === '/'
                    : location.pathname.startsWith(item.to)
                  const Icon = item.icon
                  return (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      className={cn(
                        'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors',
                        isActive
                          ? 'bg-accent font-medium text-foreground'
                          : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                      )}
                    >
                      <Icon className="h-4 w-4" />
                      <span>{t(item.labelKey)}</span>
                    </NavLink>
                  )
                })}
              </nav>
            </div>

            {/* 右侧操作 */}
            <div className="shrink-0 flex items-center gap-2 sm:gap-4">
              <ThemeToggle />
              <button
                onClick={() => i18n.changeLanguage(i18n.language === 'zh' ? 'en' : 'zh')}
                className="flex h-9 items-center justify-center gap-2 rounded-md border border-border px-3 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <Globe className="h-3.5 w-3.5" />
                <span>{i18n.language === 'zh' ? 'English' : '中文'}</span>
              </button>
              <button
                onClick={onSignOut}
                className="flex h-9 w-9 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                aria-label={t('auth.sign_out')}
                title={t('auth.sign_out')}
              >
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          </div>

          {/* 移动端导航 */}
          <nav className="mt-3 flex items-center gap-1.5 md:hidden sm:mt-4 sm:-mx-2 sm:gap-2">
            {navItems.map((item) => {
              const isActive = item.to === '/'
                ? location.pathname === '/'
                : location.pathname.startsWith(item.to)
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  className={cn(
                    'flex-1 flex items-center justify-center gap-2 rounded-md px-3 py-2 text-xs transition-colors sm:px-4 sm:text-sm',
                    isActive
                      ? 'bg-accent font-medium text-foreground'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                  )}
                >
                  <Icon className="h-5 w-5" />
                  <span>{t(item.labelKey)}</span>
                </NavLink>
              )
            })}
          </nav>
        </div>
      </header>

      {/* 主内容 */}
      <main className="relative z-0 w-full flex-1 bg-background px-4 py-5 space-y-6 sm:px-6 sm:py-6">
        <div className="mx-auto w-full max-w-[1600px]">
        <Suspense fallback={routeFallback}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path={logRequestDiffRoute} element={<LogDiffPage />} />
            <Route path="/traces" element={<Traces />} />
            <Route path="/traces/:traceId" element={<TraceDetailPage />} />
            <Route path="/playground" element={<PlaygroundPage />} />
            <Route path="/settings" element={<SettingsPage />} />
          </Routes>
        </Suspense>
        </div>
      </main>

      {/* 页脚版本号 */}
      <footer className="relative z-0 flex w-full items-center justify-center bg-background px-4 py-4 sm:px-6">
        <p className="select-none text-xs text-muted-foreground/70">
          PrismCat {version}
        </p>
      </footer>
    </div>
  )
}

function AppContent() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [isCheckingAuth, setIsCheckingAuth] = useState(true)
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [setupRequired, setSetupRequired] = useState(false)

  useEffect(() => {
    let cancelled = false
    fetchAuthStatus()
      .then((status) => {
        if (cancelled) return
        setIsAuthenticated(status.authenticated)
        setSetupRequired(status.setup_required)
      })
      .catch((err) => {
        console.error('Failed to fetch auth status:', err)
        if (!cancelled) {
          setIsAuthenticated(false)
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsCheckingAuth(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [])

  const handleSignedIn = async (password: string) => {
    const status = setupRequired
      ? await setupPassword(password)
      : await login(password)
    if (!status.authenticated) {
      throw new Error(t('auth.sign_in_failed'))
    }
    setIsAuthenticated(true)
    setSetupRequired(status.setup_required)
    navigate('/', { replace: true })
  }

  const handleSignOut = async () => {
    await logoutRequest().catch(err => console.error('Failed to sign out:', err))
    setIsAuthenticated(false)
    navigate('/', { replace: true })
  }

  if (isCheckingAuth) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-4">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
        <div className="text-sm font-medium text-muted-foreground">
          {t('common.loading')}
        </div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Login setupRequired={setupRequired} onSignedIn={handleSignedIn} />
  }

  return <AppLayout onSignOut={handleSignOut} />
}

function App() {
  return (
    <BrowserRouter>
      <TooltipProvider>
        <AppContent />
        <Toaster position="top-right" expand={true} richColors />
      </TooltipProvider>
    </BrowserRouter>
  )
}

export default App


