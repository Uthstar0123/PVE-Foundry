import { useCallback, useEffect, useState } from 'react'
import type { Inventory, ProxmoxResource, User } from '@/lib/api'
import { ApiError, api } from '@/lib/api'
import {
  Activity,
  Box,
  Check,
  ChevronDown,
  Circle,
  Clock3,
  CloudCog,
  Cpu,
  Database,
  HardDrive,
  History,
  Layers3,
  ListTodo,
  LoaderCircle,
  LogOut,
  MemoryStick,
  Network,
  RefreshCw,
  Server,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react'
import { Operations } from '@/components/Operations'

const navigation = [
  { href: '#overview', label: 'Обзор', icon: Activity },
  { href: '#resources', label: 'Ресурсы', icon: Box },
  { href: '#storage', label: 'Хранилища', icon: Database },
  { href: '#network', label: 'Сеть', icon: Network },
  { href: '#jobs', label: 'Задачи', icon: ListTodo },
  { href: '#audit', label: 'Аудит', icon: History },
]

const numberFormat = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 })

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 Б'
  const units = ['Б', 'КиБ', 'МиБ', 'ГиБ', 'ТиБ']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${numberFormat.format(value / 1024 ** index)} ${units[index]}`
}

function formatUptime(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return days > 0 ? `${days} д ${hours} ч` : `${hours} ч ${Math.floor((seconds % 3600) / 60)} мин`
}

function Status({ value }: { value: string }) {
  const online = value === 'running' || value === 'online'
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs ${online ? 'text-primary' : 'text-muted-foreground'}`}>
      <span className={`size-1.5 rounded-full ${online ? 'bg-primary' : 'bg-muted-foreground'}`} />
      {value || 'неизвестно'}
    </span>
  )
}

function ResourceDetails({ resource }: { resource: ProxmoxResource }) {
  return (
    <details className="group rounded-lg border border-border bg-card transition-colors open:bg-secondary/20">
      <summary className="flex min-h-16 cursor-pointer list-none items-center gap-3 rounded-lg px-4 py-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:px-5">
        <span className={`flex size-9 shrink-0 items-center justify-center rounded-md ${resource.type === 'qemu' ? 'bg-primary/10 text-primary' : 'bg-secondary text-secondary-foreground'}`}>
          {resource.type === 'qemu' ? <Server className="size-4" aria-hidden="true" /> : <Box className="size-4" aria-hidden="true" />}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="truncate font-medium">{resource.name || `Ресурс ${resource.vmid}`}</span>
            <span className="font-mono text-xs text-muted-foreground">#{resource.vmid}</span>
          </span>
          <span className="mt-0.5 block text-xs text-muted-foreground">{resource.type.toUpperCase()} · {resource.node}</span>
        </span>
        <span className="hidden sm:inline-flex"><Status value={resource.status} /></span>
        {resource.template === 1 && <span className="hidden rounded border border-border px-2 py-0.5 text-xs text-muted-foreground md:inline">шаблон</span>}
        <ChevronDown className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" aria-hidden="true" />
      </summary>
      <dl className="grid grid-cols-2 gap-4 border-t border-border px-5 py-4 text-sm sm:grid-cols-4">
        <div><dt className="text-xs text-muted-foreground">vCPU</dt><dd className="mt-1 font-medium">{resource.maxcpu || '—'}</dd></div>
        <div><dt className="text-xs text-muted-foreground">Память</dt><dd className="mt-1 font-medium">{formatBytes(resource.maxmem)}</dd></div>
        <div><dt className="text-xs text-muted-foreground">Диск</dt><dd className="mt-1 font-medium">{formatBytes(resource.maxdisk)}</dd></div>
        <div><dt className="text-xs text-muted-foreground">Pool</dt><dd className="mt-1 break-words font-medium">{resource.pool || 'Не назначен'}</dd></div>
      </dl>
    </details>
  )
}

type Props = { user: User; onLogout: () => Promise<void> }

