import { useEffect, useState } from 'react'
import { LoaderCircle, RefreshCw, ServerCrash } from 'lucide-react'
import Home from './pages/Home'
import AuthPage from './pages/AuthPage'
import { api, type User } from './lib/api'
import { Button } from './components/ui/button'

export default function App() {
  const [setupRequired, setSetupRequired] = useState<boolean | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  async function initialize() {
    setLoading(true)
    setLoadError('')
    try {
      const status = await api.status()
      setSetupRequired(status.required)
      if (status.authenticated) {
        const session = await api.session()
        setUser(session.user)
      } else {
        setUser(null)
      }
    } catch {
      setLoadError('Management API недоступен. Запустите сервер и PostgreSQL, затем повторите попытку.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void initialize() }, [])

  if (loading) {
    return <main className="flex min-h-screen items-center justify-center bg-background text-foreground"><div className="text-center"><LoaderCircle className="mx-auto size-7 animate-spin text-primary" /><p className="mt-4 text-sm text-muted-foreground">Проверяем состояние панели…</p></div></main>
  }
  if (loadError) {
    return <main className="flex min-h-screen items-center justify-center bg-background px-5 text-foreground"><div className="max-w-md text-center"><ServerCrash className="mx-auto size-10 text-primary" /><h1 className="mt-6 text-2xl font-semibold">Панель пока недоступна</h1><p className="mt-3 leading-7 text-muted-foreground">{loadError}</p><Button onClick={() => void initialize()} className="mt-6 gap-2"><RefreshCw className="size-4" />Повторить подключение</Button></div></main>
  }
  if (!user) {
    return <AuthPage setupRequired={setupRequired ?? false} onAuthenticated={(authenticatedUser) => { setSetupRequired(false); setUser(authenticatedUser) }} />
  }
  return <Home user={user} onLogout={async () => { await api.logout(); setUser(null) }} />
}
