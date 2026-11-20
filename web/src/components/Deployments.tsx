import { useState } from 'react'
import { FileArchive, Play, ShieldCheck, Upload } from 'lucide-react'
import { api } from '@/lib/api'
import type { Job, JobDetail } from '@/lib/api'

function message(cause: unknown) {
  return cause instanceof Error ? cause.message : 'Операция не выполнена'
}

function encodeArtifact(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(new Error('Не удалось прочитать архив'))
    reader.onload = () => resolve(String(reader.result).split(',')[1] ?? '')
    reader.readAsDataURL(file)
  })
}

export function DeploymentForm({ onCreated }: { onCreated: (job: Job) => void }) {
  const [resourceId, setResourceId] = useState('deployment/manual')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!file || file.size === 0 || file.size > 1024 * 1024 || !resourceId.trim()) {
      setError('Выберите непустой tar.gz-архив до 1 МиБ и укажите идентификатор deployment.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const result = await api.createDeployment({ resourceId: resourceId.trim(), idempotencyKey: crypto.randomUUID(), artifact: await encodeArtifact(file) })
      onCreated(result.job)
      setFile(null)
      const input = document.getElementById('opentofu-artifact') as HTMLInputElement | null
      if (input) input.value = ''
    } catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }

  return <form onSubmit={(event) => void submit(event)} className="mt-5 rounded-lg border border-border bg-card p-5" aria-labelledby="deployment-title">
    <div className="flex items-start gap-3"><FileArchive className="mt-1 size-5 shrink-0 text-primary" /><div><h3 id="deployment-title" className="font-medium">OpenTofu Plan → Apply</h3><p className="mt-1 text-sm leading-6 text-muted-foreground">Загрузите самодостаточный tar.gz с .tf-файлами. Runner сначала подготовит план; изменения в Proxmox начнутся только после вашего подтверждения.</p></div></div>
    <div className="mt-4 grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end">
      <label className="grid gap-1.5 text-sm"><span className="text-muted-foreground">Идентификатор deployment</span><input value={resourceId} onChange={(event) => setResourceId(event.target.value)} maxLength={255} required className="min-h-11 min-w-0 rounded-md border border-border bg-background px-3 font-mono text-sm" /></label>
      <label className="grid gap-1.5 text-sm"><span className="text-muted-foreground">Архив tar.gz, до 1 МиБ</span><input id="opentofu-artifact" type="file" accept=".gz,.tgz,application/gzip" required onChange={(event) => setFile(event.target.files?.[0] ?? null)} className="min-h-11 min-w-0 rounded-md border border-border bg-background px-3 py-2 text-sm file:mr-3 file:rounded file:border-0 file:bg-secondary file:px-3 file:py-1.5" /></label>
      <button type="submit" disabled={busy} className="inline-flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-60"><Upload className="size-4" />{busy ? 'Загрузка…' : 'Создать Plan'}</button>
    </div>
    {error && <p role="alert" className="mt-3 text-sm text-destructive">{error}</p>}
    <p className="mt-3 text-xs leading-5 text-muted-foreground">Разрешён только bpg/proxmox 0.123.0. Модули, provisioner и удалённые backend запрещены. Секрет Proxmox задаётся на Runner, а не в архиве.</p>
  </form>
}

export function PlanApproval({ detail, onApproved }: { detail: JobDetail; onApproved: (detail: JobDetail) => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  if (!detail.planText || !detail.artifactSha256) return null
  const approve = async () => {
    if (!window.confirm('Подтвердить Apply именно этого плана? Отмена не выполняет rollback.')) return
    setBusy(true)
    setError('')
    try { onApproved(await api.approvePlan(detail.job.id, detail.artifactSha256!)) }
    catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }
  const canApprove = detail.job.status === 'waiting_approval' && !detail.approvedAt
  return <div className="mt-6 rounded-lg border border-primary/30 bg-primary/5 p-4">
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div><h4 className="flex items-center gap-2 text-sm font-medium"><ShieldCheck className="size-4 text-primary" />Сохранённый план</h4><p className="mt-1 break-all font-mono text-xs text-muted-foreground">SHA-256 конфигурации: {detail.artifactSha256}</p></div>{canApprove && <button type="button" disabled={busy} onClick={() => void approve()} className="inline-flex min-h-10 shrink-0 cursor-pointer items-center justify-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-60"><Play className="size-4" />{busy ? 'Подтверждение…' : 'Подтвердить Apply'}</button>}</div>
    {detail.approvedAt && <p className="mt-3 text-sm text-primary">Apply подтверждён{detail.approvalExpiresAt ? `; действует до ${new Date(detail.approvalExpiresAt).toLocaleString('ru-RU')}` : ''}.</p>}
    {error && <p role="alert" className="mt-3 text-sm text-destructive">{error}</p>}
    <pre className="mt-4 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background p-4 font-mono text-xs leading-6">{detail.planText}</pre>
  </div>
}
