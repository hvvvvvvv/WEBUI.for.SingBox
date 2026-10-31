// SPDX-License-Identifier: GPL-3.0-only
// Router adaptation of GUI-for-Cores/Plugin-Hub Generic/plugin-network-info.js and plugin-ip-geolocation.js.
// Upstream: 205ecacb0742a185947252cb91e5b11541c32a94. Bounded backend requests; never changes the selected node.
function onRun() {
  const route = configuration.Route || 'configured';
  const report = { route: route, note: 'Measured from the router backend; this does not prove every LAN client follows the same route.', exit: null, sites: [] };
  try {
    const response = Plugins.fetch('https://www.cloudflare.com/cdn-cgi/trace', { route: route });
    if (response.status !== 200 || response.truncated) throw new Error('Trace returned an incomplete or unsuccessful response');
    const trace = {};
    response.body.split(/\r?\n/).forEach(function (line) {
      const separator = line.indexOf('=');
      const key = line.slice(0, separator);
      if (['ip', 'loc', 'colo', 'warp', 'tls', 'http'].includes(key)) trace[key] = line.slice(separator + 1);
    });
    if (!trace.ip) throw new Error('Trace did not contain an exit IP');
    report.exit = trace;
  } catch (error) { report.exit = { error: String(error.message || error) }; }
  const sites = JSON.parse(configuration.Sites || '[]');
  if (!Array.isArray(sites) || sites.length > 6) throw new Error('Sites must be a JSON array of at most six {name,url} entries');
  sites.forEach(function (site) {
    if (!site || typeof site.name !== 'string' || typeof site.url !== 'string') throw new Error('Each site needs name and url strings');
    const item = { name: site.name };
    try {
      const response = Plugins.fetch(site.url, { method: 'HEAD', route: route });
      item.status = response.status;
      item.elapsed_ms = response.elapsed_ms;
      item.reachable = response.status >= 200 && response.status < 400;
    } catch (error) { item.error = String(error.message || error); }
    report.sites.push(item);
  });
  return report;
}
