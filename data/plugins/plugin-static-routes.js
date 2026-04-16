/**
 * 静态路由管理插件
 *
 * 功能：
 * 1. 配置项支持添加若干条静态路由
 * 2. 手动触发时可管理当前后端系统的静态路由（列出、添加、删除）
 * 3. 核心启动后自动添加配置项定义的静态路由
 * 4. 核心停止后自动删除配置项定义的静态路由
 */

/**
 * 解析配置项中的路由列表
 * 每条格式: "目标网段 via 网关 [dev 接口]"
 * 示例: "10.0.0.0/8 via 192.168.1.1"
 *       "172.16.0.0/12 via 192.168.1.1 dev eth0"
 */
const parseRoutes = () => {
  const lines = Plugin.Routes || []
  const routes = []
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    routes.push(trimmed)
  }
  return routes
}

/**
 * 获取当前操作系统
 */
const getOS = () => {
  const env = Plugins.useEnvStore().env
  return env.os
}

/**
 * 添加一条路由
 */
const addRoute = async (route) => {
  const os = getOS()
  if (os === 'linux') {
    await Plugins.Exec('ip', ['route', 'add', ...route.split(/\s+/)])
  } else if (os === 'darwin') {
    // macOS: route -n add <dest> <gateway>
    const parts = route.split(/\s+/)
    const args = ['-n', 'add']
    // 将 "10.0.0.0/8 via 192.168.1.1" 转换为 "route -n add 10.0.0.0/8 192.168.1.1"
    for (let i = 0; i < parts.length; i++) {
      if (parts[i] === 'via') continue
      if (parts[i] === 'dev') { i++; continue } // macOS route 不支持 dev
      args.push(parts[i])
    }
    await Plugins.Exec('route', args)
  } else if (os === 'windows') {
    // Windows: route ADD <dest> MASK <mask> <gateway>
    const parts = route.split(/\s+/)
    const dest = parts[0]
    let gateway = ''
    for (let i = 1; i < parts.length; i++) {
      if (parts[i] === 'via') { gateway = parts[i + 1]; break }
    }
    if (!gateway) throw '无法解析网关地址'
    // 解析 CIDR 为网段和掩码
    const [network, prefix] = dest.split('/')
    const mask = prefix ? cidrToMask(parseInt(prefix)) : '255.255.255.255'
    await Plugins.Exec('route', ['ADD', network, 'MASK', mask, gateway], { Convert: true })
  }
}

/**
 * 删除一条路由
 */
const deleteRoute = async (route) => {
  const os = getOS()
  if (os === 'linux') {
    await Plugins.Exec('ip', ['route', 'del', ...route.split(/\s+/)])
  } else if (os === 'darwin') {
    const parts = route.split(/\s+/)
    const args = ['-n', 'delete']
    for (let i = 0; i < parts.length; i++) {
      if (parts[i] === 'via') continue
      if (parts[i] === 'dev') { i++; continue }
      args.push(parts[i])
    }
    await Plugins.Exec('route', args)
  } else if (os === 'windows') {
    const parts = route.split(/\s+/)
    const dest = parts[0]
    const [network] = dest.split('/')
    await Plugins.Exec('route', ['DELETE', network], { Convert: true })
  }
}

/**
 * CIDR 前缀转子网掩码 (仅 Windows 需要)
 */
const cidrToMask = (prefix) => {
  const mask = []
  for (let i = 0; i < 4; i++) {
    const bits = Math.min(prefix, 8)
    mask.push(256 - Math.pow(2, 8 - bits))
    prefix -= bits
  }
  return mask.join('.')
}

/**
 * 获取当前系统路由表
 */
const getSystemRoutes = async () => {
  const os = getOS()
  if (os === 'linux') {
    const out = await Plugins.Exec('ip', ['route', 'show'])
    return out.trim().split('\n').filter(Boolean)
  } else if (os === 'darwin') {
    const out = await Plugins.Exec('netstat', ['-rn'])
    return out.trim().split('\n').filter(Boolean)
  } else if (os === 'windows') {
    const out = await Plugins.Exec('route', ['PRINT'], { Convert: true })
    return out.trim().split('\n').filter(Boolean)
  }
  return []
}

/* ===================== 触发器 ===================== */

/**
 * 手动触发 - 管理静态路由
 */
