import { mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'

const baseURL = process.env.PREVIEW_URL || 'http://127.0.0.1:7100/'
const browser = await chromium.launch({ channel: 'msedge', headless: true })
const errors = []
const artifactSha256 = 'a'.repeat(64)
const user = { id: 'admin', email: 'admin@example.local', displayName: 'Администратор', role: 'administrator', createdAt: '2026-11-20T07:00:00Z' }
const job = { id: '5f9160cb-b4ce-4ee3-9f62-89027d34a201', operation: 'opentofu.plan_apply', resourceId: 'deployment/browser-test', status: 'waiting_approval', initiatorUserId: 'admin', workerId: 'runner-test', logsTruncated: false, createdAt: '2026-11-20T07:01:00Z', startedAt: '2026-11-20T07:02:00Z', updatedAt: '2026-11-20T07:03:00Z' }
let detail = { job, steps: [{ id: 'step-plan', jobId: job.id, name: 'plan', status: 'succeeded', detail: '', createdAt: job.createdAt, updatedAt: job.updatedAt }], events: [{ id: 1, jobId: job.id, level: 'info', message: 'План сохранён', createdAt: job.updatedAt }], artifactSha256, planSha256: 'b'.repeat(64), planText: 'OpenTofu will perform the following actions:\n\nPlan: 1 to add, 0 to change, 0 to destroy.' }
let createBody
let approveBody

try {
  await mkdir('test-results', { recursive: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  page.on('console', (message) => { if (message.type() === 'error') errors.push(`console: ${message.text()}`) })
  page.on('pageerror', (error) => errors.push(`page: ${error.message}`))
  page.on('response', (response) => { if (response.status() >= 400) errors.push(`http ${response.status()}: ${response.url()}`) })
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = {}
    if (path === '/api/v1/setup/status') body = { required: false, authenticated: true }
    else if (path === '/api/v1/auth/session') body = { user }
    else if (path === '/api/v1/auth/csrf') body = { token: 'browser-csrf' }
    else if (path === '/api/v1/proxmox/inventory') body = { collectedAt: job.updatedAt, version: { version: '9.2.10', release: '9.2', repoid: 'mock' }, nodes: [], resources: [], storage: [], networks: [], pools: [] }
    else if (path === '/api/v1/deployments/opentofu' && request.method() === 'POST') {
      createBody = request.postDataJSON()
      body = { job, artifactSha256 }
    }
    else if (path === '/api/v1/jobs') body = { jobs: [detail.job] }
    else if (path === `/api/v1/jobs/${job.id}/approve` && request.method() === 'POST') {
      approveBody = request.postDataJSON()
      detail = { ...detail, approvedAt: '2026-11-20T07:04:00Z', approvalExpiresAt: '2026-11-20T07:14:00Z' }
      body = detail
    }
    else if (path === `/api/v1/jobs/${job.id}`) body = detail
    else if (path === '/api/v1/audit') body = { events: [] }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })
  await page.routeWebSocket(/\/ws\/jobs\//, (socket) => { socket.send(JSON.stringify(detail)) })

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('link', { name: 'Задачи', exact: true }).click()
  await page.getByLabel('Идентификатор deployment').fill('deployment/browser-test')
  await page.locator('#opentofu-artifact').setInputFiles({ name: 'deployment.tar.gz', mimeType: 'application/gzip', buffer: Buffer.from('mock-open-tofu-archive') })
  await page.getByRole('button', { name: 'Создать Plan' }).click()
  await page.getByRole('heading', { name: 'Сохранённый план' }).waitFor()
  await page.getByText(/Plan: 1 to add, 0 to change, 0 to destroy/).waitFor()
  if (createBody?.resourceId !== 'deployment/browser-test' || !createBody?.idempotencyKey || !createBody?.artifact) errors.push(`invalid create body: ${JSON.stringify(createBody)}`)

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Подтвердить Apply' }).click()
  await page.getByText(/Apply подтверждён/).waitFor()
  if (approveBody?.artifactSha256 !== artifactSha256) errors.push(`invalid approval body: ${JSON.stringify(approveBody)}`)
  await page.screenshot({ path: 'test-results/deployments-desktop.png', fullPage: true })

  const dimensions = []
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    const size = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }))
    dimensions.push(size)
    if (size.scrollWidth > size.width) errors.push(`horizontal overflow: ${JSON.stringify(size)}`)
    if (width === 375) await page.screenshot({ path: 'test-results/deployments-mobile.png', fullPage: true })
  }
  console.log(JSON.stringify({ dataSource: 'Playwright mock API and WebSocket; no Proxmox changes', createBody: createBody && { resourceId: createBody.resourceId, artifactBytes: createBody.artifact.length }, approveBody, dimensions, errors }, null, 2))
  if (errors.length) process.exitCode = 1
} catch (error) {
  console.error(JSON.stringify({ errors, failure: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
