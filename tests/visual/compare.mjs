// Cloud-only visual/interaction regression. Both targets use their real Go
// backend; profile drafts are cancelled and the proxy core stays off.
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const { chromium } = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const [baseURL, output] = process.argv.slice(2)
assert.ok(baseURL?.startsWith('http://127.0.0.1:'))
const browser = await chromium.launch()
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, colorScheme: 'dark', locale: 'zh-CN' })
const page = await context.newPage()
const errors = []
page.on('pageerror', error => errors.push(error.stack || error.message))
page.setDefaultTimeout(20000)
const capture = async name => {
  await page.screenshot({ path: path.join(output, `${name}.png`), fullPage: true, animations: 'disabled' })
  await fs.writeFile(path.join(output, `${name}.txt`), await page.locator('body').innerText())
}
const button = name => page.getByRole('button', { name, exact: true }).filter({ visible: true }).last()
// Scope navigation separately from identically named settings tabs and page
// visibility controls. These classes are from v1.28.1 and the current WebUI.
const navigate = async (name, route) => {
  await page.locator('.main-nav, .app-navigation').getByText(name, { exact: true }).click()
  await page.waitForURL(url => url.hash === `#${route}`)
}
const radio = name => page.locator('.gui-radio').getByText(name, { exact: true }).filter({ visible: true })
const tab = name => page.locator('.gui-tabs-tab').filter({ visible: true }).last().getByText(name, { exact: true })
const dialog = () => page.locator('.gui-modal-modal').filter({ visible: true }).last()
const dialogButton = name => dialog().getByRole('button', { name, exact: true })
const closeDialog = async (name = '取消') => {
  // Keep the original element: a live .last() locator would resolve to the
  // parent modal as soon as this one closes. Wait out its 200 ms transition.
  const closing = await dialog().elementHandle()
  assert.ok(closing, 'expected an open dialog')
  try {
    await dialogButton(name).click()
    await closing.waitForElementState('hidden')
  } finally {
    await closing.dispose()
  }
}
const nextStep = async (name, number) => {
  await dialogButton('下一步').click()
  await dialog().locator('.gui-modal-header').getByText(`${name} （${number} / 7）`, { exact: true }).waitFor()
}
const editCard = async text => {
  const card = dialog().locator('.gui-card').filter({ hasText: text }).filter({ visible: true }).last()
  await card.waitFor()
  const actions = card.getByRole('button')
  const count = await actions.count()
  assert.ok(count >= 2, `expected edit/delete actions for ${text}`)
  // Edit/Delete are the final pair in both sources. WebUI also has Copy
  // before Edit, so clicking the first button would silently test copying.
  await actions.nth(count - 2).click()
}
try {
  // Both upstreams intentionally default to English. Change language through
  // the real setting, rather than assuming the browser locale overrides it.
  await page.goto(`${baseURL}/#/settings`)
  await radio('简体中文').click()
  await radio('深色').click()
  await capture('00-settings-defaults-dark')
  // Plugins and scheduled tasks are hidden on a fresh official installation.
  const pluginsToggle = page.locator('.gui-checkbox-button').filter({ hasText: /^\s*插件\s*$/ })
  if (!await pluginsToggle.evaluate(el => el.classList.contains('active'))) await pluginsToggle.click()
  await navigate('插件', '/plugins')
  await page.getByText('插件列表为空', { exact: false }).waitFor()
  await capture('01-plugins-empty-dark')
  assert.equal(await page.getByRole('button', { name: '检查更新', exact: true }).filter({ visible: true }).count(), 0, 'empty plugins must not have an installed-list toolbar')
  await navigate('设置', '/settings')
  await page.getByText('页面可见性', { exact: true }).waitFor()
  await capture('02-settings-dark')
  await tab('插件').click()
  await page.getByText('配置插件', { exact: true }).waitFor()
  await capture('02b-plugin-settings-empty-dark')
  await tab('通用').click()
  await button('关于').click()
  await dialog().getByText(/Bridge:.*- UI:/).waitFor()
  await capture('03-about-dark')
  await closeDialog('关闭')
  await navigate('配置', '/profiles')
  await button('添加').click()
  await dialog().getByPlaceholder('名称', { exact: true }).fill('云端界面对照')
  await nextStep('通用设置', 2)
  await dialog().getByText('禁用日志', { exact: true }).waitFor()
  await capture('04-general-dark')
  await dialogButton('更多').click()
  await dialog().getByText('启用缓存', { exact: true }).waitFor()
  await capture('05-general-more-dark')
  await nextStep('入站设置', 3)
  await nextStep('出站设置', 4)
  await nextStep('路由设置', 5)
  await dialog().getByText('远程规则集使用的默认 HTTP 客户端', { exact: true }).waitFor()
  await capture('06-route-general-dark')
  await tab('规则集').click()
  await dialog().getByText('Category-Ads', { exact: true }).waitFor()
  await capture('07-route-rulesets-dark')
  await editCard('Category-Ads')
  await dialog().getByText('自动更新间隔', { exact: true }).waitFor()
  await capture('08-ruleset-editor-dark')
  await closeDialog()
  await tab('规则').click()
  await editCard('GeoLocation-!CN')
  await dialog().getByText('规则类型', { exact: true }).waitFor()
  await capture('09-rule-editor-dark')
  await page.setViewportSize({ width: 390, height: 844 })
  await capture('10-rule-editor-mobile-dark')
  await closeDialog()
  await closeDialog()
  await page.setViewportSize({ width: 1440, height: 1000 })
  await navigate('设置', '/settings')
  await radio('浅色').click()
  await capture('11-settings-light')
  await navigate('插件', '/plugins')
  await page.getByText('插件列表为空', { exact: false }).waitFor()
  await capture('12-plugins-empty-light')
  await page.setViewportSize({ width: 390, height: 844 })
  await capture('13-plugins-mobile-light')
  if (process.env.VISUAL_TARGET === 'webui') {
    await page.setViewportSize({ width: 1440, height: 1000 })
    await navigate('设置', '/settings')
    const png = await page.evaluate(() => {
      const canvas = document.createElement('canvas')
      canvas.width = 800; canvas.height = 600
      const ctx = canvas.getContext('2d')
      const gradient = ctx.createLinearGradient(0, 0, 800, 600)
      gradient.addColorStop(0, '#1859a8'); gradient.addColorStop(1, '#fdab68')
      ctx.fillStyle = gradient; ctx.fillRect(0, 0, 800, 600)
      return canvas.toDataURL('image/png').split(',')[1]
    })
    await page.locator('.personalization-settings input[type=file][accept="image/png,image/jpeg,image/webp"]').setInputFiles({ name: 'cloud-test-background.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') })
    await page.locator('.app-background-image').waitFor()
    await capture('14-background-light')
    await radio('深色').click()
    await capture('15-background-dark')
    await page.reload()
    await page.locator('.app-background-image').waitFor()
    await page.getByText('背景图片', { exact: true }).waitFor()
    await page.setViewportSize({ width: 390, height: 844 })
    await capture('16-background-mobile-dark')
    await page.locator('.personalization-settings').getByRole('button', { name: '移除', exact: true }).click()
    await page.locator('.app-background-image').waitFor({ state: 'detached' })
  }
  await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ target: process.env.VISUAL_TARGET, errors, completed: true }, null, 2))
  // Preserve upstream exceptions as evidence instead of patching the reference
  // or confusing a Wails dev-browser limitation with our application's result.
  if (process.env.VISUAL_TARGET === 'webui') assert.deepEqual(errors, [], 'application emitted uncaught browser errors')
} catch (error) {
  await capture('failure').catch(() => {})
  await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ target: process.env.VISUAL_TARGET, errors, completed: false, failure: String(error) }, null, 2))
  throw error
} finally {
  await browser.close()
}
