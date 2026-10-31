// SPDX-License-Identifier: GPL-3.0-only
// Router adaptation of GUI-for-Cores/Plugin-Hub Generic/plugin-geoip-search.js.
// Upstream: 205ecacb0742a185947252cb91e5b11541c32a94. Queries are manual and disclose only the explicitly entered IP.
function onRun() {
  const ip = String(configuration.IP || '').trim();
  if (ip && !/^[0-9a-f:.]+$/i.test(ip)) throw new Error('Enter an IP address, not a URL or hostname');
  const response = Plugins.fetch('https://ipwho.is/' + encodeURIComponent(ip), { route: configuration.Route || 'configured' });
  if (response.status !== 200 || response.truncated) throw new Error('IP lookup returned an incomplete or unsuccessful response');
  const data = JSON.parse(response.body);
  if (data.success !== true || typeof data.ip !== 'string') throw new Error('IP lookup provider did not return a successful result');
  return {
    provider: 'ipwho.is', route: response.route, ip: data.ip, type: data.type,
    country: data.country, country_code: data.country_code, region: data.region, city: data.city,
    asn: data.connection && data.connection.asn, isp: data.connection && data.connection.isp,
    note: 'Third-party geolocation is approximate. It is not a streaming-unlock or fraud-risk verdict.',
  };
}