export default function Home({ user, onLogout }: Props) {
  const [inventory, setInventory] = useState<Inventory | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<{ message: string; status: number } | null>(null)

  const loadInventory = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setInventory(await api.inventory())
    } catch (cause) {
      setError({
        message: cause instanceof Error ? cause.message : 'Не удалось получить инвентарь Proxmox',
        status: cause instanceof ApiError ? cause.status : 0,
      })
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void loadInventory() }, [loadInventory])

  const resources = inventory?.resources ?? []
  const qemu = resources.filter((resource) => resource.type === 'qemu').length
  const lxc = resources.filter((resource) => resource.type === 'lxc').length

  return (
    <div className="min-h-screen bg-background text-foreground">
      <aside className="border-b border-border bg-card/60 px-5 py-5 lg:fixed lg:inset-y-0 lg:left-0 lg:w-64 lg:border-b-0 lg:border-r lg:px-6 lg:py-7">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <CloudCog className="size-5" aria-hidden="true" />
          </div>
          <div><div className="font-semibold tracking-tight">PVE</div><div className="-mt-0.5 text-sm text-muted-foreground">Foundry</div></div>
        </div>

        <div className="mt-6 flex items-center gap-2 rounded-md border border-border bg-background/50 px-3 py-2 text-sm lg:mt-10">
          <Circle className="size-2 fill-primary text-primary" aria-hidden="true" />
          <span>Локальный контур</span>
        </div>

        <nav className="mt-5 grid grid-cols-2 gap-1 lg:mt-8 lg:grid-cols-1" aria-label="Разделы инвентаря">
          {navigation.map((item) => (
            <a key={item.href} href={item.href} className="flex min-h-11 items-center gap-3 rounded-md px-3 py-2.5 text-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground">
              <item.icon className="size-4" aria-hidden="true" /><span>{item.label}</span>
            </a>
          ))}
        </nav>

        <div className="mt-6 lg:absolute lg:inset-x-6 lg:bottom-7">
          <div className="hidden mb-4 items-center gap-2 text-xs text-muted-foreground lg:flex"><Check className="size-4 text-primary" aria-hidden="true" />Этап 4 · задачи и аудит</div>
          <div className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2">
            <div className="min-w-0"><div className="truncate text-sm font-medium">{user.displayName}</div><div className="truncate text-xs text-muted-foreground">{user.email}</div></div>
            <button type="button" onClick={() => void onLogout()} className="flex size-11 shrink-0 cursor-pointer items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" aria-label="Выйти"><LogOut className="size-4" /></button>
          </div>
          <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground"><ShieldCheck className="size-4" aria-hidden="true" />Без доступа из Интернета</div>
        </div>
      </aside>

      <main className="lg:pl-64">
        <div className="mx-auto max-w-7xl scroll-mt-6 px-5 py-8 sm:px-8 lg:px-12 lg:py-10">
          <header className="flex flex-col gap-6 border-b border-border pb-8 md:flex-row md:items-end md:justify-between">
            <div className="max-w-3xl">
              <div className="mb-4 inline-flex items-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-3 py-1 text-sm text-primary">
                <ShieldCheck className="size-4" aria-hidden="true" />Очередь и аудит · этап 4
              </div>
              <h1 className="text-3xl font-semibold tracking-[-0.035em] sm:text-4xl lg:text-5xl">Инфраструктура без ручной рутины.</h1>
              <p className="mt-4 max-w-2xl text-base leading-7 text-muted-foreground sm:text-lg">Фактическое состояние нод, ВМ, LXC, хранилищ и сети Proxmox — без изменяющих операций.</p>
            </div>
            <div className="flex shrink-0 items-center gap-4">
              <div className="font-mono text-sm text-muted-foreground">PVE <span className="text-foreground">{inventory?.version.version || '—'}</span></div>
              <button type="button" onClick={() => void loadInventory()} disabled={loading} className="inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-md border border-border bg-card px-4 text-sm font-medium transition-colors hover:bg-secondary disabled:cursor-wait disabled:opacity-60">
                <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />Обновить
              </button>
            </div>
          </header>

          {error && inventory && (
            <div role="alert" className="mt-6 flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-sm"><TriangleAlert className="mt-0.5 size-5 shrink-0 text-destructive" aria-hidden="true" /><span>Не удалось обновить данные. Показан предыдущий снимок: {error.message}</span></div>
          )}

          {loading && !inventory && (
            <div className="flex min-h-80 items-center justify-center" role="status"><div className="text-center"><LoaderCircle className="mx-auto size-7 animate-spin text-primary" aria-hidden="true" /><p className="mt-4 text-sm text-muted-foreground">Читаем инвентарь Proxmox…</p></div></div>
          )}

          {error && !inventory && !loading && (
            <section className="mt-8 rounded-xl border border-border bg-card p-6 sm:p-8" aria-labelledby="connection-title">
              <TriangleAlert className="size-7 text-primary" aria-hidden="true" />
              <h2 id="connection-title" className="mt-5 text-xl font-semibold">{error.status === 503 ? 'Proxmox ещё не подключён' : 'Не удалось получить инвентарь'}</h2>
              <p className="mt-3 max-w-2xl leading-7 text-muted-foreground">{error.message}</p>
              <p className="mt-3 max-w-2xl text-sm leading-6 text-muted-foreground">
                {error.status === 503
                  ? 'Укажите URL и read-only API token в окружении Management, затем перезапустите сервис. Токен не вводится в браузере и не сохраняется в репозитории.'
                  : 'Проверьте доступность Proxmox, права токена PVEAuditor и доверие к сертификату CA.'}
              </p>
              {error.status === 503 && <div className="mt-5 overflow-x-auto rounded-md bg-background p-4 font-mono text-xs leading-6 text-secondary-foreground"><code>MANAGEMENT_PROXMOX_API_URL<br />MANAGEMENT_PROXMOX_API_TOKEN<br />MANAGEMENT_PROXMOX_CA_FILE</code></div>}
              <button type="button" onClick={() => void loadInventory()} className="mt-6 inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90"><RefreshCw className="size-4" aria-hidden="true" />Повторить</button>
            </section>
          )}

          {inventory && (
            <>
              <section id="overview" className="scroll-mt-6 py-8" aria-labelledby="overview-title">
                <div className="mb-5 flex items-end justify-between gap-4"><div><h2 id="overview-title" className="text-xl font-semibold tracking-tight">Обзор контура</h2><p className="mt-1 text-sm text-muted-foreground">Один снимок Proxmox API, без локальных догадок.</p></div></div>
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                  {[
                    { icon: Server, value: inventory.nodes.length, label: 'Ноды' },
                    { icon: Box, value: `${qemu} / ${lxc}`, label: 'QEMU / LXC' },
                    { icon: Database, value: inventory.storage.length, label: 'Хранилища' },
                    { icon: Layers3, value: inventory.pools.length, label: 'Pools' },
                  ].map((item) => (
                    <div key={item.label} className="rounded-lg border border-border bg-card p-4 sm:p-5"><item.icon className="mb-5 size-5 text-primary" aria-hidden="true" /><div className="text-2xl font-semibold tracking-tight">{item.value}</div><div className="mt-1 text-xs text-muted-foreground sm:text-sm">{item.label}</div></div>
                  ))}
                </div>

                <div className="mt-5 grid gap-4 md:grid-cols-2">
                  {inventory.nodes.map((node) => (
                    <article key={node.node} className="rounded-lg border border-border bg-card p-5">
                      <div className="flex items-center justify-between gap-4"><h3 className="flex items-center gap-2 font-medium"><Server className="size-4 text-primary" aria-hidden="true" />{node.node}</h3><Status value={node.status} /></div>
                      <dl className="mt-5 grid grid-cols-3 gap-3 text-sm">
                        <div><dt className="flex items-center gap-1.5 text-xs text-muted-foreground"><Cpu className="size-3.5" aria-hidden="true" />CPU</dt><dd className="mt-1 font-medium">{Math.round(node.cpu * 100)}% · {node.maxcpu} ядер</dd></div>
                        <div><dt className="flex items-center gap-1.5 text-xs text-muted-foreground"><MemoryStick className="size-3.5" aria-hidden="true" />RAM</dt><dd className="mt-1 font-medium">{formatBytes(node.mem)} / {formatBytes(node.maxmem)}</dd></div>
                        <div><dt className="flex items-center gap-1.5 text-xs text-muted-foreground"><Clock3 className="size-3.5" aria-hidden="true" />Uptime</dt><dd className="mt-1 font-medium">{formatUptime(node.uptime)}</dd></div>
                      </dl>
                    </article>
                  ))}
                  {inventory.nodes.length === 0 && <p className="rounded-lg border border-border bg-card p-5 text-sm text-muted-foreground">Proxmox не вернул ни одной ноды.</p>}
                </div>
              </section>

              <section id="resources" className="scroll-mt-6 border-t border-border py-8" aria-labelledby="resources-title">
                <div className="mb-5"><h2 id="resources-title" className="text-xl font-semibold tracking-tight">Виртуальные ресурсы</h2><p className="mt-1 text-sm text-muted-foreground">Раскройте ресурс, чтобы увидеть параметры. Управление питанием здесь отсутствует.</p></div>
                <div className="space-y-3">{resources.map((resource) => <ResourceDetails key={resource.id || `${resource.type}/${resource.vmid}`} resource={resource} />)}</div>
                {resources.length === 0 && <div className="rounded-lg border border-dashed border-border p-8 text-center"><Box className="mx-auto size-6 text-muted-foreground" aria-hidden="true" /><p className="mt-3 text-sm text-muted-foreground">QEMU и LXC не найдены.</p></div>}
              </section>

              <section id="storage" className="scroll-mt-6 border-t border-border py-8" aria-labelledby="storage-title">
                <h2 id="storage-title" className="text-xl font-semibold tracking-tight">Хранилища и pools</h2>
                <div className="mt-5 grid gap-4 md:grid-cols-2">
                  {inventory.storage.map((storage) => {
                    const usedPercent = storage.total > 0 ? Math.min(100, Math.round((storage.used / storage.total) * 100)) : 0
                    return (
                      <article key={`${storage.node}/${storage.storage}`} className="rounded-lg border border-border bg-card p-5">
                        <div className="flex items-start justify-between gap-3"><div><h3 className="flex items-center gap-2 font-medium"><HardDrive className="size-4 text-primary" aria-hidden="true" />{storage.storage}</h3><p className="mt-1 text-xs text-muted-foreground">{storage.node} · {storage.type}</p></div><Status value={storage.active === 1 ? 'online' : 'offline'} /></div>
                        <div className="mt-5 h-1.5 overflow-hidden rounded-full bg-secondary" role="progressbar" aria-label={`Заполнение ${storage.storage}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={usedPercent}><div className="h-full rounded-full bg-primary" style={{ width: `${usedPercent}%` }} /></div>
                        <div className="mt-3 flex flex-wrap justify-between gap-2 text-sm"><span>{formatBytes(storage.used)} занято</span><span className="text-muted-foreground">{formatBytes(storage.avail)} свободно · {usedPercent}%</span></div>
                        <p className="mt-4 break-words border-t border-border pt-3 text-xs leading-5 text-muted-foreground">{storage.content || 'Тип содержимого не указан'}</p>
                      </article>
                    )
                  })}
                </div>
                {inventory.storage.length === 0 && <p className="mt-5 rounded-lg border border-border bg-card p-5 text-sm text-muted-foreground">Хранилища не найдены.</p>}
                <div className="mt-4 flex flex-wrap gap-2">{inventory.pools.map((pool) => <span key={pool.poolid} title={pool.comment || pool.poolid} className="inline-flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm"><Layers3 className="size-3.5 text-primary" aria-hidden="true" />{pool.poolid}</span>)}</div>
              </section>

              <section id="network" className="scroll-mt-6 border-t border-border py-8" aria-labelledby="network-title">
                <h2 id="network-title" className="text-xl font-semibold tracking-tight">Сетевые интерфейсы</h2>
                <div className="mt-5 overflow-hidden rounded-lg border border-border bg-card">
                  {inventory.networks.map((network) => (
                    <div key={`${network.node}/${network.iface}`} className="flex flex-col gap-3 border-b border-border p-4 last:border-b-0 sm:flex-row sm:items-center sm:justify-between sm:px-5">
                      <div className="flex min-w-0 items-center gap-3"><Network className="size-4 shrink-0 text-primary" aria-hidden="true" /><div className="min-w-0"><div className="break-words font-medium">{network.iface}<span className="ml-2 font-mono text-xs font-normal text-muted-foreground">{network.type}</span></div><div className="mt-0.5 break-words text-xs text-muted-foreground">{network.node}{network.bridge_ports ? ` · порты: ${network.bridge_ports}` : ''}</div></div></div>
                      <div className="pl-7 text-sm text-muted-foreground sm:pl-4 sm:text-right"><div>{network.cidr || network.address || 'Адрес не назначен'}</div><div className="mt-1"><Status value={network.active === 1 ? 'online' : 'offline'} /></div></div>
                    </div>
                  ))}
                  {inventory.networks.length === 0 && <p className="p-5 text-sm text-muted-foreground">Сетевые интерфейсы не найдены.</p>}
                </div>
              </section>

              <footer className="flex flex-col gap-2 border-t border-border pt-5 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between"><span>Снимок: {new Date(inventory.collectedAt).toLocaleString('ru-RU')}</span><span className="inline-flex items-center gap-1.5"><ShieldCheck className="size-3.5" aria-hidden="true" />Изменяющие запросы к Proxmox не выполняются</span></footer>
            </>
          )}

          <Operations />
        </div>
      </main>
    </div>
  )
}
