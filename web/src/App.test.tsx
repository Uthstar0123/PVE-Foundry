import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'

const user = { id: '1', email: 'admin@example.local', displayName: 'Администратор', role: 'administrator', createdAt: new Date().toISOString() }
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('App', () => {
  it('creates the initial administrator and opens the protected panel', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/v1/setup/status') return json({ required: true, authenticated: false })
      if (path === '/api/v1/auth/csrf') return json({ token: 'csrf-token' })
      if (path === '/api/v1/setup') return json({ user })
      return json({ error: { message: 'not found' } }, 404)
    }))
    render(<BrowserRouter><App /></BrowserRouter>)
    fireEvent.change(await screen.findByLabelText('Ваше имя'), { target: { value: 'Администратор' } })
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: user.email } })
    fireEvent.change(screen.getByLabelText('Пароль'), { target: { value: 'very-long-password' } })
    fireEvent.change(screen.getByLabelText('Повторите пароль'), { target: { value: 'very-long-password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Создать и войти' }))
    expect(await screen.findByRole('heading', { name: 'Инфраструктура без ручной рутины.' })).toBeInTheDocument()
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
})
