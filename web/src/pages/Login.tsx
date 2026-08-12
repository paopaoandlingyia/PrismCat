import { useState } from 'react'
import type { FormEvent } from 'react'
import { ArrowRight, Eye, EyeOff, Globe, LockKeyhole, Server, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PrismCatLogo } from '@/components/PrismCatLogo'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ThemeToggle } from '@/components/ThemeToggle'

interface LoginProps {
  setupRequired: boolean
  onSignedIn: (password: string) => Promise<void>
}

export function Login({ setupRequired, onSignedIn }: LoginProps) {
  const { t, i18n } = useTranslation()
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!password.trim()) {
      setError(t('auth.password_required'))
      return
    }
    if (setupRequired && password !== confirmPassword) {
      setError(t('auth.password_mismatch'))
      return
    }
    setError('')
    setIsSubmitting(true)
    try {
      await onSignedIn(password)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth.sign_in_failed'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex w-full items-center justify-between px-4 py-4 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <PrismCatLogo className="h-9 w-9" />
          <div className="min-w-0">
            <p className="truncate text-lg font-semibold prism-gradient-text">
              {t('app.title')}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <ThemeToggle />
          <button
            onClick={() => i18n.changeLanguage(i18n.language === 'zh' ? 'en' : 'zh')}
            className="flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-accent/50 px-3 text-xs font-semibold text-muted-foreground transition-all hover:border-border hover:bg-accent hover:text-foreground active:scale-95 sm:min-w-[110px] sm:px-4"
            aria-label={t('auth.switch_language')}
          >
            <Globe className="h-3.5 w-3.5" />
            <span>{i18n.language === 'zh' ? 'English' : '中文'}</span>
          </button>
        </div>
      </header>

      <main className="grid min-h-[calc(100vh-76px)] place-items-center px-4 py-8 sm:px-6">
        <div className="grid w-full max-w-5xl overflow-hidden rounded-lg border border-border bg-card md:grid-cols-[1fr_420px]">
          <section className="relative hidden min-h-[560px] overflow-hidden border-r border-border bg-muted/40 md:block">
            <div className="relative flex h-full flex-col justify-between p-10">
              <div>
                <div className="mb-8 inline-flex h-12 w-12 items-center justify-center rounded-lg border border-border bg-card">
                  <ShieldCheck className="h-6 w-6 text-muted-foreground" />
                </div>
                <h1 className="max-w-md text-3xl font-semibold leading-tight">
                  {t('auth.hero_title')}
                </h1>
                <p className="mt-4 max-w-md text-sm leading-6 text-muted-foreground">
                  {t('auth.hero_description')}
                </p>
              </div>

              <div className="grid gap-3">
                <div className="flex items-center justify-between rounded-lg border border-border bg-card px-4 py-3">
                  <div className="flex items-center gap-3">
                    <Server className="h-4 w-4 text-success" />
                    <span className="text-sm font-medium">{t('auth.server_ready')}</span>
                  </div>
                  <span className="h-2 w-2 rounded-full bg-success" />
                </div>
                <div className="flex items-center justify-between rounded-lg border border-border bg-card px-4 py-3">
                  <div className="flex items-center gap-3">
                    <LockKeyhole className="h-4 w-4 text-warning" />
                    <span className="text-sm font-medium">{t('auth.session_ready')}</span>
                  </div>
                  <span className="h-2 w-2 rounded-full bg-warning" />
                </div>
              </div>
            </div>
          </section>

          <section className="flex min-h-[540px] flex-col justify-center px-5 py-8 sm:px-8">
            <div className="mx-auto w-full max-w-sm">
              <div className="mb-8">
                <div className="mb-5 flex h-12 w-12 items-center justify-center rounded-lg border border-border bg-accent/60 md:hidden">
                  <ShieldCheck className="h-6 w-6 text-primary" />
                </div>
                <p className="text-sm font-semibold text-primary">
                  {t('auth.welcome_back')}
                </p>
                <h2 className="mt-2 text-2xl font-semibold tracking-tight">
                  {setupRequired ? t('auth.setup_title') : t('auth.sign_in_title')}
                </h2>
                <p className="mt-3 text-sm leading-6 text-muted-foreground">
                  {setupRequired ? t('auth.setup_description') : t('auth.sign_in_description')}
                </p>
              </div>

              <form className="space-y-5" onSubmit={handleSubmit}>
                <div className="space-y-2">
                  <Label htmlFor="password">{t('auth.password')}</Label>
                  <div className="relative">
                    <LockKeyhole className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      id="password"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(event) => {
                        setPassword(event.target.value)
                        if (error) setError('')
                      }}
                      className="h-11 pl-9 pr-11"
                      placeholder={setupRequired ? t('auth.new_password_placeholder') : t('auth.password_placeholder')}
                      aria-invalid={error ? 'true' : 'false'}
                      autoComplete={setupRequired ? 'new-password' : 'current-password'}
                      disabled={isSubmitting}
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword((value) => !value)}
                      className="absolute right-2 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                      aria-label={showPassword ? t('auth.hide_password') : t('auth.show_password')}
                    >
                      {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                </div>

                {setupRequired ? (
                  <div className="space-y-2">
                    <Label htmlFor="confirm-password">{t('auth.confirm_password')}</Label>
                    <div className="relative">
                      <LockKeyhole className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                      <Input
                        id="confirm-password"
                        type={showPassword ? 'text' : 'password'}
                        value={confirmPassword}
                        onChange={(event) => {
                          setConfirmPassword(event.target.value)
                          if (error) setError('')
                        }}
                        className="h-11 pl-9"
                        placeholder={t('auth.confirm_password_placeholder')}
                        aria-invalid={error ? 'true' : 'false'}
                        autoComplete="new-password"
                        disabled={isSubmitting}
                      />
                    </div>
                  </div>
                ) : null}

                <div>
                  {error ? (
                    <p className="text-sm font-medium text-destructive">{error}</p>
                  ) : null}
                </div>

                <Button type="submit" size="lg" className="h-11 w-full" disabled={isSubmitting}>
                  {setupRequired ? t('auth.setup_submit') : t('auth.sign_in')}
                  <ArrowRight className="h-4 w-4" />
                </Button>
              </form>

              <div className="mt-8 border-t border-border pt-5">
                <div className="flex items-start gap-3 text-sm text-muted-foreground">
                  <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                  <p className="leading-6">{t('auth.security_note')}</p>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main>
    </div>
  )
}
