// WEBUI.for.SingBox-Plugins. GPL-3.0-only.
// Router alternative inspired by Plugin-Hub's smart-proxy-switcher / auto-failover.
// This generates sing-box URLTest configuration; it does NOT reproduce their
// EWMA scoring, log failure thresholds, ordered failover or notification history.
// https://sing-box.sagernet.org/configuration/outbound/urltest/

function urltestText(key, fallback) {
  const value = configuration[key] === undefined ? fallback : configuration[key];
  if (typeof value !== 'string') throw new Error(key + ' must be text');
  return value.trim();
}

function urltestDuration(key, fallback, min) {
  const value = urltestText(key, fallback);
  const match = /^(\d+)(s|m|h)$/.exec(value);
  const seconds = match ? Number(match[1]) * ({s:1,m:60,h:3600})[match[2]] : NaN;
  if (!Number.isSafeInteger(seconds) || seconds < min || seconds > 86400) throw new Error(key + ' must be ' + min + '..86400 seconds, using s/m/h');
  return { value: value, seconds: seconds };
}

function urltestBoolean(key, fallback) {
  const value = configuration[key] === undefined ? fallback : configuration[key];
  if (typeof value !== 'boolean') throw new Error(key + ' must be boolean');
  return value;
}

function urltestCanonical(value) {
  if (Array.isArray(value)) return '[' + value.map(urltestCanonical).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.keys(value).sort().map(function (key) { return JSON.stringify(key) + ':' + urltestCanonical(value[key]); }).join(',') + '}';
  return JSON.stringify(value);
}

function onGenerate(config) {
  if (!urltestBoolean('Enabled', false)) return config;
  if (!Array.isArray(config.outbounds)) throw new Error('Configure proxy outbounds first');
  const tag = urltestText('Tag', 'router-auto');
  if (!tag || tag.length > 128 || /[\x00-\x1f\x7f]/.test(tag)) throw new Error('Tag must be 1..128 characters without control characters');
  const raw = configuration.Outbounds === undefined ? '' : configuration.Outbounds;
  if (typeof raw !== 'string' && !Array.isArray(raw)) throw new Error('Outbounds must be lines of tags or a JSON string array');
  const tags = (typeof raw === 'string' ? raw.split(/\r?\n/) : raw).map(function (value) {
    if (typeof value !== 'string') throw new Error('Each outbound tag must be text');
    return value.trim();
  }).filter(Boolean);
  if (!tags.length || tags.length > 256) throw new Error('Configure 1..256 proxy outbound tags');
  if (new Set(tags).size !== tags.length) throw new Error('Duplicate outbound tags');
  const byTag = new Map();
  config.outbounds.forEach(function (outbound) {
    if (!outbound || typeof outbound.tag !== 'string' || byTag.has(outbound.tag)) throw new Error('Existing outbounds must have unique tags');
    byTag.set(outbound.tag, outbound);
  });
  const endpoints = config.endpoints === undefined ? [] : config.endpoints;
  if (!Array.isArray(endpoints)) throw new Error('endpoints must be an array');
  const endpointTags = new Set();
  endpoints.forEach(function (endpoint) {
    if (!endpoint || typeof endpoint.tag !== 'string' || !endpoint.tag || byTag.has(endpoint.tag)) throw new Error('Outbounds and endpoints must have unique tags');
    endpointTags.add(endpoint.tag);
    byTag.set(endpoint.tag, endpoint);
  });
  tags.forEach(function (name) {
    const outbound = byTag.get(name);
    if (name === tag) throw new Error('URLTest cannot reference itself');
    if (!outbound) throw new Error('Unknown outbound: ' + name);
    if (endpointTags.has(name) || !outbound.type || ['selector','urltest','direct','block','dns'].includes(outbound.type)) throw new Error('Choose proxy nodes, not endpoint/policy/direct/block groups: ' + name);
  });
  const interval = urltestDuration('Interval', '3m', 30);
  const idle = urltestDuration('IdleTimeout', '30m', interval.seconds);
  const toleranceRaw = configuration.Tolerance === undefined ? '50' : configuration.Tolerance;
  if ((typeof toleranceRaw !== 'string' && typeof toleranceRaw !== 'number') || String(toleranceRaw).trim() === '') throw new Error('Tolerance must be an integer');
  const tolerance = Number(toleranceRaw);
  if (!Number.isInteger(tolerance) || tolerance < 0 || tolerance > 10000) throw new Error('Tolerance must be 0..10000 milliseconds');
  const url = urltestText('TestURL', 'https://www.gstatic.com/generate_204');
  if (url.length > 2048 || !/^https?:\/\/[^\s/?#@]+(?:\/[^\s#]*)?$/.test(url)) throw new Error('TestURL must be an HTTP(S) URL without credentials or fragment');
  const group = {
    type: 'urltest', tag: tag, outbounds: tags, url: url,
    interval: interval.value, tolerance: tolerance, idle_timeout: idle.value,
    interrupt_exist_connections: urltestBoolean('InterruptExisting', false)
  };
  const existing = byTag.get(tag);
  if (existing && (endpointTags.has(tag) || urltestCanonical(existing) !== urltestCanonical(group))) throw new Error('Tag already in use; choose an unused URLTest tag: ' + tag);
  const selectorTag = urltestText('SelectorTag', '');
  const makeDefault = urltestBoolean('MakeDefault', false);
  if (makeDefault && !selectorTag) throw new Error('MakeDefault requires SelectorTag');
  let selector;
  if (selectorTag) {
    selector = byTag.get(selectorTag);
    if (!selector || selector.type !== 'selector' || !Array.isArray(selector.outbounds)) throw new Error('SelectorTag must identify an existing selector');
    selector = Object.assign({}, selector, {outbounds: selector.outbounds.includes(tag) ? selector.outbounds.slice() : selector.outbounds.concat([tag])});
    if (makeDefault) selector.default = tag;
  }
  // Validate the proposed graph before mutating config, including proxy detours.
  const graph = new Map(byTag);
  graph.set(tag, group);
  if (selector) graph.set(selectorTag, selector);
  const visited = new Set(), active = new Set();
  function visit(name) {
    if (active.has(name)) throw new Error('Outbound dependency cycle at ' + name);
    if (!graph.has(name)) throw new Error('Unknown outbound dependency: ' + name);
    if (visited.has(name)) return;
    if (active.size >= 128) throw new Error('Outbound dependency graph is too deep');
    active.add(name);
    const outbound = graph.get(name);
    const references = Array.isArray(outbound.outbounds) ? outbound.outbounds.slice() : [];
    if (typeof outbound.detour === 'string') references.push(outbound.detour);
    references.forEach(visit);
    active.delete(name); visited.add(name);
  }
  graph.forEach(function (_, name) { visit(name); });
  config.outbounds = config.outbounds.map(function (outbound) { return selector && outbound.tag === selectorTag ? selector : outbound; });
  if (!existing) config.outbounds.push(group);
  console.log('URLTest generated: ' + tags.length + ' nodes. Choose this group in routing; save and manually restart the core to apply.');
  return config;
}
