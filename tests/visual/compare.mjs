// Cloud-only visual/interaction regression. Both targets use their real Go
// backend; only disposable profiles are created and the proxy core stays off.
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
  await page.screenshot({ path: path.join(output, `${name}.png`), fullPage: true })
  await fs.writeFile(path.join(output, `${name}.txt`), await page.locator('body').innerText())
}
const clickText = text => page.getByText(text, { exact: true }).filter({ visible: true }).first().click()
const button = name => page.getByRole('button', { name, exact: true }).filter({ visible: true }).last()
try {
  // Both upstreams intentionally default to English. Change language through
  // the real setting, rather than assuming the browser locale overrides it.
  await page.goto(`${baseURL}/#/settings`)
  await button('简体中文').click()
  await button('深色').click()
  await clickText('插件')
  await page.getByText('插件列表为空', { exact: false }).waitFor()
  await capture('01-plugins-empty-dark')
  assert.equal(await page.getByRole('button', { name: '检查更新', exact: true }).count(), 0, 'empty plugins must not have an installed-list toolbar')
  await clickText('设置')
  await page.getByText('页面可见性', { exact: true }).waitFor()
  await capture('02-settings-dark')
  await page.getByRole('tab', { name: '插件', exact: true }).click()
  await page.getByText('配置插件', { exact: true }).waitFor()
  await capture('02b-plugin-settings-empty-dark')
  await page.getByRole('tab', { name: '通用', exact: true }).click()
  await clickText('关于')
  await page.getByText(/Bridge:/).waitFor()
  await capture('03-about-dark')
  await button('关闭').click()
  await clickText('配置')
  await button('添加').click()
  await page.getByPlaceholder('名称', { exact: true }).fill('云端界面对照')
  await button('下一步').click()
  await page.getByText('禁用日志', { exact: true }).waitFor()
  await capture('04-general-dark')
  await button('更多').click()
  await capture('05-general-more-dark')
  await button('下一步').click()
  await button('下一步').click()
  await button('下一步').click()
  await page.getByText('远程规则集使用的默认 HTTP 客户端', { exact: true }).waitFor()
  await capture('06-route-general-dark')
  await page.getByRole('tab', { name: '规则集', exact: true }).click()
  await page.getByText('Category-Ads', { exact: true }).waitFor()
  await capture('07-route-rulesets-dark')
  const rulesetCard = page.locator('.gui-card').filter({ hasText: 'Category-Ads' }).last()
  await rulesetCard.getByRole('button').first().click()
  await page.getByText('自动更新间隔', { exact: true }).waitFor()
  await capture('08-ruleset-editor-dark')
  await button('取消').click()
  await page.getByRole('tab', { name: '规则', exact: true }).click()
  const ruleCard = page.locator('.gui-card').filter({ hasText: 'GeoLocation-!CN' }).last()
  await ruleCard.getByRole('button').first().click()
  await page.getByText('规则类型', { exact: true }).waitFor()
  await capture('09-rule-editor-dark')
  await page.setViewportSize({ width: 390, height: 844 })
  await capture('10-rule-editor-mobile-dark')
  await button('取消').click()
  await button('取消').click()
  await page.setViewportSize({ width: 1440, height: 1000 })
  await clickText('设置')
  await button('浅色').click()
  await capture('11-settings-light')
  await clickText('插件')
  await capture('12-plugins-empty-light')
  await page.setViewportSize({ width: 390, height: 844 })
  await capture('13-plugins-mobile-light')
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
