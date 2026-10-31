// WEBUI.for.SingBox router script plugin. GPL-3.0-only.
// Independently adapted for the backend plugin API; no desktop/Wails access.
function stable(value) {
  if (Array.isArray(value)) return "[" + value.map(stable).join(",") + "]";
  if (value && typeof value === "object") {
    return "{" + Object.keys(value).sort().map(function (key) { return JSON.stringify(key) + ":" + stable(value[key]); }).join(",") + "}";
  }
  return JSON.stringify(value);
}
function onSubscribe(proxies) {
  const seen = new Map(), aliases = new Map(), result = [];
  proxies.forEach(function (proxy) {
    const config = Object.assign({}, proxy);
    delete config.tag;
    delete config.__id_in_gui;
    const key = stable(config);
    if (seen.has(key)) aliases.set(proxy.tag, seen.get(key));
    else { seen.set(key, proxy.tag); result.push(Object.assign({}, proxy)); }
  });
  result.forEach(function (proxy) {
    ["detour", "default"].forEach(function (key) {
      if (typeof proxy[key] === "string" && aliases.has(proxy[key])) proxy[key] = aliases.get(proxy[key]);
    });
    if (Array.isArray(proxy.outbounds)) proxy.outbounds = Array.from(new Set(proxy.outbounds.map(function (tag) { return aliases.get(tag) || tag; })));
  });
  console.log("Removed " + (proxies.length - result.length) + " identical nodes.");
  return result;
}