const onRun = async () => {
  while (true) {
    const action = await Plugins.picker.single(
      '静态路由管理',
      [
        { label: '📋 查看当前系统路由表', value: 'list' },
        { label: '➕ 手动添加一条路由', value: 'add' },
        { label: '➖ 手动删除一条路由', value: 'remove' },
        { label: '🚀 立即应用配置路由', value: 'apply' },
        { label: '🧹 立即清除配置路由', value: 'clean' },
      ],
      []
    )

    if (!action) return 0

    try {
      if (action === 'list') {
        await showRouteTable()
      } else if (action === 'add') {
        await manualAddRoute()
      } else if (action === 'remove') {
        await manualRemoveRoute()
      } else if (action === 'apply') {
        await applyConfiguredRoutes()
        Plugins.message.success('配置路由已应用')
      } else if (action === 'clean') {
        await cleanConfiguredRoutes()
        Plugins.message.success('配置路由已清除')
      }
    } catch (err) {
      if (err === null || err === undefined) return 0
      Plugins.message.error('操作失败: ' + (err.message || err))
    }
  }
}

/**
 * 显示当前路由表
 */
const showRouteTable = async () => {
  const routes = await getSystemRoutes()
  if (routes.length === 0) {
    await Plugins.alert('当前路由表', '路由表为空')
    return
  }
  const content = routes.join('\n')
  await Plugins.alert('当前路由表', content)
}

/**
 * 手动添加路由
 */
const manualAddRoute = async () => {
  const route = await Plugins.prompt(
    '添加静态路由',
    '',
    { placeholder: '例如: 10.0.0.0/8 via 192.168.1.1 dev eth0' }
  )
  if (!route) return
  await addRoute(route)
  Plugins.message.success('路由已添加: ' + route)
}

/**
 * 手动删除路由 - 从当前路由表中选择
 */
const manualRemoveRoute = async () => {
  const os = getOS()
  let routes = []

  if (os === 'linux') {
    const out = await Plugins.Exec('ip', ['route', 'show'])
    routes = out.trim().split('\n').filter(Boolean)
  } else {
    routes = await getSystemRoutes()
  }

  if (routes.length === 0) {
    Plugins.message.info('路由表为空')
    return
  }

  const selected = await Plugins.picker.single(
    '选择要删除的路由',
    routes.map((r) => ({ label: r, value: r })),
    []
  )

  if (!selected) return

  if (os === 'linux') {
    await Plugins.Exec('ip', ['route', 'del', ...selected.split(/\s+/)])
  } else {
    await deleteRoute(selected)
  }

  Plugins.message.success('路由已删除')
}

/**
 * 批量应用配置中的路由（跳过失败条目）
 */
const applyConfiguredRoutes = async () => {
  const routes = parseRoutes()
  const errors = []
  for (const route of routes) {
    try {
      await addRoute(route)
    } catch (err) {
      errors.push({ route, error: err.message || String(err) })
    }
  }
  if (errors.length > 0) {
    console.log('[静态路由] 部分路由添加失败:', JSON.stringify(errors))
  }
  return errors
}

/**
 * 批量清除配置中的路由（跳过失败条目）
 */
const cleanConfiguredRoutes = async () => {
  const routes = parseRoutes()
  const errors = []
  for (const route of routes) {
    try {
      await deleteRoute(route)
    } catch (err) {
      errors.push({ route, error: err.message || String(err) })
    }
  }
  if (errors.length > 0) {
    console.log('[静态路由] 部分路由删除失败:', JSON.stringify(errors))
  }
  return errors
}

/**
 * 核心启动后 - 自动添加静态路由
 */
const onCoreStarted = async () => {
  const routes = parseRoutes()
  if (routes.length === 0) return 0

  const errors = await applyConfiguredRoutes()
  if (errors.length > 0) {
    console.log(`[静态路由] 核心启动后路由配置完成，${errors.length} 条失败已跳过`)
  }
  return 0
}

/**
 * 核心停止后 - 自动删除静态路由
 */
const onCoreStopped = async () => {
  const routes = parseRoutes()
  if (routes.length === 0) return 0

  const errors = await cleanConfiguredRoutes()
  if (errors.length > 0) {
    console.log(`[静态路由] 核心停止后路由清理完成，${errors.length} 条失败已跳过`)
  }
  return 0
}
