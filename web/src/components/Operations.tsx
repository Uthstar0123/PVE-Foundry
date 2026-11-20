import { useCallback, useEffect, useRef, useState } from 'react'
import { AlertTriangle, Ban, CheckCircle2, CircleDashed, ClipboardList, Download, History, LoaderCircle, RefreshCw, ScrollText, ShieldCheck, XCircle } from 'lucide-react'
import { api } from '@/lib/api'
import type { AuditEvent, Job, JobDetail } from '@/lib/api'
import { DeploymentForm, PlanApproval } from './Deployments'

const statusLabels: Record<string, string> = {
  queued: 'В очереди', waiting_approval: 'Ждёт подтверждения', running: 'Выполняется',
  succeeded: 'Завершена', failed: 'Ошибка', cancelled: 'Отменена', pending: 'Ожидает',
  skipped: 'Пропущен', lease_expired: 'Lease истёк', cancelled_by_user: 'Отменено пользователем',
}

function Label({ value }: { value: string }) {
  const color = value === 'failed' ? 'text-destructive' : value === 'succeeded' ? 'text-primary' : value === 'running' ? 'text-amber-500' : 'text-muted-foreground'
  return <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${color}`}><span className="size-1.5 rounded-full bg-current" />{statusLabels[value] ?? value}</span>
}

function time(value?: string) {
  return value ? new Date(value).toLocaleString('ru-RU') : '—'
}

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error ? cause.message : fallback
}

export function JobsPanel({ active }: { active: boolean }) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [detail, setDetail] = useState<JobDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [error, setError] = useState('')
  const [streamState, setStreamState] = useState<'idle' | 'connected' | 'reconnecting'>('idle')
  const loaded = useRef(false)

  const loadJobs = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const result = await api.jobs()
      setJobs(result.jobs)
      loaded.current = true
      setDetail((current) => current ? current : null)
    } catch (cause) {
      setError(errorMessage(cause, 'Не удалось получить задачи'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { if (active && !loaded.current) void loadJobs() }, [active, loadJobs])

  const selectJob = useCallback(async (id: string) => {
    setDetailLoading(true)
    setError('')
    try { setDetail(await api.job(id)) }
    catch (cause) { setError(errorMessage(cause, 'Не удалось получить задачу')) }
    finally { setDetailLoading(false) }
  }, [])

  const detailID = detail?.job.id
  const detailStatus = detail?.job.status
  useEffect(() => {
    if (!active || !detailID || !detailStatus || ['succeeded', 'failed', 'cancelled'].includes(detailStatus)) {
      setStreamState('idle')
      return
    }
    let disposed = false
    let socket: WebSocket | undefined
    let retry: ReturnType<typeof setTimeout> | undefined
    const connect = () => {
      if (disposed) return
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      socket = new WebSocket(`${protocol}//${window.location.host}/ws/jobs/${detailID}`)
      socket.onopen = () => { if (!disposed) setStreamState('connected') }
      socket.onmessage = (event) => {
        if (disposed) return
        try {
          const snapshot = JSON.parse(String(event.data)) as JobDetail
          setDetail(snapshot)
          setJobs((current) => current.map((job) => job.id === snapshot.job.id ? snapshot.job : job))
        } catch { setError('Получен некорректный снимок задачи') }
      }
      socket.onerror = () => { if (!disposed) setStreamState('reconnecting') }
      socket.onclose = () => {
        if (!disposed) {
          setStreamState('reconnecting')
          retry = setTimeout(connect, 2000)
        }
      }
    }
    connect()
    return () => {
      disposed = true
      if (retry) clearTimeout(retry)
      socket?.close()
    }
  }, [active, detailID, detailStatus])

  const cancel = async () => {
    if (!detail || !window.confirm('Запросить отмену задачи? Уже выполненные изменения не откатываются автоматически.')) return
    setError('')
    try {
      const result = await api.cancelJob(detail.job.id)
      setDetail((current) => current ? { ...current, job: result.job } : current)
      setJobs((current) => current.map((job) => job.id === result.job.id ? result.job : job))
    } catch (cause) { setError(errorMessage(cause, 'Не удалось запросить отмену')) }
  }

  const cancellable = detail && ['queued', 'waiting_approval', 'running'].includes(detail.job.status) && !detail.job.cancelRequestedAt

  return (
    <section id="jobs" className="scroll-mt-6 border-t border-border py-8" aria-labelledby="jobs-title">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div><h2 id="jobs-title" className="flex items-center gap-2 text-xl font-semibold tracking-tight"><ClipboardList className="size-5 text-primary" aria-hidden="true" />Очередь задач</h2><p className="mt-1 text-sm text-muted-foreground">Состояние сохраняется в PostgreSQL и переживает перезапуск Management.</p></div>
        <button type="button" onClick={() => void loadJobs()} disabled={loading} className="inline-flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md border border-border bg-card px-4 text-sm font-medium hover:bg-secondary disabled:opacity-60"><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />Синхронизировать задачи</button>
      </div>
      {error && <div role="alert" className="mt-5 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-sm"><AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" />{error}</div>}
      {active && <DeploymentForm onCreated={(job) => { void loadJobs(); void selectJob(job.id) }} />}
      {!active ? <div className="mt-5 rounded-lg border border-dashed border-border p-6 text-sm text-muted-foreground">Перейдите в этот раздел через меню, чтобы загрузить задачи.</div> : (
        <div className="mt-5 grid gap-4 xl:grid-cols-[minmax(280px,0.8fr)_minmax(0,1.5fr)]">
          <div className="overflow-hidden rounded-lg border border-border bg-card">
            {loading && jobs.length === 0 ? <div role="status" className="p-6 text-sm text-muted-foreground"><LoaderCircle className="mb-2 size-5 animate-spin text-primary" />Загружаем задачи…</div> : jobs.length === 0 ? (
              <div className="p-6"><CircleDashed className="size-6 text-muted-foreground" /><h3 className="mt-3 font-medium">Задач пока нет</h3><p className="mt-2 text-sm leading-6 text-muted-foreground">Загрузите OpenTofu-архив выше или дождитесь подключения Runner для выполнения существующих задач.</p></div>
            ) : jobs.map((job) => (
              <button key={job.id} type="button" onClick={() => void selectJob(job.id)} className={`flex min-h-20 w-full cursor-pointer flex-col gap-2 border-b border-border px-4 py-3 text-left last:border-b-0 hover:bg-secondary/50 ${detail?.job.id === job.id ? 'bg-secondary/60' : ''}`}>
                <span className="flex w-full items-center justify-between gap-3"><span className="font-mono text-sm font-medium">{job.operation}</span><Label value={job.status} /></span>
                <span className="break-all text-xs text-muted-foreground">{job.resourceId} · {time(job.createdAt)}</span>
              </button>
            ))}
          </div>
          <div className="min-w-0 rounded-lg border border-border bg-card p-5 sm:p-6">
            {detailLoading ? <div role="status" className="flex min-h-40 items-center justify-center"><LoaderCircle className="size-6 animate-spin text-primary" /></div> : !detail ? <div className="flex min-h-40 flex-col items-center justify-center text-center"><ScrollText className="size-6 text-muted-foreground" /><p className="mt-3 text-sm text-muted-foreground">Выберите задачу, чтобы увидеть шаги и журнал.</p></div> : (
              <>
                <div className="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0"><div className="flex flex-wrap items-center gap-3"><h3 className="font-mono text-lg font-semibold">{detail.job.operation}</h3><Label value={detail.job.status} /></div><p className="mt-2 break-all text-sm text-muted-foreground">{detail.job.resourceId}</p><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{detail.job.id}</p></div>
                  <div className="flex shrink-0 flex-wrap gap-2">
                    <a href={api.jobLogsURL(detail.job.id)} download={`job-${detail.job.id}.log`} className="inline-flex min-h-10 items-center gap-2 rounded-md border border-border px-3 text-sm hover:bg-secondary"><Download className="size-4" />Скачать лог</a>
                    {cancellable && <button type="button" onClick={() => void cancel()} className="inline-flex min-h-10 cursor-pointer items-center gap-2 rounded-md border border-destructive/40 px-3 text-sm text-destructive hover:bg-destructive/10"><Ban className="size-4" />Отменить</button>}
                  </div>
                </div>
                {detail.job.cancelRequestedAt && <div className="mt-4 flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm"><AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" /><span>Отмена запрошена. Это не rollback: уже выполненные изменения могут остаться в Proxmox.</span></div>}
                {detail.job.errorCode && <div className="mt-4 flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm"><XCircle className="mt-0.5 size-4 shrink-0 text-destructive" /><span>{statusLabels[detail.job.errorCode] ?? detail.job.errorCode}. Результат внешней операции может быть неизвестен; автоматический повтор запрещён.</span></div>}
                <dl className="mt-5 grid grid-cols-2 gap-4 text-sm sm:grid-cols-3"><div><dt className="text-xs text-muted-foreground">Создана</dt><dd className="mt-1">{time(detail.job.createdAt)}</dd></div><div><dt className="text-xs text-muted-foreground">Запущена</dt><dd className="mt-1">{time(detail.job.startedAt)}</dd></div><div><dt className="text-xs text-muted-foreground">Подписка</dt><dd className="mt-1">{streamState === 'connected' ? 'WebSocket активен' : streamState === 'reconnecting' ? 'Переподключение…' : 'Завершена'}</dd></div></dl>
                <PlanApproval detail={detail} onApproved={(approved) => setDetail(approved)} />
                <div className="mt-6"><h4 className="text-sm font-medium">Шаги</h4>{detail.steps.length === 0 ? <p className="mt-2 text-sm text-muted-foreground">Runner ещё не сообщил шаги.</p> : <ol className="mt-3 space-y-2">{detail.steps.map((step) => <li key={step.id} className="flex flex-col gap-2 rounded-md border border-border p-3 sm:flex-row sm:items-center sm:justify-between"><span className="flex items-center gap-2 text-sm">{step.status === 'succeeded' ? <CheckCircle2 className="size-4 text-primary" /> : <CircleDashed className="size-4 text-muted-foreground" />}{step.name}</span><Label value={step.status} /></li>)}</ol>}</div>
                <div className="mt-6"><div className="flex items-center justify-between gap-3"><h4 className="text-sm font-medium">Журнал</h4><span className="text-xs text-muted-foreground">Последние {detail.events.length} событий</span></div>{detail.job.logsTruncated && <p className="mt-2 text-xs text-amber-500">Хранятся только последние 1000 событий; старые записи удалены ротацией.</p>}<pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background p-4 font-mono text-xs leading-6 text-secondary-foreground">{detail.events.length ? detail.events.map((event) => `${new Date(event.createdAt).toLocaleTimeString('ru-RU')} [${event.level}] ${event.message}`).join('\n') : 'Событий пока нет.'}</pre></div>
              </>
            )}
          </div>
        </div>
      )}
    </section>
  )
}

export function AuditPanel({ active }: { active: boolean }) {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const loaded = useRef(false)
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try { const result = await api.audit(); setEvents(result.events); loaded.current = true }
    catch (cause) { setError(errorMessage(cause, 'Не удалось получить аудит')) }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { if (active && !loaded.current) void load() }, [active, load])
  return (
    <section id="audit" className="scroll-mt-6 border-t border-border py-8" aria-labelledby="audit-title">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between"><div><h2 id="audit-title" className="flex items-center gap-2 text-xl font-semibold"><History className="size-5 text-primary" />Аудит действий</h2><p className="mt-1 text-sm text-muted-foreground">Входы, создание задач, отмена, завершение и истечение leases.</p></div><button type="button" onClick={() => void load()} disabled={loading} className="inline-flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md border border-border bg-card px-4 text-sm hover:bg-secondary disabled:opacity-60"><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />Синхронизировать аудит</button></div>
      {error && <div role="alert" className="mt-5 rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-sm">{error}</div>}
      {!active ? <div className="mt-5 rounded-lg border border-dashed border-border p-6 text-sm text-muted-foreground">Перейдите в этот раздел через меню, чтобы загрузить события аудита.</div> : <div className="mt-5 overflow-hidden rounded-lg border border-border bg-card">{events.length === 0 && !loading ? <p className="p-6 text-sm text-muted-foreground">Событий аудита пока нет.</p> : events.map((event) => <article key={event.id} className="grid gap-2 border-b border-border p-4 last:border-b-0 sm:grid-cols-[minmax(180px,0.7fr)_minmax(0,1.5fr)_auto] sm:items-center sm:gap-4 sm:px-5"><div className="flex items-center gap-2 font-mono text-sm"><ShieldCheck className="size-4 shrink-0 text-primary" />{event.eventType}</div><div className="break-words text-sm text-muted-foreground">{event.detail || 'Без дополнительных сведений'}<span className="mt-1 block text-xs">{event.sourceIp || 'internal'}</span></div><time className="text-xs text-muted-foreground">{time(event.createdAt)}</time></article>)}</div>}
      <footer className="mt-5 flex items-center gap-2 text-xs text-muted-foreground"><ShieldCheck className="size-3.5 text-primary" />Секреты в журналы не записываются; отмена задачи не означает откат инфраструктуры.</footer>
    </section>
  )
}

export function Operations() {
  const [section, setSection] = useState(() => window.location.hash)
  useEffect(() => {
    const update = () => setSection(window.location.hash)
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  return <><JobsPanel active={section === '#jobs'} /><AuditPanel active={section === '#audit'} /></>
}
