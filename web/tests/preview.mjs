import { mkdir } from 'node:fs/promises'
import { chromium } from 'playwright'

const baseURL = process.env.PREVIEW_URL || 'http://127.0.0.1:7100/'
const browser = await chromium.launch({ channel: 'msedge', headless: true })
const errors = []

try {
  await mkdir('test-results', { recursive: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  page.on('console', (message) => {
    const expectedMissingProxmox = message.text().includes('503') && message.location().url.endsWith('/api/v1/proxmox/inventory')
    if (message.type() === 'error' && !expectedMissingProxmox) errors.push(`console: ${message.text()}`)
  })
  page.on('pageerror', (error) => errors.push(`page: ${error.message}`))
  page.on('response', (response) => {
    const expectedMissingProxmox = response.status() === 503 && response.url().endsWith('/api/v1/proxmox/inventory')
    if (response.status() >= 400 && !expectedMissingProxmox) errors.push(`http ${response.status()}: ${response.url()}`)
  })

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: 'Создайте администратора' }).waitFor()
  await page.screenshot({ path: 'test-results/setup-desktop.png', fullPage: true })
  await page.getByLabel('Ваше имя').fill('Integration Admin')
  await page.getByLabel('Email', { exact: true }).fill('integration@example.local')
  await page.getByLabel('Пароль', { exact: true }).fill('integration-test-password')
  await page.getByLabel('Повторите пароль').fill('integration-test-password')
  await page.getByRole('button', { name: 'Создать и войти' }).click()
  await page.getByRole('heading', { name: 'Инфраструктура без ручной рутины.' }).waitFor()
  await page.getByRole('heading', { name: 'Proxmox ещё не подключён' }).waitFor()
  await page.screenshot({ path: 'test-results/overview-desktop.png', fullPage: true })

  const logoutResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/logout'))
  await page.getByRole('button', { name: 'Выйти' }).click()
  console.log('logout status', (await logoutResponse).status())
  await page.getByRole('heading', { name: 'С возвращением' }).waitFor()
  await page.reload({ waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: 'С возвращением' }).waitFor()
  await page.getByLabel('Email', { exact: true }).fill('integration@example.local')
  await page.getByLabel('Пароль', { exact: true }).fill('integration-test-password')
  await page.getByRole('button', { name: 'Войти в панель' }).click()
  await page.getByRole('heading', { name: 'Инфраструктура без ручной рутины.' }).waitFor()

  const desktop = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth, title: document.title }))
  await page.setViewportSize({ width: 375, height: 812 })
  await page.getByRole('button', { name: 'Выйти' }).click()
  await page.getByRole('heading', { name: 'С возвращением' }).waitFor()
  await page.screenshot({ path: 'test-results/login-mobile.png', fullPage: true })
  const mobile = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }))
  if (desktop.scrollWidth > desktop.width || mobile.scrollWidth > mobile.width) errors.push(`horizontal overflow: ${JSON.stringify({ desktop, mobile })}`)

  console.log(JSON.stringify({ flow: 'setup → logout → login → logout', desktop, mobile, errors }, null, 2))
  if (errors.length > 0) process.exitCode = 1
} catch (error) {
  console.error(JSON.stringify({ errors, failure: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
