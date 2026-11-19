import { useState, type FormEvent } from 'react'
import { ArrowRight, Check, CloudCog, KeyRound, LoaderCircle, LockKeyhole, Mail, ShieldCheck, UserRound } from 'lucide-react'
import { api, ApiError, type User } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

type Props = {
  setupRequired: boolean
  onAuthenticated: (user: User) => void
}

export default function AuthPage({ setupRequired, onAuthenticated }: Props) {
  const setup = setupRequired
  const [displayName, setDisplayName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError('')
    if (setup && password !== confirmation) {
      setError('Пароли не совпадают')
      return
    }
    if (setup && password.length < 12) {
      setError('Пароль должен содержать не менее 12 символов')
      return
    }
    setPending(true)
    try {
      const result = setup
        ? await api.setup({ displayName, email, password })
        : await api.login({ email, password })
      onAuthenticated(result.user)
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : 'Нет связи с Management API')
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="grid min-h-screen bg-background text-foreground lg:grid-cols-[1fr_1.05fr]">
      <section className="relative hidden overflow-hidden border-r border-border bg-card/40 p-12 lg:flex lg:flex-col lg:justify-between">
        <div className="flex items-center gap-3">
          <div className="flex size-11 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <CloudCog className="size-6" aria-hidden="true" />
          </div>
          <div><div className="font-semibold">PVE</div><div className="text-sm text-muted-foreground">Foundry</div></div>
        </div>
        <div className="max-w-xl">
          <div className="mb-7 flex size-14 items-center justify-center rounded-xl border border-primary/25 bg-primary/10 text-primary">
            <ShieldCheck className="size-7" aria-hidden="true" />
          </div>
          <h1 className="text-5xl font-semibold leading-tight tracking-[-0.045em]">Ваша инфраструктура.<br />Под вашим контролем.</h1>
          <p className="mt-6 max-w-lg text-lg leading-8 text-muted-foreground">Единая локальная панель для проектирования ВМ, LXC и сетей с прозрачным планом изменений.</p>
          <ul className="mt-10 space-y-4 text-sm">
            {['Пароли защищены алгоритмом Argon2id', 'Сессии хранятся на сервере', 'Изменения требуют CSRF-подтверждения'].map((item) => (
              <li key={item} className="flex items-center gap-3"><span className="flex size-5 items-center justify-center rounded-full bg-primary/15 text-primary"><Check className="size-3" /></span>{item}</li>
            ))}
          </ul>
        </div>
        <p className="text-xs text-muted-foreground">Локальный контур · без внешней публикации</p>
      </section>

      <section className="flex min-h-screen items-center justify-center px-5 py-10 sm:px-10">
        <div className="w-full max-w-md">
          <div className="mb-10 flex items-center gap-3 lg:hidden">
            <CloudCog className="size-7 text-primary" /><span className="font-semibold">PVE Foundry</span>
          </div>
          <div className="mb-8">
            <div className="mb-5 inline-flex items-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-3 py-1 text-sm text-primary">
              {setup ? <ShieldCheck className="size-4" /> : <LockKeyhole className="size-4" />}
              {setup ? 'Первый запуск' : 'Защищённая область'}
            </div>
            <h2 className="text-3xl font-semibold tracking-[-0.035em]">{setup ? 'Создайте администратора' : 'С возвращением'}</h2>
            <p className="mt-3 leading-7 text-muted-foreground">{setup ? 'Одна учётная запись для первоначальной настройки. Стандартного пароля нет.' : 'Войдите, чтобы продолжить работу с локальной панелью.'}</p>
          </div>

          <form onSubmit={submit} className="space-y-5" noValidate>
            {setup && <div className="space-y-2">
              <Label htmlFor="displayName">Ваше имя</Label>
              <div className="relative"><UserRound className="absolute left-3 top-3 size-4 text-muted-foreground" /><Input id="displayName" className="h-11 pl-10" value={displayName} onChange={(e) => setDisplayName(e.target.value)} autoComplete="name" required maxLength={100} placeholder="Администратор" /></div>
            </div>}
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <div className="relative"><Mail className="absolute left-3 top-3 size-4 text-muted-foreground" /><Input id="email" type="email" className="h-11 pl-10" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" required placeholder="admin@example.local" /></div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Пароль</Label>
              <div className="relative"><KeyRound className="absolute left-3 top-3 size-4 text-muted-foreground" /><Input id="password" type="password" className="h-11 pl-10" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete={setup ? 'new-password' : 'current-password'} required minLength={setup ? 12 : undefined} maxLength={128} placeholder={setup ? 'Минимум 12 символов' : 'Ваш пароль'} /></div>
            </div>
            {setup && <div className="space-y-2">
              <Label htmlFor="confirmation">Повторите пароль</Label>
              <Input id="confirmation" type="password" className="h-11" value={confirmation} onChange={(e) => setConfirmation(e.target.value)} autoComplete="new-password" required minLength={12} maxLength={128} />
            </div>}
            {error && <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm leading-6 text-foreground">{error}</div>}
            <Button type="submit" className="h-11 w-full gap-2" disabled={pending}>
              {pending ? <LoaderCircle className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
              {pending ? 'Проверяем…' : setup ? 'Создать и войти' : 'Войти в панель'}
            </Button>
          </form>
        </div>
      </section>
    </main>
  )
}
