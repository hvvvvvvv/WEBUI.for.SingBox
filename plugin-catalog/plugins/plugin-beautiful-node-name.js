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



const cities = [
  ['香港', /铜锣湾|銅鑼灣|九龙|九龍|新界|Kowloon/i],
  ['台湾', /台北|臺北|台中|臺中|新北|彰化|Taipei/i],
  ['日本', /东京|東京|大阪|名古屋|埼玉|Tokyo|Osaka|Nagoya/i],
  ['新加坡', /滨海|濱海|NorthWest/i],
  ['美国', /洛杉矶|洛杉磯|西雅图|西雅圖|纽约|紐約|硅谷|旧金山|舊金山|圣何塞|聖何塞|芝加哥|波士顿|Virginia|California|Seattle|Los Angeles|New York|San Jose/i],
  ['英国', /伦敦|倫敦|London/i], ['德国', /法兰克福|法蘭克福|Frankfurt/i],
  ['韩国', /首尔|首爾|釜山|Seoul/i], ['加拿大', /多伦多|多倫多|温哥华|溫哥華|蒙特利尔|Toronto|Vancouver|Waterloo/i],
  ['澳大利亚', /墨尔本|墨爾本|悉尼|Sydney|Melbourne/i]
];
function beautify(proxies, subscription) {
  subscription = subscription || {};
  const subscriptionName = String(subscription.name || '');
  const prefix = configuration.EnableSubscriptionName !== false && subscriptionName ? subscriptionName + ' | ' : '';
  const reserved = configuration.ReservedKeywords ? new RegExp(String(configuration.ReservedKeywords), 'gi') : null;
  const counts = new Map();
  const sorted = proxies.slice().sort(function(a, b) {
    const key = function(p) { return [p.server || '', p.server_port || '', p.type || '', p.tag].join('\u0000'); };
    return key(a) < key(b) ? -1 : key(a) > key(b) ? 1 : 0;
  });
  return renameNodes(sorted, function(name) {
    [subscriptionName + ' | ', '[' + subscriptionName + '] '].forEach(function(oldPrefix) {
      if (subscriptionName && name.indexOf(oldPrefix) === 0) name = name.slice(oldPrefix.length);
    });
    name = name.replace(/\s+/g, ' ').trim();
    // Traffic / expiry notices are not regions (e.g. a traffic quantity in GB).
    if (/^(剩余流量|已用流量|总流量|流量剩余|套餐到期|订阅到期|到期时间|有效期|官网|距离下次重置|remaining traffic|expires?|website)\s*[:：]?/i.test(name)) return prefix + name;
    let region = regionFor(name);
    if (!region) region = regions.find(function(r) { return name.indexOf(r[1]) >= 0; });
    if (!region) {
      const city = cities.find(function(c) { return c[1].test(name); });
      if (city) region = regions.find(function(r) { return r[2] === city[0]; });
    }
    if (!region) return prefix + name;
    const originalRegion = name.match(region[0]);
    const cityRule = cities.find(function(c) { return c[0] === region[2]; });
    const cityMatch = cityRule ? name.match(cityRule[1]) : null;
    const city = configuration.EnableCityName !== false && cityMatch ? cityMatch[0] : '';
    let rest = name.replace(region[0], ' ').split(region[1]).join(' ');
    if (cityRule) rest = rest.replace(cityRule[1], ' ');
    const extras = reserved ? Array.from(new Set(rest.match(reserved) || [])) : [];
    const count = (counts.get(region[2]) || 0) + 1;
    counts.set(region[2], count);
    const mode = configuration.EnableUnifyRegionName;
    const label = mode === '统一为英文' ? region[3] : mode === '不启用' && originalRegion ? originalRegion[0] : region[2];
    return prefix + [configuration.EnableNationalEmoji !== false ? region[1] : '', label, String(count).padStart(2, '0'), city].filter(Boolean).join(' ') + (extras.length ? ' | ' + extras.join(' ') : '');
  });
}
function onSubscribe(proxies, subscription) { return beautify(proxies, subscription); }
function onRun(proxies, subscription) {
  const result = beautify(proxies, subscription);
  console.log('Updated names for ' + result.length + ' nodes.');
  return result;
}
