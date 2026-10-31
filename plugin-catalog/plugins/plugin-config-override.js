// Router adaptation of GUI.for.Cores Plugin-Hub. GPL-3.0.
// Upstream commit: 205ecacb0742a185947252cb91e5b11541c32a94
// Source: https://github.com/GUI-for-Cores/Plugin-Hub/blob/205ecacb0742a185947252cb91e5b11541c32a94/plugins/GFS/plugin-config-override.js
// Pure onGenerate: no filesystem, network, process, GUI or system-rule access.

function fail(message) { throw new Error(message) }
function object(value) { return value !== null && typeof value === 'object' && !Array.isArray(value) }
function copy(value) { return JSON.parse(JSON.stringify(value)) }
function text(value, label) {
  if (typeof value !== 'string' || !value.trim() || value.length > 256 || /[\x00-\x1f\x7f]/.test(value)) fail(label + ' must be a non-empty string')
  return value
}
function jsonSetting(key, fallback) {
  const value = configuration[key]
  if (value === undefined || value === '') return copy(fallback)
  if (typeof value !== 'string' || value.length > 16384) fail(key + ' must be JSON text (maximum 16384 characters)')
  try { return JSON.parse(value) } catch (_) { fail(key + ' contains invalid JSON') }
}
function list(value, label, maximum) {
  if (!Array.isArray(value) || value.length > (maximum || 256)) fail(label + ' must be a bounded array')
  return value
}
function fields(value, allowed, label) {
  if (!object(value)) fail(label + ' must be an object')
  Object.keys(value).forEach(function (key) { if (allowed.indexOf(key) < 0) fail(label + ': unknown field ' + key) })
}
function bool(value, fallback, label) {
  if (value === undefined) return fallback
  if (typeof value !== 'boolean') fail(label + ' must be boolean')
  return value
}
function port(value) {
  if (!Number.isInteger(value) || value < 1 || value > 65535) fail('listen_port must be an integer from 1 to 65535')
  return value
}
function ipv4(value) {
  const parts = value.split('.')
  return parts.length === 4 && parts.every(function (part) { return /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255 })
}
function ipVersion(value) {
  if (typeof value !== 'string') return 0
  if (ipv4(value)) return 4
  if (!value.includes(':') || /[^0-9a-fA-F:.]/.test(value)) return 0
  let ip = value
  if (ip.includes('.')) {
    const index = ip.lastIndexOf(':')
    if (!ipv4(ip.slice(index + 1))) return 0
    ip = ip.slice(0, index) + ':0:0'
  }
  if (ip.indexOf('::') !== ip.lastIndexOf('::')) return 0
  if ((ip.startsWith(':') && !ip.startsWith('::')) || (ip.endsWith(':') && !ip.endsWith('::'))) return 0
  const compressed = ip.includes('::')
  const groups = ip.split(':').filter(Boolean)
  if (!groups.every(function (group) { return /^[a-fA-F0-9]{1,4}$/.test(group) })) return 0
  if (compressed) return groups.length < 8 ? 6 : 0
  return groups.length === 8 && !ip.startsWith(':') && !ip.endsWith(':') ? 6 : 0
}
function cidr(value) {
  if (typeof value !== 'string') fail('CIDR must be a string')
  const parts = value.split('/')
  const family = ipVersion(parts[0])
  if (parts.length !== 2 || !family || !/^(0|[1-9]\d*)$/.test(parts[1]) || Number(parts[1]) > (family === 4 ? 32 : 128)) fail('Invalid CIDR: ' + value)
  return value
}
function stable(value) {
  if (Array.isArray(value)) return '[' + value.map(stable).join(',') + ']'
  if (object(value)) return '{' + Object.keys(value).sort().map(function (key) { return JSON.stringify(key) + ':' + stable(value[key]) }).join(',') + '}'
  return JSON.stringify(value)
}
function outboundMap(config) {
  const map = new Map()
  list(config.outbounds || [], 'outbounds', 20000).concat(list(config.endpoints || [], 'endpoints', 256)).forEach(function (item) {
    if (!object(item)) fail('Invalid outbound or endpoint')
    const tag = text(item.tag, 'outbound tag')
    if (map.has(tag)) fail('Duplicate outbound tag: ' + tag)
    map.set(tag, item)
  })
  return map
}
function target(map, tag) {
  text(tag, 'outbound target')
  if (!map.has(tag)) fail('Unknown outbound target: ' + tag)
}
function graphCheck(map) {
  const state = new Map()
  function visit(tag, path) {
    if (state.get(tag) === 1) fail('Outbound cycle: ' + path.concat(tag).join(' -> '))
    if (state.get(tag) === 2) return
    if (path.length > 128) fail('Outbound dependency graph is too deep')
    state.set(tag, 1)
    const item = map.get(tag)
    let edges = []
    if (item.type === 'selector' || item.type === 'urltest') edges = list(item.outbounds, tag + '.outbounds', 20000)
    if (item.detour) edges = edges.concat(item.detour)
    edges.forEach(function (next) { target(map, next); visit(next, path.concat(tag)) })
    state.set(tag, 2)
  }
  map.forEach(function (_, tag) { visit(tag, []) })
}
function endpointPort(value) {
  if (typeof value !== 'string') return 0
  const match = /:(\d+)$/.exec(value)
  return match ? Number(match[1]) : 0
}
function usedPorts(config) {
  const used = new Set([20123]) // The backend injects its loopback native API after plugins.
  function walk(value, depth) {
    if (depth > 64) fail('Configuration is too deep')
    if (!value || typeof value !== 'object') return
    Object.keys(value).forEach(function (key) {
      if (key === 'listen_port' && Number(value[key]) > 0) used.add(Number(value[key]))
      else if (key === 'external_controller') { const p = endpointPort(value[key]); if (p) used.add(p) }
      else walk(value[key], depth + 1)
    })
  }
  walk(config, 0)
  return used
}
function inboundTags(config) {
  const tags = new Set()
  ;['inbounds', 'endpoints', 'services'].forEach(function (key) {
    list(config[key] || [], key, 20000).forEach(function (item) {
      if (item.tag) { if (tags.has(item.tag)) fail('Duplicate inbound/service tag: ' + item.tag); tags.add(item.tag) }
    })
  })
  return tags
}
function validateTargets(config) {
  const map = outboundMap(config)
  graphCheck(map)
  if (config.route && config.route.final) target(map, config.route.final)
  function rules(items, depth) {
    if (depth > 64) fail('Route rules are too deep')
    list(items || [], 'route rules', 20000).forEach(function (rule) {
      if (!object(rule)) fail('Invalid route rule')
      if (rule.outbound) target(map, rule.outbound)
      if (rule.rules) rules(rule.rules, depth + 1)
    })
  }
  rules(config.route && config.route.rules, 0)
  return map
}

