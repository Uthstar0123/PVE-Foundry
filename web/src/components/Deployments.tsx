import { useEffect, useState } from 'react'
import { Blocks, FileCode2, Play, ShieldCheck, Upload } from 'lucide-react'
import { api } from '@/lib/api'
import type { DeploymentDefaults, DeploymentDesign, DeploymentFile, DeploymentVariable, Job, JobDetail } from '@/lib/api'
import { InfrastructureDesigner } from './InfrastructureDesigner'

function message(cause: unknown) {
  return cause instanceof Error ? cause.message : 'Операция не выполнена'
}

function encodeFile(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(new Error('Не удалось прочитать файл'))
    reader.onload = () => resolve(String(reader.result).split(',')[1] ?? '')
    reader.readAsDataURL(file)
  })
}

const emptyDesign: DeploymentDesign = { name: 'deployment/design', nodes: [], edges: [] }

export function DeploymentForm({ onCreated }: { onCreated: (job: Job) => void }) {
  const [mode, setMode] = useState<'files' | 'designer'>('files')
  const [resourceId, setResourceId] = useState('deployment/upload')
  const [selected, setSelected] = useState<File[]>([])
  const [files, setFiles] = useState<DeploymentFile[]>([])
  const [variables, setVariables] = useState<DeploymentVariable[]>([])
  const [values, setValues] = useState<Record<string, unknown>>({})
  const [defaults, setDefaults] = useState<DeploymentDefaults>()
  const [design, setDesign] = useState<DeploymentDesign>(emptyDesign)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (mode !== 'designer' || defaults) return
    void api.deploymentDefaults().then(setDefaults).catch((cause) => setError(message(cause)))
  }, [mode, defaults])

  const chooseFiles = async (chosen: File[]) => {
    setError('')
    setVariables([])
    setFiles([])
    setSelected(chosen)
    if (!chosen.length) return
    if (chosen.some((file) => file.size === 0 || file.size > 1024 * 1024)) {
      setError('Каждый файл должен быть непустым и не превышать 1 МиБ.')
      return
    }
    const archive = chosen.length === 1 && /\.(gz|tgz)$/i.test(chosen[0].name)
    if (archive) return
    setBusy(true)
    try {
      const encoded = await Promise.all(chosen.map(async (file) => ({ name: file.name, content: await encodeFile(file) })))
      setFiles(encoded)
      const result = await api.preflightDeployment(encoded)
      setDefaults(result.defaults)
      setVariables(result.variables)
      setValues(Object.fromEntries(result.variables.filter((item) => !item.required).map((item) => [item.name, item.value])))
    } catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }

  const upload = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!selected.length || selected.some((file) => file.size > 1024 * 1024)) {
      setError('Выберите .tf, .tf.json, .tfvars или готовый tar.gz-архив.')
      return
    }
    if (variables.some((item) => item.required && (values[item.name] === undefined || values[item.name] === ''))) {
      setError('Заполните обязательные переменные, которые Foundry не смог определить.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const archive = selected.length === 1 && /\.(gz|tgz)$/i.test(selected[0].name)
      const result = await api.createDeployment({
        resourceId: resourceId.trim(), idempotencyKey: crypto.randomUUID(),
        ...(archive ? { artifact: await encodeFile(selected[0]) } : { files, variables: values }),
      })
      onCreated(result.job)
      setSelected([]); setFiles([]); setVariables([])
    } catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }

  const submitDesign = async () => {
    setBusy(true); setError('')
    try {
      const result = await api.createDesignerDeployment({ resourceId: design.name, idempotencyKey: crypto.randomUUID(), design })
      onCreated(result.job)
    } catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }

  return <section className="mt-5 rounded-lg border border-border bg-card p-4 sm:p-5" aria-labelledby="deployment-title">
    <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div><h3 id="deployment-title" className="font-medium">Новая инфраструктура</h3><p className="mt-1 text-sm leading-6 text-muted-foreground">Сначала создаётся Plan. Изменения в Proxmox начнутся только после вашего подтверждения.</p></div>
      <div className="flex rounded-md border border-border p-1" role="tablist" aria-label="Способ создания">
        <button type="button" role="tab" aria-selected={mode === 'files'} onClick={() => { setMode('files'); setError('') }} className={`inline-flex min-h-9 cursor-pointer items-center gap-2 rounded px-3 text-sm ${mode === 'files' ? 'bg-primary text-primary-foreground' : ''}`}><FileCode2 className="size-4" />Загрузить файл</button>
        <button type="button" role="tab" aria-selected={mode === 'designer'} onClick={() => { setMode('designer'); setError('') }} className={`inline-flex min-h-9 cursor-pointer items-center gap-2 rounded px-3 text-sm ${mode === 'designer' ? 'bg-primary text-primary-foreground' : ''}`}><Blocks className="size-4" />Конструктор</button>
      </div>
    </div>
    {mode === 'files' ? <form onSubmit={(event) => void upload(event)} className="mt-5">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1.5 text-sm"><span className="text-muted-foreground">Идентификатор deployment</span><input value={resourceId} onChange={(event) => setResourceId(event.target.value)} maxLength={255} required className="min-h-11 min-w-0 rounded-md border border-border bg-background px-3 font-mono text-sm" /></label>
        <label className="grid gap-1.5 text-sm"><span className="text-muted-foreground">Файл конфигурации</span><input id="opentofu-artifact" type="file" multiple accept=".tf,.json,.tfvars,.gz,.tgz,application/gzip" onChange={(event) => void chooseFiles(Array.from(event.target.files ?? []))} className="min-h-11 min-w-0 rounded-md border border-border bg-background px-3 py-2 text-sm file:mr-3 file:rounded file:border-0 file:bg-secondary file:px-3 file:py-1.5" /></label>
      </div>
      {selected.length > 0 && <p className="mt-3 text-xs text-muted-foreground">Выбрано: {selected.map((file) => file.name).join(', ')}. Нода, хранилище, сеть, VMID и шаблоны определяются автоматически.</p>}
      {variables.filter((item) => item.required).length > 0 && <div className="mt-4 grid gap-3 sm:grid-cols-2">{variables.filter((item) => item.required).map((item) => <label key={item.name} className="grid gap-1.5 text-sm"><span className="text-muted-foreground">{item.name}{item.sensitive ? ' (секрет)' : ''}</span><input type={item.sensitive ? 'password' : 'text'} value={String(values[item.name] ?? '')} onChange={(event) => setValues((current) => ({ ...current, [item.name]: event.target.value }))} required className="min-h-11 rounded-md border border-border bg-background px-3 font-mono text-sm" /></label>)}</div>}
      {variables.some((item) => !item.required) && <details className="mt-4 rounded-md border border-border p-3 text-sm"><summary className="cursor-pointer">Определённые значения — можно изменить</summary><div className="mt-3 grid gap-3 sm:grid-cols-2">{variables.filter((item) => !item.required).map((item) => <label key={item.name} className="grid gap-1.5"><span className="text-xs text-muted-foreground">{item.name} · {item.source === 'auto' ? 'Proxmox' : item.source === 'file' ? 'из файла' : 'по умолчанию'}</span><input value={typeof values[item.name] === 'object' ? JSON.stringify(values[item.name]) : String(values[item.name] ?? '')} onChange={(event) => setValues((current) => ({ ...current, [item.name]: event.target.value }))} className="min-h-10 rounded-md border border-border bg-background px-3 font-mono text-sm" /></label>)}</div></details>}
      <button type="submit" disabled={busy || !selected.length} className="mt-4 inline-flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-60"><Upload className="size-4" />{busy ? 'Анализ…' : 'Создать Plan'}</button>
    </form> : <div className="mt-5"><InfrastructureDesigner value={design} onChange={setDesign} onSubmit={() => void submitDesign()} busy={busy} defaults={defaults} /></div>}
    {error && <p role="alert" className="mt-3 text-sm text-destructive">{error}</p>}
    <p className="mt-3 text-xs leading-5 text-muted-foreground">Разрешён только bpg/proxmox 0.123.0. Модули, provisioner и удалённые backend запрещены. Секрет Proxmox задаётся на Runner.</p>
  </section>
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
