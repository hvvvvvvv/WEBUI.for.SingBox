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
const waitForTheme = async mode => {
  await page.waitForFunction(expected => document.body.getAttribute('theme-mode') === expected, mode)
  await page.evaluate(async () => {
    // The theme attribute changes inside the view-transition callback, before
    // its old-page overlay disappears. Observe completion without skipping or
    // replacing the application's real transition. Ignore infinite spinners.
    let timer
    const finished = async () => {
      const canObserveTransition = CSS.supports('selector(:active-view-transition)')
      let stableFrames = 0
      while (stableFrames < 2) {
        await new Promise(resolve => requestAnimationFrame(resolve))
        const animations = document.getAnimations().filter(animation =>
          (animation.playState === 'running' || animation.pending) &&
          Number.isFinite(animation.effect?.getComputedTiming().endTime),
        )
        if (animations.length) {
          stableFrames = 0
          await Promise.allSettled(animations.map(animation => animation.finished))
        } else if (canObserveTransition && document.documentElement.matches(':active-view-transition')) {
          stableFrames = 0
        } else {
          stableFrames++
        }
      }
    }
    try {
      await Promise.race([
        finished(),
        new Promise((_, reject) => {
          timer = setTimeout(() => reject(new Error('theme animations did not finish within 5 seconds')), 5000)
        }),
      ])
    } finally {
      clearTimeout(timer)
    }
  })
  assert.equal(await page.locator('body').getAttribute('theme-mode'), mode, 'theme changed while waiting for its animation')
}
const capture = async name => {
  const mode = name.endsWith('-dark') ? 'dark' : name.endsWith('-light') ? 'light' : null
  if (mode) await waitForTheme(mode)
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
const chooseTheme = async (name, mode) => {
  await radio(name).click()
  await waitForTheme(mode)
  if (process.env.VISUAL_TARGET === 'webui') {
    // The production preference writer is debounced by 500 ms. Do not reload
    // until it has persisted the choice made through the actual UI.
    await page.waitForFunction(expected => {
      try {
        return JSON.parse(localStorage.getItem('appSettings') || 'null')?.theme === expected
      } catch {
        return false
      }
    }, mode)
  }
}
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
const dropdownGeometry = []
const checkNetworkSettings = async (label, exercise = false) => {
  const network = page.locator('.network-settings')
  await network.scrollIntoViewIfNeeded()
  const modes = network.getByRole('radiogroup', { name: '请求代理', exact: true })
  assert.equal(await modes.getByRole('radio').count(), 4, 'request proxy must use the upstream segmented control')
  assert.equal(await network.getByRole('combobox').count(), 0, 'request proxy must not be a dropdown')
  assert.equal(await network.locator('input').count(), 0, 'network text fields must start in click-to-edit mode')
  assert.equal(await network.getByRole('switch', { name: 'GitHub 下载加速', exact: true }).count(), 1)
  const bounds = await modes.boundingBox()
  assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= page.viewportSize().width + 1, 'network controls must fit the viewport')
  await capture(label)
  if (!exercise) return
  const savedMode = await modes.locator('[aria-checked="true"]').innerText()
  const changeMode = async name => {
    const saved = page.waitForResponse(response => response.url().endsWith('/SaveAppConfig'))
    await modes.getByRole('radio', { name, exact: true }).click()
    assert.equal((await saved).status(), 200)
    await modes.locator('[disabled]').first().waitFor({ state: 'detached' })
  }
  await changeMode('不使用代理')
  assert.equal(await network.getByRole('button', { name: '测试', exact: true }).count(), 0)
  await changeMode(savedMode)
  assert.equal(await network.getByRole('button', { name: '测试', exact: true }).count(), 1)
  const agentName = '用户代理(User-Agent)'
  await network.getByRole('button', { name: agentName, exact: true }).click()
  const field = network.getByRole('textbox', { name: agentName, exact: true })
  const original = await field.inputValue()
  const saveAgent = async value => {
    await field.fill(value)
    const saved = page.waitForResponse(response => response.url().endsWith('/SaveAppConfig'))
    await field.press('Enter')
    assert.equal((await saved).status(), 200)
    await field.waitFor({ state: 'detached' })
  }
  await saveAgent('cloud-ui-regression')
  await network.getByRole('button', { name: agentName, exact: true }).click()
  await saveAgent(original)
  assert.equal(await network.getByRole('alert').count(), 0)
}
const checkDropdown = async (trigger, label) => {
  await trigger.scrollIntoViewIfNeeded()
  await trigger.click()
  const menu = page.locator('.gui-dropdown-overlay').filter({ visible: true }).last()
  await menu.waitFor()
  await menu.evaluate(async element => {
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    await Promise.allSettled(element.getAnimations({ subtree: true }).map(animation => animation.finished))
  })
  const anchor = await trigger.boundingBox()
  const popup = await menu.boundingBox()
  assert.ok(anchor && popup, `${label}: trigger and menu must be visible`)
  const viewport = page.viewportSize()
  const container = await trigger.evaluate(element => {
    const pane = element.closest('.shared-rule-action')
    if (!pane) return null
    const rect = pane.getBoundingClientRect()
    return { containerType: getComputedStyle(pane).containerType, x: rect.x, y: rect.y, width: rect.width, height: rect.height }
  })
  dropdownGeometry.push({ label, viewport, anchor, popup, container })
  const edgeGap = Math.min(Math.abs(popup.y - anchor.y - anchor.height), Math.abs(popup.y + popup.height - anchor.y))
  assert.ok(edgeGap <= 2, `${label}: popup must touch its trigger, actual gap ${edgeGap}`)
  assert.ok(popup.x >= 0 && popup.x + popup.width <= viewport.width + 1, `${label}: menu must fit horizontally`)
  assert.ok(popup.y >= 0 && popup.y + popup.height <= viewport.height + 1, `${label}: menu must fit vertically`)
  assert.ok(popup.x <= anchor.x + anchor.width && popup.x + popup.width >= anchor.x, `${label}: popup must overlap its trigger horizontally`)
  await capture(`dropdown-${label}`)
  await trigger.press('Escape')
  await menu.waitFor({ state: 'hidden' })
}
try {
  // Both upstreams intentionally default to English. Change language through
  // the real setting, rather than assuming the browser locale overrides it.
  await page.goto(`${baseURL}/#/settings`)
  await radio('简体中文').click()
  await chooseTheme('深色', 'dark')
  await capture('00-settings-defaults-dark')
  const features = page.locator('.gui-card').filter({ hasText: '组件轮廓' }).filter({ hasText: '禁用动画' }).last()
  await features.scrollIntoViewIfNeeded()
  for (const label of ['组件轮廓', '禁用动画', '禁用圆角', '使用指针光标', '使用左右标签布局']) {
    const row = features.locator('.px-8.py-12').filter({ hasText: label })
    assert.equal(await row.locator('.gui-switch').count(), 1, `expected one switch for ${label}`)
    assert.equal(await row.locator('.gui-switch.on').count(), 0, `${label} must be off on a fresh installation`)
  }
  // Official v1.28.1 has an additional Linux-only window-border switch.
  if (process.env.VISUAL_TARGET === 'webui') assert.equal(await features.locator('.gui-switch').count(), 5)
  await capture('00b-feature-defaults-dark')
  await page.getByText('个性化', { exact: true }).scrollIntoViewIfNeeded()
  // Keep Plugins visible without changing the other fresh-install choices.
  const pluginsToggle = page.locator('.gui-checkbox-button').filter({ hasText: /^\s*插件\s*$/ })
  if (!await pluginsToggle.evaluate(el => el.classList.contains('active'))) await pluginsToggle.click()
  // Measure the same empty-state components after real navigation. A plugin
  // wrapper that grows to 100% instead of the shared 90% shifts both elements.
  const temporarilyVisible = []
  for (const name of ['规则集', '计划任务']) {
    const toggle = page.locator('.gui-checkbox-button').filter({ hasText: new RegExp(`^\\s*${name}\\s*$`) })
    if (!await toggle.evaluate(el => el.classList.contains('active'))) {
      await toggle.click()
      temporarilyVisible.push(name)
    }
  }
  const emptyStates = []
  for (const [name, route] of [['订阅', '/subscriptions'], ['规则集', '/rulesets'], ['计划任务', '/scheduledtasks'], ['插件', '/plugins']]) {
    await navigate(name, route)
    const empty = page.locator('.grid-list-empty .gui-empty').filter({ visible: true })
    await empty.waitFor()
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    emptyStates.push({
      name,
      icon: await empty.locator(':scope > :first-child').boundingBox(),
      // Upstream subscriptions append an empty CustomAction container after
      // the description; measure the text row, not that optional extension.
      description: await empty.locator(':scope > .mt-12').boundingBox(),
    })
    await capture(`01-empty-${route.slice(1)}-dark`)
  }
  await fs.writeFile(path.join(output, 'empty-state-geometry.json'), JSON.stringify(emptyStates, null, 2))
  for (const state of emptyStates) {
    for (const part of ['icon', 'description']) {
      assert.ok(state[part], `${state.name} ${part} must be visible`)
      assert.ok(Math.abs(state[part].y - emptyStates[0][part].y) <= 1, `${state.name} ${part} must align with subscriptions`)
    }
  }
  if (process.env.VISUAL_TARGET === 'webui') {
    // Delay a real backend response, instead of replacing installed-plugin data.
    // The cached empty-state DOM must remain present on every animation frame.
    const emptyElement = await page.locator('.plugins-empty').elementHandle()
    let release
    let heldResponse
    await page.route('**/api/plugins', async route => {
      const response = await route.fetch()
      await heldResponse
      await route.fulfill({ response })
    })
    try {
      for (const [name, route] of [['订阅', '/subscriptions'], ['规则集', '/rulesets'], ['计划任务', '/scheduledtasks']]) {
        await navigate(name, route)
        heldResponse = new Promise(resolve => { release = resolve })
        const requestEntered = page.waitForRequest(request => new URL(request.url()).pathname === '/api/plugins')
        await navigate('插件', '/plugins')
        await requestEntered
        const frames = await emptyElement.evaluate(async element => {
          const frames = []
          for (let index = 0; index < 12; index++) {
            await new Promise(resolve => requestAnimationFrame(resolve))
            frames.push(element.isConnected && element.getBoundingClientRect().height > 0 && getComputedStyle(element).display !== 'none')
          }
          return frames
        })
        assert.ok(frames.every(Boolean), `plugin empty state disappeared during refresh after ${name}`)
        const completed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/plugins')
        release()
        await completed
      }
      await capture('01b-plugins-reactivation-dark')
    } finally {
      release?.()
      await page.unroute('**/api/plugins')
      await emptyElement.dispose()
    }
  }
  await navigate('设置', '/settings')
  for (const name of temporarilyVisible) {
    await page.locator('.gui-checkbox-button').filter({ hasText: new RegExp(`^\\s*${name}\\s*$`) }).click()
  }
  await navigate('插件', '/plugins')
  await page.getByText('插件列表为空', { exact: false }).waitFor()
  await capture('01-plugins-empty-dark')
  assert.equal(await page.getByRole('button', { name: '检查更新', exact: true }).filter({ visible: true }).count(), 0, 'empty plugins must not have an installed-list toolbar')
  await navigate('设置', '/settings')
  await page.getByText('页面可见性', { exact: true }).waitFor()
  await capture('02-settings-dark')
  if (process.env.VISUAL_TARGET === 'webui') {
    await checkNetworkSettings('02c-network-desktop-dark', true)
    await page.setViewportSize({ width: 390, height: 844 })
    await checkNetworkSettings('02d-network-mobile-dark')
    await page.setViewportSize({ width: 1440, height: 1000 })
  }
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
  await dialog().getByText('远程规则集使用的默认 HTTP 客户端', { exact: false }).waitFor()
  if (process.env.VISUAL_TARGET === 'webui') await checkDropdown(dialog().getByRole('combobox').first(), 'route-dialog-desktop-dark')
  await capture('06-route-general-dark')
  await tab('规则集').click()
  await dialog().getByText('Category-Ads', { exact: true }).waitFor()
  await capture('07-route-rulesets-dark')
  await editCard('Category-Ads')
  await dialog().getByText('自动更新间隔', { exact: false }).waitFor()
  await capture('08-ruleset-editor-dark')
  await closeDialog()
  await tab('规则').click()
  await editCard('GeoLocation-!CN')
  await dialog().getByText('规则类型', { exact: false }).waitFor()
  await capture('09-rule-editor-dark')
  await page.setViewportSize({ width: 390, height: 844 })
  await capture('10-rule-editor-mobile-dark')
  await closeDialog()
  await page.setViewportSize({ width: 1440, height: 1000 })
  if (process.env.VISUAL_TARGET === 'webui') {
    await nextStep('DNS设置', 6)
    await tab('规则').click()
    await editCard('GeoLocation-!CN')
    await dialog().locator('.shared-rule-action').waitFor()
    const actionPicker = dialog().getByRole('button', { name: '更换动作', exact: true })
    const dnsServer = dialog().locator('.shared-rule-action').getByRole('combobox').first()
    await checkDropdown(actionPicker, 'dns-action-desktop-dark')
    await checkDropdown(dnsServer, 'dns-server-desktop-dark')
    await page.setViewportSize({ width: 390, height: 844 })
    await checkDropdown(actionPicker, 'dns-action-mobile-dark')
    await checkDropdown(dnsServer, 'dns-server-mobile-dark')
    await closeDialog()
    await page.setViewportSize({ width: 1440, height: 1000 })
  }
  await closeDialog()
  await navigate('设置', '/settings')
  await chooseTheme('浅色', 'light')
  await capture('11-settings-light')
  if (process.env.VISUAL_TARGET === 'webui') await checkNetworkSettings('11b-network-desktop-light')
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
    await chooseTheme('深色', 'dark')
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
  await fs.writeFile(path.join(output, 'dropdown-geometry.json'), JSON.stringify({ browser: browser.version(), measurements: dropdownGeometry }, null, 2))
  await browser.close()
}
