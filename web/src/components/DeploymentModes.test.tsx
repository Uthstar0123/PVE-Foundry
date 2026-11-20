import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeploymentForm } from './Deployments'
import type { DeploymentDesign } from '@/lib/api'

vi.mock('./InfrastructureDesigner', () => ({
  InfrastructureDesigner: ({ value, onChange, onSubmit }: { value: DeploymentDesign; onChange: (value: DeploymentDesign) => void; onSubmit: () => void }) => <div>
    <button type="button" onClick={() => onChange({ ...value, nodes: [{ id: 'vm', kind: 'lxc', label: 'foundry-500', x: 0, y: 0, config: {} }] })}>Добавить тестовый блок</button>
    <button type="button" onClick={onSubmit}>Отправить схему</button>
  </div>,
}))

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
const job = { id: 'job-1', operation: 'opentofu.plan_apply', resourceId: 'deployment/upload', status: 'queued', initiatorUserId: 'admin', logsTruncated: false, createdAt: '', updatedAt: '' }

describe('Deployment modes', () => {
  it('uploads a plain tf file and asks only for unknown variables', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/v1/auth/csrf') return json({ token: 'csrf' })
      if (path.endsWith('/preflight')) return json({ defaults: { nodeName: 'node-a', poolId: 'lab', datastoreId: 'data', bridge: 'vmbr0', nextVmid: 500, templates: [] }, variables: [
        { name: 'node_name', required: false, sensitive: false, source: 'auto', value: 'node-a' },
        { name: 'custom_role', required: true, sensitive: false, source: 'required' },
      ] })
      if (path.endsWith('/opentofu')) return json({ job, artifactSha256: 'abc' }, 201)
      return json({})
    })
    vi.stubGlobal('fetch', fetchMock)
    const created = vi.fn()
    render(<DeploymentForm onCreated={created} />)
    fireEvent.change(screen.getByLabelText('Файл конфигурации'), { target: { files: [new File(['terraform {}'], 'main.tf', { type: 'text/plain' })] } })
    expect(await screen.findByLabelText('custom_role')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('custom_role'), { target: { value: 'database' } })
    fireEvent.click(screen.getByRole('button', { name: 'Создать Plan' }))
    await waitFor(() => expect(created).toHaveBeenCalledWith(job))
    const call = fetchMock.mock.calls.find(([input, init]) => String(input).endsWith('/opentofu') && init?.method === 'POST')
    const body = JSON.parse(String(call?.[1]?.body))
    expect(body.files[0].name).toBe('main.tf')
    expect(body.artifact).toBeUndefined()
    expect(body.variables).toEqual({ node_name: 'node-a', custom_role: 'database' })
  })

  it('submits visual design to the dedicated generator', async () => {
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>((input) => {
      const path = String(input)
      if (path === '/api/v1/auth/csrf') return json({ token: 'csrf' })
      if (path.endsWith('/defaults')) return json({ nodeName: 'node-a', poolId: 'lab', datastoreId: 'data', bridge: 'vmbr0', nextVmid: 500, templates: [] })
      if (path.endsWith('/designer')) return json({ job, artifactSha256: 'abc' }, 201)
      return json({})
    })
    vi.stubGlobal('fetch', fetchMock)
    const created = vi.fn()
    render(<DeploymentForm onCreated={created} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Конструктор' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Добавить тестовый блок' }))
    fireEvent.click(screen.getByRole('button', { name: 'Отправить схему' }))
    await waitFor(() => expect(created).toHaveBeenCalled())
    const call = fetchMock.mock.calls.find(([input]) => String(input).endsWith('/designer'))
    expect(JSON.parse(String(call?.[1]?.body)).design.nodes).toHaveLength(1)
  })
})
