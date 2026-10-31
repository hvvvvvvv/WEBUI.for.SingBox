// WEBUI.for.SingBox router script plugin. GPL-3.0-or-later.
// Independently adapted for the backend plugin API; no desktop/Wails access.
function renameNodes(proxies, transform, ensureUnique) {
  const names = new Map(), used = new Set();
  const result = proxies.map(function (proxy, index) {
    const original = String(proxy.tag);
    const base = String(transform(original, index) || original).trim() || original;
    let tag = base, suffix = 2;
    while (ensureUnique !== false && used.has(tag)) tag = base + " [" + suffix++ + "]";
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

const regions = [
  [/(?:Hong\s*Kong|香港|\bHK(?=\d|\b))/i, "🇭🇰", "香港", "Hong Kong"],
  [/(?:Taiwan|台湾|台灣|\bTW(?=\d|\b))/i, "🇹🇼", "台湾", "Taiwan"],
  [/(?:Japan|日本|东京|東京|\bJP(?=\d|\b))/i, "🇯🇵", "日本", "Japan"],
  [/(?:Singapore|新加坡|狮城|\bSG(?=\d|\b))/i, "🇸🇬", "新加坡", "Singapore"],
  [/(?:United States|美国|美國|\bUSA?\b)/i, "🇺🇸", "美国", "United States"],
  [/(?:United Kingdom|英国|英國|\bUK\b)/i, "🇬🇧", "英国", "United Kingdom"],
  [/(?:Germany|德国|德國|\bDE\b)/i, "🇩🇪", "德国", "Germany"],
  [/(?:Korea|韩国|韓國|\bKR\b)/i, "🇰🇷", "韩国", "Korea"],
  [/(?:Canada|加拿大|\bCA\b)/i, "🇨🇦", "加拿大", "Canada"],
  [/(?:Australia|澳大利亚|澳大利亞|\bAU\b)/i, "🇦🇺", "澳大利亚", "Australia"]
];
function regionFor(name) { return regions.find(function (r) { return r[0].test(name); }); }
function withFlag(name, region) {
  if (!region || /[\uD83C][\uDDE6-\uDDFF]/.test(name)) return name;
  return region[1] + " " + name;
}

function onSubscribe(proxies) {
  const remove = configuration.EnableRemoveKeywords && configuration.RemoveKeywords
    ? new RegExp(String(configuration.RemoveKeywords), "g") : null;
  return renameNodes(proxies, function (name, index) {
    const region = regionFor(name);
    if (remove) name = name.replace(remove, "").trim();
    if (configuration.EnableAddEmoji !== false) name = withFlag(name, region);
    if (String(configuration.EnableIndexProxyName) === "1") name += " " + (index + 1);
    return name;
  }, String(configuration.EnableIndexProxyName ?? "0") !== "0");
}
