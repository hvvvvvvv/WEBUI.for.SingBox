// WEBUI.for.SingBox router script plugin. GPL-3.0-or-later.
// Independently adapted for the backend plugin API; no desktop/Wails access.
function renameNodes(proxies, transform) {
  const names = new Map(), used = new Set();
  const result = proxies.map(function (proxy, index) {
    const original = String(proxy.tag);
    const base = String(transform(original, index) || original).trim() || original;
    let tag = base, suffix = 2;
    while (used.has(tag)) tag = base + " [" + suffix++ + "]";
    used.add(tag);
    if (!names.has(original)) names.set(original, tag);
    return Object.assign({}, proxy, { tag: tag });
  });
  return result.map(function (proxy) {
    ["detour", "default"].forEach(function (key) {
      if (typeof proxy[key] === "string" && names.has(proxy[key])) proxy[key] = names.get(proxy[key]);
    });
    if (Array.isArray(proxy.outbounds)) proxy.outbounds = proxy.outbounds.map(function (tag) { return names.get(tag) || tag; });
    return proxy;
  });
}

function onSubscribe(proxies) { return renameNodes(proxies, function (name) { return name; }); }
