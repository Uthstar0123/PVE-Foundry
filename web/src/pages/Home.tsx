import type { User } from '@/lib/api'
import {
  Box,
  Check,
  CheckCircle2,
  ChevronRight,
  Circle,
  CloudCog,
  Cpu,
  Database,
  HardDrive,
  LogOut,
  Network,
  Server,
  ShieldCheck,
  TerminalSquare,
} from 'lucide-react'

const verificationItems = [
  'Создание и полное клонирование QEMU',
  'Создание непривилегированного LXC',
  'DHCP, обнаружение IP и доступ по SSH',
  'Изменение CPU/RAM без пересоздания',
  'Повторный plan без расхождений',
  'Удаление только тестовых ресурсов',
]

const stages = [
  { number: '00', label: 'Совместимость', state: 'done' },
  { number: '01', label: 'Каркас проекта', state: 'done' },
  { number: '02', label: 'БД и авторизация', state: 'current' },
  { number: '03', label: 'Инвентарь Proxmox', state: 'next' },
]

type Props = { user: User; onLogout: () => Promise<void> }

export default function Home({ user, onLogout }: Props) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <aside className="border-b border-border bg-card/60 px-5 py-5 lg:fixed lg:inset-y-0 lg:left-0 lg:w-64 lg:border-b-0 lg:border-r lg:px-6 lg:py-7">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <CloudCog className="size-5" aria-hidden="true" />
          </div>
          <div>
            <div className="font-semibold tracking-tight">PVE</div>
            <div className="-mt-0.5 text-sm text-muted-foreground">Foundry</div>
          </div>
        </div>

        <div className="mt-6 flex items-center gap-2 rounded-md border border-border bg-background/50 px-3 py-2 text-sm lg:mt-10">
          <Circle className="size-2 fill-primary text-primary" aria-hidden="true" />
          <span>Локальный контур</span>
        </div>

        <nav className="mt-6 hidden lg:block" aria-label="Этапы проекта">
          <p className="mb-3 text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Путь реализации
          </p>
          <ol className="space-y-1">
            {stages.map((stage) => (
              <li
                key={stage.number}
                className={`flex items-center gap-3 rounded-md px-3 py-2.5 text-sm ${
                  stage.state === 'current'
                    ? 'bg-secondary text-foreground'
                    : 'text-muted-foreground'
                }`}
              >
                {stage.state === 'done' ? (
                  <Check className="size-4 text-primary" aria-hidden="true" />
                ) : (
                  <span className="w-4 text-center font-mono text-xs">{stage.number}</span>
                )}
                <span>{stage.label}</span>
              </li>
            ))}
          </ol>
        </nav>

        <div className="mt-6 lg:absolute lg:inset-x-6 lg:bottom-7">
          <div className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2">
            <div className="min-w-0"><div className="truncate text-sm font-medium">{user.displayName}</div><div className="truncate text-xs text-muted-foreground">{user.email}</div></div>
            <button type="button" onClick={() => void onLogout()} className="flex size-11 shrink-0 cursor-pointer items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label="Выйти"><LogOut className="size-4" /></button>
          </div>
          <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground"><ShieldCheck className="size-4" aria-hidden="true" />Без доступа из Интернета</div>
        </div>
      </aside>

      <main className="lg:pl-64">
        <div className="mx-auto max-w-7xl px-5 py-8 sm:px-8 lg:px-12 lg:py-12">
          <header className="flex flex-col gap-6 border-b border-border pb-9 md:flex-row md:items-end md:justify-between">
            <div className="max-w-3xl">
              <div className="mb-4 inline-flex items-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-3 py-1 text-sm text-primary">
                <CheckCircle2 className="size-4" aria-hidden="true" />
                Доступ защищён · этап 2
              </div>
              <h1 className="text-3xl font-semibold tracking-[-0.035em] sm:text-4xl lg:text-5xl">
                Инфраструктура без ручной рутины.
              </h1>
              <p className="mt-4 max-w-2xl text-base leading-7 text-muted-foreground sm:text-lg">
                Low-code конструктор ВМ, LXC и сетей с прозрачным OpenTofu plan
                и подтверждением каждого изменения.
              </p>
            </div>
            <div className="shrink-0 font-mono text-sm text-muted-foreground">
              PVE <span className="text-foreground">9.2.10</span>
              <span className="mx-2 text-border">/</span>
              bpg <span className="text-foreground">0.123.0</span>
            </div>
          </header>

          <section className="grid grid-cols-2 gap-3 py-8 lg:grid-cols-4" aria-label="Параметры контура">
            {[
              { icon: Server, value: 'pve', label: 'Нода в сети' },
              { icon: Cpu, value: '48', label: 'CPU на ноде' },
              { icon: Box, value: 'QEMU + LXC', label: 'Оба типа проверены' },
              { icon: Network, value: 'API only', label: 'Без SSH к хосту' },
            ].map((item) => (
              <div key={item.label} className="rounded-lg border border-border bg-card p-4 sm:p-5">
                <item.icon className="mb-5 size-5 text-primary" aria-hidden="true" />
                <div className="text-lg font-semibold tracking-tight sm:text-xl">{item.value}</div>
                <div className="mt-1 text-xs text-muted-foreground sm:text-sm">{item.label}</div>
              </div>
            ))}
          </section>

          <div className="grid gap-6 xl:grid-cols-[1.35fr_1fr]">
            <section className="rounded-xl border border-border bg-card p-5 sm:p-7" aria-labelledby="architecture-title">
              <div className="mb-7">
                <h2 id="architecture-title" className="text-xl font-semibold tracking-tight">
                  Разделение ответственности
                </h2>
                <p className="mt-2 text-sm leading-6 text-muted-foreground">
                  Панель не исполняет внешний код. Изменения выполняет изолированный Runner.
                </p>
              </div>

              <div className="grid items-stretch gap-3 md:grid-cols-[1fr_auto_1fr_auto_1fr]">
                {[
                  { icon: Database, title: 'Management', subtitle: 'API · UI · PostgreSQL', note: 'Каркас этапа 1' },
                  { icon: TerminalSquare, title: 'Runner', subtitle: 'OpenTofu · Ansible', note: 'VM проверена' },
                  { icon: HardDrive, title: 'Proxmox', subtitle: 'QEMU · LXC · Storage', note: 'API проверен' },
                ].map((item, index) => (
                  <div key={item.title} className="contents">
                    <div className="rounded-lg border border-border bg-background/60 p-4">
                      <item.icon className="size-5 text-primary" aria-hidden="true" />
                      <h3 className="mt-4 font-medium">{item.title}</h3>
                      <p className="mt-1 text-xs leading-5 text-muted-foreground">{item.subtitle}</p>
                      <p className="mt-4 border-t border-border pt-3 text-xs text-muted-foreground">{item.note}</p>
                    </div>
                    {index < 2 && (
                      <div className="flex items-center justify-center text-muted-foreground md:px-1">
                        <ChevronRight className="size-4 rotate-90 md:rotate-0" aria-hidden="true" />
                      </div>
                    )}
                  </div>
                ))}
              </div>

              <div className="mt-6 flex items-start gap-3 rounded-lg bg-secondary/70 p-4 text-sm leading-6 text-secondary-foreground">
                <ShieldCheck className="mt-0.5 size-5 shrink-0 text-primary" aria-hidden="true" />
                Apply выполняется только после просмотра плана и явного подтверждения администратора.
              </div>
            </section>

            <section className="rounded-xl border border-border bg-card p-5 sm:p-7" aria-labelledby="verified-title">
              <h2 id="verified-title" className="text-xl font-semibold tracking-tight">
                Проверено на контуре
              </h2>
              <p className="mt-2 text-sm text-muted-foreground">19 ноября 2026</p>
              <ul className="mt-6 space-y-4">
                {verificationItems.map((item) => (
                  <li key={item} className="flex items-start gap-3 text-sm leading-6">
                    <span className="mt-1 flex size-5 shrink-0 items-center justify-center rounded-full bg-primary/15 text-primary">
                      <Check className="size-3" aria-hidden="true" />
                    </span>
                    {item}
                  </li>
                ))}
              </ul>
            </section>
          </div>

          <footer className="mt-8 flex flex-col gap-2 border-t border-border pt-5 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
            <span>OpenTofu 1.13.9 · Ansible 2.19.11 · Debian 13</span>
            <span>Рабочие ресурсы вне тестового pool не изменялись</span>
          </footer>
        </div>
      </main>
    </div>
  )
}
