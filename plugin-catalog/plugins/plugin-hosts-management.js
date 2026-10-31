// SPDX-License-Identifier: GPL-3.0-only
// Router adaptation of GUI-for-Cores/Plugin-Hub Generic/plugin-hosts-management.js
// Upstream: 205ecacb0742a185947252cb91e5b11541c32a94. Changes: DNS-scoped hosts; never writes /etc/hosts.
function validIP(value) {
  if (/^\d+\.\d+\.\d+\.\d+$/.test(value)) return value.split('.').every(function (part) { return /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255; });
  if (!/^[0-9a-f:]+$/i.test(value) || !value.includes(':') || value.includes(':::')) return false;
  if ((value.startsWith(':') && !value.startsWith('::')) || (value.endsWith(':') && !value.endsWith('::'))) return false;
  const split = value.split('::');
  if (split.length > 2) return false;
  const groups = value.split(':').filter(Boolean);
  return groups.every(function (part) { return part.length <= 4; }) && (split.length === 2 ? groups.length < 8 : groups.length === 8);
}
function validDomain(value) {
  return value.length <= 253 && value.split('.').every(function (label) { return /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(label); }) && !['__proto__', 'constructor', 'prototype'].includes(value);
}
function onGenerate(config) {
  if (configuration.Enabled !== true) return config;
  const text = String(configuration.Hosts || '');
  const predefined = Object.create(null);
  text.split(/\r?\n/).forEach(function (line) {
    const tokens = line.split('#')[0].trim().split(/\s+/);
    if (!tokens[0]) return;
    const ip = tokens.shift();
    if (!validIP(ip) || !tokens.length) throw new Error('Hosts must contain a valid IP followed by domain names');
    tokens.forEach(function (token) {
      const name = token.toLowerCase().replace(/\.$/, '');
      if (!validDomain(name)) throw new Error('Invalid hosts domain: ' + name);
      const addresses = predefined[name] || (predefined[name] = []);
      if (!addresses.includes(ip)) addresses.push(ip);
    });
  });
  const domains = Object.keys(predefined).sort();
  if (!domains.length || domains.length > 256) throw new Error('Configure 1–256 hosts domains');
  const server = { type: 'hosts', tag: 'router-hosts', predefined: predefined };
  const dns = config.dns || (config.dns = {});
  const servers = dns.servers || (dns.servers = []);
  const existing = servers.find(function (item) { return item.tag === server.tag; });
  if (existing && canonical(existing) !== canonical(server)) throw new Error('DNS tag router-hosts is already in use');
  if (!existing) servers.push(server);
  const rule = { domain: domains, action: 'route', server: server.tag };
  const rules = dns.rules || (dns.rules = []);
  if (!rules.some(function (item) { return canonical(item) === canonical(rule); })) rules.unshift(rule);
  return config;
}

function canonical(value) {
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.keys(value).sort().map(function (key) { return JSON.stringify(key) + ':' + canonical(value[key]); }).join(',') + '}';
  return JSON.stringify(value);
}
