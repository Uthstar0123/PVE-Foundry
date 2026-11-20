import { mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'

const baseURL = process.env.PREVIEW_URL || 'http://127.0.0.1:7100/'
const browser = await chromium.launch({ channel: 'msedge', headless: true })
const errors = []
const user = { id: 'admin', email: 'admin@example.local', displayName: 'Администратор', role: 'administrator', createdAt: '2026-11-19T10:00:00Z' }
const running = { id: '5f9160cb-b4ce-4ee3-9f62-89027d34a101', operation: 'vm.create', resourceId: 'qemu/example-web', status: 'running', initiatorUserId: 'admin', workerId: 'runner-test', logsTruncated: false, createdAt: '2026-11-19T11:58:00Z', startedAt: '2026-11-19T11:59:00Z', updatedAt: '2026-11-19T12:00:00Z' }
const failed = { ...running, id: '5f9160cb-b4ce-4ee3-9f62-89027d34a102', operation: 'lxc.create', resourceId: 'lxc/example-dns', status: 'failed', errorCode: 'lease_expired', finishedAt: '2026-11-19T12:02:00Z' }
let detail = { job: running, steps: [{ id: 'step-1', jobId: running.id, name: 'validate', status: 'succeeded', detail: '', createdAt: running.createdAt, updatedAt: running.startedAt }], events: [{ id: 1, jobId: running.id, level: 'info', message: 'Конфигурация проверена', createdAt: running.startedAt }] }

try {
  await mkdir('test-results', { recursive: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, acceptDownloads: true })
  page.on('console', (message) => { if (message.type() === 'error') errors.push(`console: ${message.text()}`) })
  page.on('pageerror', (error) => errors.push(`page: ${error.message}`))
  page.on('response', (response) => { if (response.status() >= 400 && !response.url().endsWith('/proxmox/inventory')) errors.push(`http ${response.status()}: ${response.url()}`) })
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = {}
    let status = 200
    let headers = {}
    if (path === '/api/v1/setup/status') body = { required: false, authenticated: true }
    else if (path === '/api/v1/auth/session') body = { user }
    else if (path === '/api/v1/proxmox/inventory') body = { collectedAt: '2026-11-19T12:00:00Z', version: { version: '9.2.10', release: '9.2', repoid: 'mock' }, nodes: [], resources: [], storage: [], networks: [], pools: [] }
    else if (path === '/api/v1/jobs') body = { jobs: [detail.job, failed] }
    else if (path === `/api/v1/jobs/${running.id}`) body = detail
    else if (path === `/api/v1/jobs/${failed.id}`) body = { job: failed, steps: [], events: [{ id: 2, jobId: failed.id, level: 'error', message: 'Lease expired; manual reconciliation required', createdAt: failed.finishedAt }] }
    else if (path === `/api/v1/jobs/${running.id}/cancel`) { detail = { ...detail, job: { ...detail.job, status: 'cancelled', cancelRequestedAt: '2026-11-19T12:04:00Z', finishedAt: '2026-11-19T12:04:00Z' } }; body = { job: detail.job, rollback: false } }
    else if (path === `/api/v1/jobs/${running.id}/logs`) {
      await route.fulfill({ status: 200, contentType: 'text/plain', headers: { 'Content-Disposition': 'attachment; filename=job-test.log' }, body: '2026-11-19T12:00:00Z [info] safe log\n' })
      return
    }
    else if (path === '/api/v1/auth/csrf') body = { token: 'browser-csrf' }
    else if (path === '/api/v1/audit') body = { events: [{ id: 1, eventType: 'job.started', userId: 'admin', sourceIp: 'internal', detail: 'job started by runner-test', createdAt: running.startedAt }] }
    await route.fulfill({ status, contentType: 'application/json', headers, body: JSON.stringify(body) })
  })
  await page.routeWebSocket(/\/ws\/jobs\//, (socket) => { socket.send(JSON.stringify(detail)) })

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: 'Инфраструктура без ручной рутины.' }).waitFor()
  await page.getByRole('link', { name: 'Задачи', exact: true }).click()
  await page.getByRole('button', { name: /vm.create/ }).click()
  await page.getByText(/Конфигурация проверена/).waitFor()
  await page.getByText('WebSocket активен', { exact: true }).waitFor()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('link', { name: 'Скачать лог' }).click()
  const download = await downloadPromise
  if (!download.suggestedFilename().endsWith('.log')) errors.push(`download filename: ${download.suggestedFilename()}`)
  await page.screenshot({ path: 'test-results/jobs-desktop.png', fullPage: true })

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: 'Отменить', exact: true }).click()
  await page.getByText(/Отмена запрошена. Это не rollback/).waitFor()
  await page.getByRole('button', { name: /lxc.create/ }).click()
  await page.getByText(/Результат внешней операции может быть неизвестен/).waitFor()

  await page.getByRole('link', { name: 'Аудит', exact: true }).click()
  await page.getByText('job.started', { exact: true }).waitFor()
  const dimensions = []
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    const size = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }))
    dimensions.push(size)
    if (size.scrollWidth > size.width) errors.push(`horizontal overflow: ${JSON.stringify(size)}`)
    if (width === 375) await page.screenshot({ path: 'test-results/jobs-mobile.png', fullPage: true })
  }
  console.log(JSON.stringify({ dataSource: 'Playwright mock API and WebSocket', dimensions, errors }, null, 2))
  if (errors.length) process.exitCode = 1
} catch (error) {
  console.error(JSON.stringify({ errors, failure: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
