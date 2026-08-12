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

  const isNavActive = (to: string) =>
    to === '/' ? location.pathname === '/' : location.pathname.startsWith(to)

  const pageTitle = navItems.find(item => isNavActive(item.to))?.labelKey ?? 'app.title'

  const routeFallback = (
    <div className="flex min-h-[40vh] flex-col items-center justify-center gap-3">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      <div className="text-sm text-muted-foreground">{t('common.loading')}</div>
    </div>
  )

  return (
    <div className="min-h-screen bg-background">
      {/* 侧边栏 - 桌面 */}
      <aside
        className="fixed inset-y-0 left-0 z-40 hidden w-[200px] flex-col border-r border-border bg-muted/40 md:flex"
      >
        <a
          href="https://github.com/paopaoandlingyia/PrismCat"
          target="_blank"
          rel="noopener noreferrer"
          className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-3 transition-colors hover:bg-accent/60"
        >
          <PrismCatLogo className="h-5 w-5 shrink-0" />
          <span className="truncate text-sm font-medium text-foreground">{t('app.title')}</span>
        </a>

        <nav className="flex-1 space-y-0.5 overflow-y-auto p-2">
          {navItems.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                to={item.to}
                className={cn(
                  'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm transition-colors',
                  isNavActive(item.to)
                    ? 'bg-accent font-medium text-foreground'
                    : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                )}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="truncate">{t(item.labelKey)}</span>
              </NavLink>
            )
          })}
        </nav>

        <div className="shrink-0 border-t border-border p-2">
          <div className="flex items-center gap-1">
            <ThemeToggle />
            <button
              onClick={() => i18n.changeLanguage(i18n.language === 'zh' ? 'en' : 'zh')}
              className="flex h-8 flex-1 items-center justify-center gap-1.5 rounded-md text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label={t('auth.switch_language')}
            >
              <Globe className="h-3.5 w-3.5" />
              <span>{i18n.language === 'zh' ? 'EN' : '中'}</span>
            </button>
            <button
              onClick={onSignOut}
              className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label={t('auth.sign_out')}
              title={t('auth.sign_out')}
            >
              <LogOut className="h-4 w-4" />
            </button>
          </div>
          <p className="mt-2 select-none px-1 text-xs text-muted-foreground/70">PrismCat {version}</p>
        </div>
      </aside>

      <div className="flex min-h-screen flex-col md:pl-[200px]">
        {/* 移动端顶栏 */}
        <header className="sticky top-0 z-30 border-b border-border bg-background/90 backdrop-blur-md md:hidden">
          <div className="flex items-center justify-between gap-3 px-4 py-2.5">
            <div className="flex min-w-0 items-center gap-2">
              <PrismCatLogo className="h-5 w-5 shrink-0" />
              <span className="truncate text-sm font-medium">{t('app.title')}</span>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <ThemeToggle />
              <button
                onClick={() => i18n.changeLanguage(i18n.language === 'zh' ? 'en' : 'zh')}
                className="flex h-8 items-center justify-center gap-1.5 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                aria-label={t('auth.switch_language')}
              >
                <Globe className="h-3.5 w-3.5" />
                <span>{i18n.language === 'zh' ? 'EN' : '中'}</span>
              </button>
              <button
                onClick={onSignOut}
                className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                aria-label={t('auth.sign_out')}
                title={t('auth.sign_out')}
              >
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          </div>
          <nav className="flex items-center gap-1 overflow-x-auto px-2 pb-2">
            {navItems.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  className={cn(
                    'flex flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-3 py-1.5 text-xs transition-colors',
                    isNavActive(item.to)
                      ? 'bg-accent font-medium text-foreground'
                      : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
                  )}
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  <span>{t(item.labelKey)}</span>
                </NavLink>
              )
            })}
          </nav>
        </header>

        {/* 页面标题栏 - 给内容区一个"顶" */}
        <div className="sticky top-0 z-20 hidden h-12 shrink-0 items-center border-b border-border bg-background/90 px-5 backdrop-blur-md md:flex">
          <h1 className="text-sm font-medium text-foreground">{t(pageTitle)}</h1>
        </div>

        {/* 主内容 */}
        <main className="w-full flex-1 px-4 py-4 sm:px-5">
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
      </div>
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
      <div className="flex min-h-screen flex-col items-center justify-center gap-3">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
        <div className="text-sm text-muted-foreground">{t('common.loading')}</div>
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
