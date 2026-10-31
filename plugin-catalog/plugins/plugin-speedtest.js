// SPDX-License-Identifier: GPL-3.0-only
// Router adaptation of GUI-for-Cores/Plugin-Hub Generic/plugin-speedtest.js.
// Related upstream batch-download-speed/speedtest-cli are replaced by a bounded current-exit sample.
// Upstream: 205ecacb0742a185947252cb91e5b11541c32a94. No binary download, license acceptance, selector switching or persistent timers.
function onRun() {
  const bytes = Number(configuration.Bytes || '1048576');
  if (!Number.isInteger(bytes) || bytes < 65536 || bytes > 8388608) throw new Error('Sample bytes must be 65536–8388608');
  const route = configuration.Route || 'core';
  const url = String(configuration.URL || 'https://speed.cloudflare.com/__down?bytes=' + bytes);
  const result = Plugins.measureDownload(url, { route: route, max_bytes: bytes });
  return {
    route: result.route,
    bytes: result.bytes,
    elapsed_ms: result.elapsed_ms,
    megabits_per_second: Number((result.bytes_per_second * 8 / 1000000).toFixed(3)),
    incomplete: result.incomplete,
    truncated: result.truncated,
    note: 'Short, bounded HTTP sample of the current router exit, including request setup. Not a line-rate benchmark or a per-node ranking. No global node was switched.',
  };
}
