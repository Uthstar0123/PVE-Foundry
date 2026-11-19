import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { Inventory } from './lib/api'

const user = { id: '1', email: 'admin@example.local', displayName: 'Администратор', role: 'administrator', createdAt: new Date().toISOString() }
const inventory: Inventory = {
  collectedAt: '2026-11-19T12:00:00Z',
  version: { version: '9.2.10', release: '9.2', repoid: 'test' },
  nodes: [{ node: 'pve-test', status: 'online', cpu: 0.12, maxcpu: 8, mem: 1024, maxmem: 8192, uptime: 3600 }],
  resources: [{ id: 'qemu/100', vmid: 100, name: 'foundry-test', type: 'qemu', node: 'pve-test', status: 'running', template: 0, maxcpu: 2, maxmem: 2147483648, maxdisk: 10737418240, pool: 'test' }],
  storage: [{ storage: 'local', node: 'pve-test', type: 'dir', content: 'iso,vztmpl', active: 1, enabled: 1, shared: 0, total: 1000, used: 250, avail: 750 }],
  networks: [{ iface: 'vmbr0', node: 'pve-test', type: 'bridge', active: 1, cidr: '192.168.80.149/24' }],
  pools: [{ poolid: 'test', comment: 'Test pool' }],
}
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))

function mockAuthenticatedInventory(inventoryResponse: () => Promise<Response>) {
  return vi.fn((input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/setup/status') return json({ required: false, authenticated: true })
    if (path === '/api/v1/auth/session') return json({ user })
    if (path === '/api/v1/proxmox/inventory') return inventoryResponse()
    return json({ error: { message: 'not found' } }, 404)
  })
}

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('App', () => {
  it('creates the initial administrator and displays the Proxmox inventory', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/v1/setup/status') return json({ required: true, authenticated: false })
      if (path === '/api/v1/auth/csrf') return json({ token: 'csrf-token' })
      if (path === '/api/v1/setup') return json({ user })
      if (path === '/api/v1/proxmox/inventory') return json(inventory)
      return json({ error: { message: 'not found' } }, 404)
    }))
    render(<BrowserRouter><App /></BrowserRouter>)
    fireEvent.change(await screen.findByLabelText('Ваше имя'), { target: { value: 'Администратор' } })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: user.email } })
    fireEvent.change(screen.getByLabelText('Пароль'), { target: { value: 'very-long-password' } })
    fireEvent.change(screen.getByLabelText('Повторите пароль'), { target: { value: 'very-long-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Создать и войти' }))
    expect(await screen.findByRole('heading', { name: 'Инфраструктура без ручной рутины.' })).toBeInTheDocument()
    expect(await screen.findByText('9.2.10')).toBeInTheDocument()
    expect(screen.getByText('foundry-test')).toBeInTheDocument()
    expect(screen.getByText('Администратор')).toBeInTheDocument()
  })

  it('shows login when setup is complete and blocks the panel without a session', async () => {
    vi.stubGlobal('fetch', vi.fn(() => json({ required: false, authenticated: false })))
    render(<BrowserRouter><App /></BrowserRouter>)
    expect(await screen.findByRole('heading', { name: 'С возвращением' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Инфраструктура без ручной рутины.' })).not.toBeInTheDocument()
  })

  it('validates password confirmation before sending setup', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => String(input) === '/api/v1/setup/status'
      ? json({ required: true, authenticated: false }) : json({ token: 'csrf-token' }))
    vi.stubGlobal('fetch', fetchMock)
    render(<BrowserRouter><App /></BrowserRouter>)
    fireEvent.change(await screen.findByLabelText('Ваше имя'), { target: { value: 'Администратор' } })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: user.email } })
    fireEvent.change(screen.getByLabelText('Пароль'), { target: { value: 'very-long-password' } })
    fireEvent.change(screen.getByLabelText('Повторите пароль'), { target: { value: 'different-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Создать и войти' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Пароли не совпадают'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('explains missing Proxmox configuration and retries on request', async () => {
    let configured = false
    vi.stubGlobal('fetch', mockAuthenticatedInventory(() => configured
      ? json(inventory)
      : json({ error: { code: 'proxmox_not_configured', message: 'Подключение Proxmox не настроено' } }, 503)))
    render(<BrowserRouter><App /></BrowserRouter>)
    expect(await screen.findByRole('heading', { name: 'Proxmox ещё не подключён' })).toBeInTheDocument()
    expect(screen.getByText(/MANAGEMENT_PROXMOX_API_URL/)).toBeInTheDocument()
    configured = true
    fireEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    expect(await screen.findByText('foundry-test')).toBeInTheDocument()
  })

  it('shows an actionable error when Proxmox is unavailable', async () => {
    vi.stubGlobal('fetch', mockAuthenticatedInventory(() => json({ error: { code: 'proxmox_unavailable', message: 'Не удалось получить инвентарь Proxmox' } }, 502)))
    render(<BrowserRouter><App /></BrowserRouter>)
    expect(await screen.findByRole('heading', { name: 'Не удалось получить инвентарь' })).toBeInTheDocument()
    expect(screen.getByText(/права токена PVEAuditor/)).toBeInTheDocument()
    expect(screen.queryByText('foundry-test')).not.toBeInTheDocument()
  })
})
