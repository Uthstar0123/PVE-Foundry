import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Operations } from './Operations'
import type { Job, JobDetail } from '@/lib/api'

const job: Job = { id: '5f9160cb-b4ce-4ee3-9f62-89027d34a101', operation: 'vm.create', resourceId: 'qemu/example', status: 'queued', initiatorUserId: 'admin', logsTruncated: false, createdAt: '2026-11-19T12:00:00Z', updatedAt: '2026-11-19T12:00:00Z' }
const detail: JobDetail = { job, steps: [], events: [{ id: 1, jobId: job.id, level: 'info', message: 'Задача добавлена', createdAt: '2026-11-19T12:00:00Z' }] }
const json = (body: unknown) => Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }))

class MockWebSocket {
  static instances: MockWebSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  url: string
  constructor(url: string) { this.url = url; MockWebSocket.instances.push(this); queueMicrotask(() => this.onopen?.()) }
  close() { this.onclose?.() }
}

beforeEach(() => {
  window.history.replaceState(null, '', '/')
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
  vi.stubGlobal('confirm', vi.fn(() => true))
})

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('Operations', () => {
  it('loads lazily, streams a job, cancels it and shows audit', async () => {
    let current = detail
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/v1/jobs' && !init?.method) return json({ jobs: [current.job] })
      if (path === `/api/v1/jobs/${job.id}`) return json(current)
      if (path === `/api/v1/jobs/${job.id}/cancel`) { current = { ...current, job: { ...current.job, status: 'cancelled', cancelRequestedAt: '2026-11-19T12:03:00Z', finishedAt: '2026-11-19T12:03:00Z' } }; return json({ job: current.job, rollback: false }) }
      if (path === '/api/v1/auth/csrf') return json({ token: 'test-csrf' })
      if (path === '/api/v1/audit') return json({ events: [{ id: 1, eventType: 'job.cancelled', sourceIp: 'internal', detail: 'job cancelled', createdAt: '2026-11-19T12:03:00Z' }] })
      return json({})
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<Operations />)
    expect(fetchMock).not.toHaveBeenCalled()

    window.location.hash = '#jobs'
    fireEvent(window, new HashChangeEvent('hashchange'))
    expect(await screen.findByText('vm.create')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /vm.create/ }))
    expect(await screen.findByText(/Задача добавлена/)).toBeInTheDocument()
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))
    MockWebSocket.instances[0].onmessage?.({ data: JSON.stringify({ ...detail, events: [...detail.events, { id: 2, jobId: job.id, level: 'info', message: 'Получено по WebSocket', createdAt: '2026-11-19T12:01:00Z' }] }) })
    expect(await screen.findByText(/Получено по WebSocket/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Отменить' }))
    expect(await screen.findByText('Отмена запрошена. Это не rollback: уже выполненные изменения могут остаться в Proxmox.')).toBeInTheDocument()

    window.location.hash = '#audit'
    fireEvent(window, new HashChangeEvent('hashchange'))
    expect(await screen.findByText('job.cancelled')).toBeInTheDocument()
  })
})
