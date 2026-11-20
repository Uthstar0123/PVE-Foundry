import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlanApproval } from './Deployments'
import type { JobDetail } from '@/lib/api'

const detail: JobDetail = {
  job: { id: '5f9160cb-b4ce-4ee3-9f62-89027d34a101', operation: 'opentofu.plan_apply', resourceId: 'deployment/test', status: 'waiting_approval', initiatorUserId: 'admin', logsTruncated: false, createdAt: '2026-11-20T07:00:00Z', updatedAt: '2026-11-20T07:00:00Z' },
  steps: [], events: [], planText: '1 to change', artifactSha256: 'abc123', planSha256: 'def456',
}

beforeEach(() => { vi.stubGlobal('confirm', vi.fn(() => true)) })
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('PlanApproval', () => {
  it('pins approval to the displayed artifact hash', async () => {
    const approved = { ...detail, approvedAt: '2026-11-20T07:05:00Z' }
    const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
      if (String(input) === '/api/v1/auth/csrf') return Promise.resolve(new Response(JSON.stringify({ token: 'csrf' })))
      return Promise.resolve(new Response(JSON.stringify(approved), { status: 200 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const onApproved = vi.fn()
    render(<PlanApproval detail={detail} onApproved={onApproved} />)
    expect(screen.getByText('1 to change')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Подтвердить Apply' }))
    await waitFor(() => expect(onApproved).toHaveBeenCalledWith(approved))
    const call = fetchMock.mock.calls.find(([input]) => String(input).includes('/approve'))
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ artifactSha256: 'abc123' })
  })
})
