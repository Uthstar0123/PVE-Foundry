export type User = {
  id: string
  email: string
  displayName: string
  role: string
  createdAt: string
}

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
  setup: (body: { displayName: string; email: string; password: string }) => mutation<{ user: User }>('/api/v1/setup', body),
  login: (body: { email: string; password: string }) => mutation<{ user: User }>('/api/v1/auth/login', body),
  logout: async () => {
    await mutation<{ ok: boolean }>('/api/v1/auth/logout', {})
    csrfToken = undefined
  },
}
