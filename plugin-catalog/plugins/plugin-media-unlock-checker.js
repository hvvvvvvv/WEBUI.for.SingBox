// SPDX-License-Identifier: GPL-3.0-only
// Router adaptation of GUI-for-Cores/Plugin-Hub Generic/plugin-media-unlock-checker.js.
// Upstream: 205ecacb0742a185947252cb91e5b11541c32a94. GET-only observations, no account/device-token registration.
function onRun() {
  const route = configuration.Route || 'core';
  const targets = [
    { name: 'Netflix licensed title', url: 'https://www.netflix.com/title/70143836', detect: function (r) { return r.status === 404 ? 'title unavailable from this exit' : 'unknown; webpage access does not prove playback'; } },
    { name: 'YouTube Premium', url: 'https://www.youtube.com/premium?hl=en', detect: function (r) {
      if (/Premium is not available in your country/i.test(r.body)) return 'Premium reports unavailable in this country';
      return 'unknown; login, account and playback restrictions were not tested';
    } },
    { name: 'Spotify country service', url: 'https://www.spotify.com/api/content/v1/country-selector?platform=web&format=json', detect: function () { return 'country service reachable; account and playback restrictions were not tested'; } },
  ];
  return { route: route, note: 'Read-only reachability and explicit region-denial observations. HTTP 200 is never treated as proof of full media unlock. Disney device registration is excluded.', services: targets.map(function (target) {
    const item = { name: target.name };
    try {
      const response = Plugins.fetch(target.url, { route: route });
      item.status = response.status;
      item.elapsed_ms = response.elapsed_ms;
      item.truncated = response.truncated;
      item.observation = response.truncated ? 'unknown; response exceeds diagnostic limit' : (response.status >= 200 && response.status < 300) || response.status === 404 ? target.detect(response) : 'unknown; HTTP ' + response.status;
    } catch (error) { item.observation = 'unknown'; item.error = String(error.message || error); }
    return item;
  }) };
}