function onGenerate(config) {
  if (!bool(configuration.Enabled, false, 'Enabled')) return config
  const patch = jsonSetting('Override', {})
  if (!object(patch)) fail('Override must be a JSON object')
  const paths = list(jsonSetting('AppendPaths', []), 'AppendPaths', 64)
  const append = new Set(), seen = new Set()
  paths.forEach(function (path) {
    if (typeof path !== 'string' || !/^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z0-9_]+)*$/.test(path)) fail('Invalid append path')
    if (append.has(path)) fail('Duplicate append path: ' + path)
    append.add(path)
  })
  function safe(value, depth) {
    if (depth > 64) fail('Override is too deep')
    if (!value || typeof value !== 'object') return
    Object.keys(value).forEach(function (key) {
      if (['__proto__', 'prototype', 'constructor'].includes(key)) fail('Unsafe object key: ' + key)
      safe(value[key], depth + 1)
    })
  }
  safe(patch, 0)
  function merge(original, extra, path) {
    if (Array.isArray(extra)) {
      if (append.has(path)) {
        if (original !== undefined && !Array.isArray(original)) fail('Append target is not an array: ' + path)
        seen.add(path)
        return (original || []).concat(copy(extra))
      }
      return copy(extra)
    }
    if (!object(extra)) return extra
    const result = object(original) ? copy(original) : {}
    Object.keys(extra).forEach(function (key) { result[key] = merge(result[key], extra[key], path ? path + '.' + key : key) })
    return result
  }
  const result = merge(config, patch, '')
  append.forEach(function (path) { if (!seen.has(path)) fail('Append path did not match an override array: ' + path) })
  ;['inbounds', 'endpoints'].forEach(function (key) {
    const current = list(result[key] || [], key, 20000)
    ;(config[key] || []).forEach(function (old) {
      if (!current.some(function (item) { return stable(item) === stable(old) })) fail('Existing ' + key + ' cannot be removed or overwritten: ' + (old.tag || old.type))
    })
  })
  const oldAPI = (config.services || []).filter(function (item) { return item.type === 'api' })
  const newAPI = list(result.services || [], 'services', 256).filter(function (item) { return item.type === 'api' })
  if (stable(oldAPI) !== stable(newAPI) || stable(config.native_api) !== stable(result.native_api)) fail('Managed native API cannot be changed')
  if (stable(config.experimental && config.experimental.clash_api) !== stable(result.experimental && result.experimental.clash_api)) fail('Do not change or add the legacy Clash API')
  inboundTags(result)
  const occupied = usedPorts(config)
  ;['inbounds', 'endpoints', 'services'].forEach(function (key) {
    ;(result[key] || []).forEach(function (item) {
      if ((config[key] || []).some(function (old) { return stable(old) === stable(item) })) return
      if (item.listen_port !== undefined) {
        const p = port(item.listen_port)
        if (occupied.has(p)) fail('Listening port already in use by configuration: ' + p)
        occupied.add(p)
      }
    })
  })
  validateTargets(result)
  return result
}
