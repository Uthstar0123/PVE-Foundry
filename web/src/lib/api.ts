export type User = {
  id: string
  email: string
  displayName: string
  role: string
  createdAt: string
}

export type ProxmoxVersion = {
  version: string
  release: string
  repoid: string
}

export type ProxmoxNode = {
  node: string
  status: string
  cpu: number
  maxcpu: number
  mem: number
  maxmem: number
  uptime: number
}

export type ProxmoxResource = {
  id: string
  vmid: number
  name: string
  type: 'qemu' | 'lxc' | string
  node: string
  status: string
  template: number
  pool?: string
  maxcpu: number
  maxmem: number
  maxdisk: number
}

export type ProxmoxStorage = {
  storage: string
  node: string
  type: string
  content: string
  active: number
  enabled: number
  shared: number
  total: number
  used: number
  avail: number
}

export type ProxmoxNetwork = {
  iface: string
  node: string
  type: string
  active: number
  bridge_ports?: string
  address?: string
  cidr?: string
  gateway?: string
}

export type ProxmoxPool = {
  poolid: string
  comment?: string
}

export type Inventory = {
  collectedAt: string
  version: ProxmoxVersion
  nodes: ProxmoxNode[]
  resources: ProxmoxResource[]
  storage: ProxmoxStorage[]
  networks: ProxmoxNetwork[]
  pools: ProxmoxPool[]
  templates?: ProxmoxTemplate[]
  nextVmid?: number
}

export type ProxmoxTemplate = {
  id: string
  vmid?: number
  name: string
  type: 'qemu' | 'lxc' | string
  node?: string
  storage?: string
}

export type DeploymentDefaults = {
  nodeName: string
  poolId: string
  datastoreId: string
  bridge: string
  nextVmid: number
  templates: ProxmoxTemplate[]
}

export type DeploymentVariable = {
  name: string
  required: boolean
  sensitive: boolean
  source: 'auto' | 'default' | 'file' | 'required'
  value?: unknown
}

export type DeploymentFile = { name: string; content: string }
export type DeploymentDesign = import('@/components/InfrastructureDesigner').DesignerValue

export type Job = {
  id: string
  operation: string
  resourceId: string
  status: 'queued' | 'waiting_approval' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  initiatorUserId: string
  workerId?: string
  leaseExpiresAt?: string
  cancelRequestedAt?: string
  errorCode?: string
  logsTruncated: boolean
  createdAt: string
  startedAt?: string
  finishedAt?: string
  updatedAt: string
}

export type JobStep = {
  id: string
  jobId: string
  name: string
  status: string
  detail: string
  startedAt?: string
  finishedAt?: string
  createdAt: string
  updatedAt: string
}

export type JobEvent = { id: number; jobId: string; level: string; message: string; createdAt: string }
export type JobDetail = {
  job: Job
  steps: JobStep[]
  events: JobEvent[]
  artifactSha256?: string
  planText?: string
  planSha256?: string
  approvalExpiresAt?: string
  approvedAt?: string
}
export type AuditEvent = { id: number; eventType: string; userId?: string; sourceIp: string; detail: string; createdAt: string }

type ApiErrorBody = { error?: { code?: string; message?: string } }

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  const body = (await response.json().catch(() => ({}))) as ApiErrorBody & T
  if (!response.ok) {
    throw new ApiError(body.error?.message ?? 'Сервер недоступен. Проверьте Management API.', response.status)
  }
  return body
}

let csrfToken: string | undefined
async function csrf(): Promise<string> {
  if (csrfToken) return csrfToken
  const result = await request<{ token: string }>('/api/v1/auth/csrf')
  csrfToken = result.token
  return csrfToken
}

async function mutation<T>(path: string, body: unknown): Promise<T> {
  const token = await csrf()
  return request<T>(path, {
    method: 'POST',
    headers: { 'X-CSRF-Token': token },
    body: JSON.stringify(body),
  })
}

export const api = {
  status: () => request<{ required: boolean; authenticated: boolean }>('/api/v1/setup/status'),
  session: () => request<{ user: User }>('/api/v1/auth/session'),
  inventory: () => request<Inventory>('/api/v1/proxmox/inventory'),
  jobs: () => request<{ jobs: Job[] }>('/api/v1/jobs'),
  job: (id: string) => request<JobDetail>(`/api/v1/jobs/${id}`),
  cancelJob: (id: string) => mutation<{ job: Job; rollback: false }>(`/api/v1/jobs/${id}/cancel`, {}),
  deploymentDefaults: () => request<DeploymentDefaults>('/api/v1/deployments/defaults'),
  preflightDeployment: (files: DeploymentFile[]) => mutation<{ defaults: DeploymentDefaults; variables: DeploymentVariable[] }>('/api/v1/deployments/opentofu/preflight', { files }),
  createDeployment: (body: { resourceId: string; idempotencyKey: string; artifact?: string; files?: DeploymentFile[]; variables?: Record<string, unknown> }) => mutation<{ job: Job; artifactSha256: string }>('/api/v1/deployments/opentofu', body),
  createDesignerDeployment: (body: { resourceId: string; idempotencyKey: string; design: DeploymentDesign }) => mutation<{ job: Job; artifactSha256: string }>('/api/v1/deployments/opentofu/designer', body),
  approvePlan: (id: string, artifactSha256: string) => mutation<JobDetail>(`/api/v1/jobs/${id}/approve`, { artifactSha256 }),
  jobLogsURL: (id: string) => `/api/v1/jobs/${id}/logs`,
  audit: () => request<{ events: AuditEvent[] }>('/api/v1/audit'),
  setup: (body: { displayName: string; email: string; password: string }) => mutation<{ user: User }>('/api/v1/setup', body),
  login: (body: { email: string; password: string }) => mutation<{ user: User }>('/api/v1/auth/login', body),
  logout: async () => {
    await mutation<{ ok: boolean }>('/api/v1/auth/logout', {})
    csrfToken = undefined
  },
}
