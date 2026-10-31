// SPDX-License-Identifier: GPL-3.0-only
// Router adaptation of GUI-for-Cores/Plugin-Hub Generic/plugin-adguardhome.js
// Upstream: 205ecacb0742a185947252cb91e5b11541c32a94. Existing service integration only; procd/systemd owns the daemon.
function onGenerate(config) {
  if (configuration.Enabled !== true) return config;
  const host = String(configuration.Address || '127.0.0.1').trim();
  const canonicalHost = canonicalIP(host);
  if (!canonicalHost || ['0.0.0.0', '0:0:0:0:0:0:0:0'].includes(canonicalHost)) throw new Error('AdGuard DNS address must be an explicit IPv4 or IPv6 address');
  const port = Number(configuration.Port || '20053');
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Invalid AdGuard DNS port');
  const protocol = configuration.Protocol || 'udp';
  if (!['udp', 'tcp'].includes(protocol)) throw new Error('AdGuard protocol must be udp or tcp');
  if ((config.inbounds || []).some(function (inbound) {
    return Number(inbound.listen_port) === port && (!inbound.listen || ['0.0.0.0', '0:0:0:0:0:0:0:0', canonicalHost].includes(canonicalIP(inbound.listen)));
  })) throw new Error('AdGuard DNS endpoint conflicts with a sing-box listener and may loop');
  const mode = configuration.Mode || 'domains';
  if (!['domains', 'final'].includes(mode)) throw new Error('Unknown AdGuard DNS mode');
  const domains = String(configuration.Domains || '').split(/[\s,]+/).filter(Boolean);
  if (mode === 'domains' && (!domains.length || domains.length > 128)) throw new Error('Configure 1–128 domain suffixes before enabling AdGuard DNS');
  if (domains.some(function (value) { return value.length > 253 || !value.split('.').every(function (label) { return /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(label); }); })) throw new Error('Invalid AdGuard domain suffix');
  const dns = config.dns || (config.dns = {});
  const servers = dns.servers || (dns.servers = []);
  const server = { type: protocol, tag: 'router-adguard', server: host, server_port: port };
  const previous = servers.find(function (item) { return item.tag === server.tag; });
  if (previous && canonical(previous) !== canonical(server)) throw new Error('DNS tag router-adguard is already in use');
  if (!previous) servers.push(server);
  if (mode === 'final') dns.final = server.tag;
  else {
    const rule = { domain_suffix: Array.from(new Set(domains)).sort(), action: 'route', server: server.tag };
    const rules = dns.rules || (dns.rules = []);
    if (!rules.some(function (item) { return canonical(item) === canonical(rule); })) rules.unshift(rule);
  }
  return config;
}

function canonical(value) {
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.keys(value).sort().map(function (key) { return JSON.stringify(key) + ':' + canonical(value[key]); }).join(',') + '}';
  return JSON.stringify(value);
}
function validIP(value) {
  if (/^\d+\.\d+\.\d+\.\d+$/.test(value)) return value.split('.').every(function (part) { return /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255; });
  if (!/^[0-9a-f:]+$/i.test(value) || !value.includes(':') || value.includes(':::')) return false;
  if ((value.startsWith(':') && !value.startsWith('::')) || (value.endsWith(':') && !value.endsWith('::'))) return false;
  const split = value.split('::');
  if (split.length > 2) return false;
  const groups = value.split(':').filter(Boolean);
  return groups.every(function (part) { return part.length <= 4; }) && (split.length === 2 ? groups.length < 8 : groups.length === 8);
}
function canonicalIP(value) {
  if (typeof value !== 'string' || !validIP(value)) return '';
  if (!value.includes(':')) return value;
  const halves = value.toLowerCase().split('::');
  const left = halves[0] ? halves[0].split(':') : [];
  const right = halves.length === 2 && halves[1] ? halves[1].split(':') : [];
  const expanded = halves.length === 2 ? left.concat(Array(8 - left.length - right.length).fill('0'), right) : left;
  return expanded.map(function (part) { return parseInt(part, 16).toString(16); }).join(':');
}
