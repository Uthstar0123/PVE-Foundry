import { mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'

const baseURL = process.env.PREVIEW_URL || 'http://127.0.0.1:7100/'
const browser = await chromium.launch({ channel: 'msedge', headless: true })
const errors = []
const user = { id: 'admin', email: 'admin@example.local', displayName: 'Администратор', role: 'administrator', createdAt: '2026-11-20T07:00:00Z' }
const defaults = { nodeName: 'node-a', poolId: 'lab', datastoreId: 'data', bridge: 'vmbr0', nextVmid: 500, templates: [{ id: '900', vmid: 900, name: 'cloud', type: 'qemu' }] }
const job = { id: '5f9160cb-b4ce-4ee3-9f62-89027d34a301', operation: 'opentofu.plan_apply', resourceId: 'deployment/test', status: 'queued', initiatorUserId: 'admin', logsTruncated: false, createdAt: '2026-11-20T07:01:00Z', updatedAt: '2026-11-20T07:01:00Z' }
const requests = []

try {
  await mkdir('test-results', { recursive: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  page.on('console', (message) => { if (message.type() === 'error') errors.push(`console: ${message.text()}`) })
  page.on('pageerror', (error) => errors.push(`page: ${error.message}`))
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = {}
    if (path === '/api/v1/setup/status') body = { required: false, authenticated: true }
    else if (path === '/api/v1/auth/session') body = { user }
    else if (path === '/api/v1/auth/csrf') body = { token: 'browser-csrf' }
    else if (path === '/api/v1/proxmox/inventory') body = { collectedAt: job.updatedAt, version: { version: '9.2', release: '9.2', repoid: 'mock' }, nodes: [], resources: [], storage: [], networks: [], pools: [], templates: [], nextVmid: 500 }
    else if (path === '/api/v1/deployments/defaults') body = defaults
    else if (path.endsWith('/preflight')) {
      requests.push({ path, body: request.postDataJSON() })
      body = { defaults, variables: [{ name: 'node_name', required: false, sensitive: false, source: 'auto', value: 'node-a' }, { name: 'custom_role', required: true, sensitive: false, source: 'required' }] }
    }
    else if (path === '/api/v1/deployments/opentofu' || path.endsWith('/designer')) {
      requests.push({ path, body: request.postDataJSON() })
      body = { job: { ...job, resourceId: request.postDataJSON().resourceId }, artifactSha256: 'a'.repeat(64) }
    }
    else if (path === '/api/v1/jobs') body = { jobs: [] }
    else if (path.startsWith('/api/v1/jobs/')) body = { job, steps: [], events: [] }
    else if (path === '/api/v1/audit') body = { events: [] }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })
  await page.routeWebSocket(/\/ws\/jobs\//, () => {})

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('link', { name: 'Задачи', exact: true }).click()
  await page.locator('#opentofu-artifact').setInputFiles({ name: 'main.tf', mimeType: 'text/plain', buffer: Buffer.from('variable "node_name" { type = string }') })
  await page.getByLabel('custom_role').fill('database')
  const uploaded = page.waitForResponse((response) => response.url().endsWith('/api/v1/deployments/opentofu') && response.request().method() === 'POST')
  await page.getByRole('button', { name: 'Создать Plan' }).click()
  await uploaded

  await page.getByRole('tab', { name: 'Конструктор' }).click()
  await page.getByRole('button', { name: 'QEMU VM', exact: true }).click()
  await page.getByRole('button', { name: 'Сеть', exact: true }).click()
  await page.getByRole('button', { name: 'Хранилище', exact: true }).click()
  await page.getByRole('button', { name: 'Шаблон', exact: true }).click()
  await page.locator('.react-flow').scrollIntoViewIfNeeded()
  for (const source of ['network-2', 'storage-3', 'template-4']) {
    const from = await page.locator(`.react-flow__node[data-id="${source}"] .react-flow__handle.source`).boundingBox()
    const to = await page.locator('.react-flow__node[data-id="qemu-1"] .react-flow__handle.target').boundingBox()
    if (!from || !to) throw new Error(`Missing handle for ${source}`)
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
    await page.mouse.down()
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 15 })
    await page.mouse.up()
  }
  await page.locator('.react-flow__edge-path').first().waitFor({ state: 'attached', timeout: 3000 }).catch(() => {})
  const renderedEdges = await page.locator('.react-flow__edge-path').evaluateAll((paths) => paths.map((path) => ({
    d: path.getAttribute('d'),
    stroke: getComputedStyle(path).stroke,
    box: path.getBoundingClientRect().toJSON(),
  })))
  if (renderedEdges.length !== 3 || renderedEdges.some((edge) => !edge.d || edge.box.width === 0 || edge.box.height === 0)) errors.push(`edges are not visibly rendered: ${JSON.stringify(renderedEdges)}`)
  await page.screenshot({ path: 'test-results/stage6-designer-desktop.png', fullPage: true })
  const designed = page.waitForResponse((response) => response.url().endsWith('/api/v1/deployments/opentofu/designer') && response.request().method() === 'POST')
  await page.getByRole('button', { name: 'Сформировать Plan' }).click()
  await designed

  const dimensions = []
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    const size = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }))
    dimensions.push(size)
    if (size.scrollWidth > size.width) errors.push(`horizontal overflow: ${JSON.stringify(size)}`)
    if (width === 375) await page.screenshot({ path: 'test-results/stage6-designer-mobile.png', fullPage: true })
  }
  if (!requests.some((entry) => entry.path.endsWith('/preflight') && entry.body.files[0].name === 'main.tf')) errors.push('plain file preflight was not called')
  const designer = requests.find((entry) => entry.path.endsWith('/designer'))
  if (!designer || designer.body.design.edges.length !== 3) errors.push(`invalid designer request: ${JSON.stringify(designer)}`)
  console.log(JSON.stringify({ dataSource: 'Playwright mock API; no Proxmox changes', requests: requests.map((entry) => entry.path), designerEdges: designer?.body.design.edges.length, dimensions, errors }, null, 2))
  if (errors.length) process.exitCode = 1
} catch (error) {
  console.error(JSON.stringify({ errors, failure: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
