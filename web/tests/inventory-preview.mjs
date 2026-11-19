import { mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'

const baseURL = process.env.PREVIEW_URL || 'http://127.0.0.1:7100/'
const browser = await chromium.launch({ channel: 'msedge', headless: true })
const errors = []
const user = { id: 'admin', email: 'admin@example.local', displayName: 'Администратор', role: 'administrator', createdAt: '2026-11-19T10:00:00Z' }
const inventory = {
  collectedAt: '2026-11-19T12:00:00Z',
  version: { version: '9.2.10', release: '9.2', repoid: '43df2e01f27a1a19' },
  nodes: [{ node: 'pve-test', status: 'online', cpu: 0.125, maxcpu: 8, mem: 4294967296, maxmem: 17179869184, uptime: 345600 }],
  resources: [
    { id: 'qemu/100', vmid: 100, name: 'foundry-web', type: 'qemu', node: 'pve-test', status: 'running', template: 0, maxcpu: 4, maxmem: 4294967296, maxdisk: 10737418240, pool: 'orchestrator-test' },
    { id: 'lxc/101', vmid: 101, name: 'foundry-dns', type: 'lxc', node: 'pve-test', status: 'stopped', template: 0, maxcpu: 2, maxmem: 1073741824, maxdisk: 8589934592, pool: 'orchestrator-test' },
  ],
  storage: [
    { storage: 'local', node: 'pve-test', type: 'dir', content: 'iso,vztmpl,backup', active: 1, enabled: 1, shared: 0, total: 107374182400, used: 26843545600, avail: 80530636800 },
    { storage: 'local-lvm', node: 'pve-test', type: 'lvmthin', content: 'rootdir,images', active: 1, enabled: 1, shared: 0, total: 858993459200, used: 549755813888, avail: 309237645312 },
  ],
  networks: [
    { iface: 'vmbr0', node: 'pve-test', type: 'bridge', active: 1, bridge_ports: 'eno1', cidr: '192.168.80.149/24', gateway: '192.168.80.1' },
    { iface: 'eno1', node: 'pve-test', type: 'eth', active: 1 },
  ],
  pools: [{ poolid: 'orchestrator-test', comment: 'Изолированный тестовый контур' }],
}

async function mockAPI(page, getInventory) {
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body
    let status = 200
    if (path === '/api/v1/setup/status') body = { required: false, authenticated: true }
    else if (path === '/api/v1/auth/session') body = { user }
    else if (path === '/api/v1/proxmox/inventory') {
      const result = getInventory()
      body = result.body
      status = result.status
    } else if (path === '/api/v1/auth/logout') body = { ok: true }
    else status = 404
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body ?? {}) })
  })
}

try {
  await mkdir('test-results', { recursive: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  let inventoryRequests = 0
  await mockAPI(page, () => { inventoryRequests += 1; return { status: 200, body: inventory } })
  page.on('console', (message) => { if (message.type() === 'error') errors.push(`console: ${message.text()}`) })
  page.on('pageerror', (error) => errors.push(`page: ${error.message}`))
  page.on('response', (response) => { if (response.status() >= 400) errors.push(`http ${response.status()}: ${response.url()}`) })

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: 'Инфраструктура без ручной рутины.' }).waitFor()
  await page.getByText('foundry-web', { exact: true }).waitFor()
  await page.getByText('foundry-web', { exact: true }).click()
  await page.getByText('10 ГиБ', { exact: true }).waitFor()

  for (const [name, hash] of [['Хранилища', '#storage'], ['Сеть', '#network'], ['Обзор', '#overview']]) {
    await page.getByRole('link', { name, exact: true }).click()
    if (new URL(page.url()).hash !== hash) errors.push(`navigation ${name}: ${page.url()}`)
  }
  const requestsBeforeRefresh = inventoryRequests
  const refreshed = page.waitForResponse((response) => response.url().endsWith('/api/v1/proxmox/inventory'))
  await page.getByRole('button', { name: 'Обновить' }).click()
  await refreshed
  if (inventoryRequests <= requestsBeforeRefresh) errors.push('refresh did not request inventory')

  await page.screenshot({ path: 'test-results/inventory-desktop.png', fullPage: true })
  const dimensions = []
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    const size = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }))
    dimensions.push(size)
    if (size.scrollWidth > size.width) errors.push(`horizontal overflow: ${JSON.stringify(size)}`)
    if (width === 375) await page.screenshot({ path: 'test-results/inventory-mobile.png', fullPage: true })
  }

  const unconfigured = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  let configured = false
  await mockAPI(unconfigured, () => configured
    ? { status: 200, body: inventory }
    : { status: 503, body: { error: { code: 'proxmox_not_configured', message: 'Подключение Proxmox не настроено' } } })
  await unconfigured.goto(baseURL, { waitUntil: 'networkidle' })
  await unconfigured.getByRole('heading', { name: 'Proxmox ещё не подключён' }).waitFor()
  await unconfigured.screenshot({ path: 'test-results/inventory-not-configured.png', fullPage: true })
  configured = true
  await unconfigured.getByRole('button', { name: 'Повторить', exact: true }).click()
  await unconfigured.getByText('foundry-web', { exact: true }).waitFor()

  console.log(JSON.stringify({ dataSource: 'Playwright mock, not real Proxmox', inventoryRequests, dimensions, errors }, null, 2))
  if (errors.length > 0) process.exitCode = 1
} catch (error) {
  console.error(JSON.stringify({ errors, failure: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
