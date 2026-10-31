# WebUI 脚本插件

顶部「插件」提供与桌面版相近的独立管理页面：网格/列表、插件中心、检查更新、添加、源码、更多菜单和手动运行。插件是实际 JavaScript 源码，保存在路由器的 `data/plugins.json`，由 Go 后端执行。浏览器关闭时，定时更新订阅仍会触发插件。

[Ghostdehole/WEBUI.for.SingBox-Plugins](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins) 是插件中心唯一的发行来源，维护清单、源码和版本。主程序只提供下载、管理和执行接口，不包含插件发行目录或内置源码。官方桌面版使用 [GUI-for-Cores/Plugin-Hub](https://github.com/GUI-for-Cores/Plugin-Hub)，其 Generic/GFS/GFC 脚本依赖不同的桌面 API；本项目采用独立的后端接口，不能直接导入这些源码。

## 使用

1. 打开「插件 → 插件中心」。有有效目录缓存时先显示缓存，没有缓存时只获取独立仓库的 `index.json` 元数据列表；「检查更新」也只刷新列表，不下载 JS、不安装插件。列表获取失败且无缓存时显示空目录和警告，有缓存时继续显示缓存。选择所需插件并点击「安装」，才下载该插件源码，通过 SHA256 和语法校验后保存。
2. 安装后的插件开关默认启用，但安装、编辑或检查更新本身不会运行脚本。配置生成类插件的功能开关默认为关闭，或使用空规则，默认不会改变配置。在「设置 → 插件」用开关、单选项、文本框和多行文本框修改参数，自动保存到后端；支持逐项及整插件恢复默认。更多菜单也可打开相同表单，并提供高级 JSON 编辑、触发器、启停、顺序和删除。
3. 更新订阅时，按页面顺序执行启用的订阅插件；已有订阅脚本先运行。插件失败时，此订阅的旧缓存和元数据保留。
4. 「美化节点名称 → 运行」选择已有缓存的订阅，在后端处理并保存结果，不重新下载订阅、不自动重启核心。检查生成配置后按需要手动重启核心。
5. 诊断插件点击「运行」即可从路由器后端发起检查，不选择订阅、不写节点缓存、不切换全局节点，也不重启核心。结果显示 JSON 报告和日志；网站实际可用性仍取决于设备出口、服务方限制及当时网络。
6. 点击插件的「更新」才下载该插件的新源码，通过 SHA256 和语法校验后替换已安装版本；下载或校验失败保留原版本。更新保留参数、启停状态、触发器和顺序，会覆盖本地源码改动；自行修改的插件可改用新 ID 保存。已安装插件可离线执行，缓存列表可离线浏览，但安装或更新仍需联网下载所选源码。

## 升级与独立仓库插件

先将主程序升级到 **1.3.1 或更新版本**，再从独立仓库更新插件。`TextArea`、`manual_target` 和 `conflicts` 元数据从 1.3.0 引入；1.3.1 取消主程序随附插件，统一从独立仓库浏览列表、按用户选择下载源码。旧版含源码的目录缓存会被忽略并重新获取元数据列表；已安装插件保持不变。

防代理重名、更好的代理名称显示、美化节点名称是三种替代命名策略，最多启用其中一个。旧版本若同时启用了多个，请先禁用多余的命名插件，再更新各插件并保留一个启用。更新保留旧启停状态，不会替用户静默禁用其他插件；检测到冲突时拒绝保存并显示两个插件名称。已保存的旧冲突组合不会阻止服务启动，但同一流水线选中冲突插件时会在执行前报错，保留旧数据。重复节点移除可以和一种命名策略配合使用。

独立仓库目前发行 16 个插件：4 个订阅处理、8 个配置生成、4 个手动诊断，均由用户选择安装。它们是适配版本；功能相近的官方脚本进行了合并，桌面常驻服务与界面装饰并未逐项复制。

| 插件 | 触发方式 | 功能与范围 |
| --- | --- | --- |
| 防代理重名 | 订阅 | 按节点顺序添加索引，修复订阅内部出站引用 |
| 更好的代理名称显示 | 订阅 | 按名称关键词添加地区旗帜，可选正则移除关键词与添加索引 |
| 美化节点名称 | 订阅、手动订阅 | 订阅前缀、地区旗帜、统一地区名称、保留城市名及关键词 |
| 重复节点移除 | 订阅 | 合并除名称外完整配置相同的节点，保留凭据/TLS/协议不同的节点 |
| 出站链管理 | 配置生成 | 为指定节点或分组成员设置 detour，检查引用和环路，拒绝覆盖冲突绑定 |
| 配置覆盖 | 配置生成 | 合并明确提供的 JSON；数组默认替换，可指定路径追加；保护已有入站、端点和管理 API |
| 按需引用规则集 | 配置生成 | 为选定的 HTTPS sing-box 规则集生成引用；由核心下载，不批量安装整表 |
| 路由器透明代理 | 配置生成 | 生成限定 LAN 接口的 TUN、客户端分流及可选 DNS 劫持；默认关闭 |
| 路由器独立代理端口 | 配置生成 | 添加 HTTP/SOCKS/mixed 入站，可绑定已有出站；默认不添加端口 |
| 核心自动测速选择 | 配置生成 | 用 sing-box 原生 URLTest 维护候选节点组，默认关闭；不复刻桌面日志阈值和常驻评分器 |
| 路由器 Hosts DNS | 配置生成 | 将显式 hosts 映射应用于 sing-box DNS，不改 `/etc/hosts` |
| AdGuardHome DNS 对接 | 配置生成 | 对接已有 AdGuardHome DNS 服务；不安装、启动或托管其进程 |
| 路由器出口与连通诊断 | 手动诊断 | 查询当前后端出口并检查指定站点，不据此推断所有 LAN 客户端的路径 |
| IP 地区与运营商查询 | 手动诊断 | 向 ipwho.is 查询指定 IP 或当前出口；地区信息仅供参考 |
| 流媒体可达性诊断 | 手动诊断 | Netflix、YouTube Premium、Spotify 的只读可达性和明确区域拒绝检查；HTTP 200 不等于完整解锁 |
| 路由器限量下载测速 | 手动诊断 | 当前出口的短时 HTTP 下载采样，默认 1 MiB；不切节点，不执行逐节点批量测速 |

命名插件同步订阅内部的 `detour`、`default`、`outbounds` 引用。原始重名无法区分的引用指向第一个同名节点；配置中手工填写的旧名称不会自动重写，应检查生成结果。地区识别只根据名称，不查询 IP 定位。

更好的名称显示中 `EnableIndexProxyName` 为 `"0"` 时不添加索引，`"1"` 时全部编号，`"2"` 时仅消除名称冲突；`RemoveKeywords` 是 JavaScript 正则表达式。美化插件的 `EnableUnifyRegionName` 支持 `不启用`、`统一为中文`、`统一为英文`；`EnableCityName` 控制城市，`ReservedKeywords` 保留 IEPL、BGP、倍率等匹配内容。已识别地区按稳定顺序编号，流量和到期提示保持原文。

独立仓库的命名插件使用完整官方地区表及兼容别名，覆盖 **142 个地区、907 个国家/地区/城市关键词**；未知名称保留原文。节点 ID 和内部引用仍会保留，排序不依赖刷新时临时生成的 GUI ID。数据、归属和调整说明见 [命名来源](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins/blob/main/sources/naming/NOTICE.md)，无需升级主程序即可随插件更新地区数据。

## 配置生成与路由器部署

生成器使用生成配置中的实际标签，而非只看页面显示名称。先填写参数、预览生成结果，再使用对应 sing-box 核心检查；插件参数保存本身不会重启核心。插件按安装顺序执行，后续插件会看到前面插件生成的结果。

透明代理插件面向 Linux/OpenWrt 和 sing-box 1.14+。必须明确填写真实 LAN 接口、不会与现有网络重叠的 TUN 地址，以及需要绕过的 LAN、管理或其他 VPN 网段。已有 TUN 应在配置页面直接调整；插件会拒绝替换它，也会拒绝与 `default_mark`/`routing_mark` 冲突的自动重定向组合。新增路由规则按 DNS 劫持、客户端分流、可选 TUN 默认出口、原规则排列，因此显式客户端规则和 TUN 默认出口会优先于原规则。

脚本只生成配置，不写系统防火墙、不运行 Shell，也不探测或改写现有系统路由。核心启动后，`auto_route`/`auto_redirect` 才按 sing-box 自身实现设置路由和重定向；设备仍需具备 TUN、NET_ADMIN、转发和相应 nftables/fw4 能力。DNS 劫持仅针对新增 TUN 的端口 53 流量，不绑定系统 53、不替换 dnsmasq；sing-box 1.14 不会因此自动劫持发往路由器自身接口地址的 DNS。

AdGuardHome 由 procd/systemd 等服务管理器管理，插件仅设置 sing-box 到该服务的 DNS 路径。确认监听地址和端口，并避免 AdGuardHome 上游再指回同一 sing-box DNS。独立代理端口默认监听回环地址；开放 LAN 监听时使用认证，系统实际端口是否可绑定仍由核心启动确认。URLTest 自动探测在核心中运行，并非浏览器定时器；默认不会中断已有连接或替换 selector 默认节点。

本轮没有在真实路由器上测试。静态构建、配置检查和本地回归不能替代固件上的转发、防火墙、DNS 与多客户端验证，见 [路由器部署说明](routers.md)。

## 开发接口

源码使用普通 JavaScript 函数，无 `import`/`export`。为元数据中声明的触发器实现对应函数：

| 触发器 | 函数 | 输入与返回值 |
| --- | --- | --- |
| `on::subscribe` | `onSubscribe(proxies, subscription)` | sing-box 出站对象数组；订阅元数据 |
| `on::generate` | `onGenerate(config)` | 完整 sing-box 配置对象 |
| `on::manual`，默认或 `manual_target: "subscription"` | `onRun(proxies, subscription)` | 所选已缓存订阅的出站数组与元数据 |
| `on::manual`，`manual_target: "diagnostic"` | `onRun()` | 无参数；返回 JSON 报告，不写订阅或配置 |

订阅和配置钩子返回新数组/对象，或原地修改参数并不返回值。`undefined` 表示使用修改后的输入；`null` 和错误类型会被拒绝。诊断返回 JSON 数据，未返回值时报告为 `null`，日志仍可显示。诊断插件只能声明 `on::manual`，不能兼用自动触发器。每次调用创建独立 JS 环境，不能靠全局变量跨调用保存状态。纯 JS 的立即兑现 Promise 可用，但没有事件循环或定时器；诊断网络接口为同步调用。

```js
function onSubscribe(proxies, subscription) {
  const prefix = String(configuration.prefix || "");
  // 此例只过滤节点，避免重命名时遗漏内部引用。
  return proxies.filter(function (proxy) {
    return !prefix || String(proxy.tag).startsWith(prefix);
  });
}

function onRun(proxies, subscription) {
  console.log("Processing " + subscription.name + ": " + proxies.length);
  return onSubscribe(proxies, subscription);
}
```

全局 `configuration` 是插件参数对象。`console.log/info/warn/error/debug` 与 `Plugins.Log` 可记录受限日志，手动运行显示结果。没有文件系统、Shell、Wails、桌面窗口和前端 Store API。普通订阅、配置和手动订阅钩子没有网络能力；仅手动诊断提供下面两个受限接口。

插件元数据可提供 `configuration_schema`：每项包含 `key`、`title`、`description`、`component`、`value` 和可选 `options`。`component` 只允许 `Switch`（布尔值）、`Input`（字符串）、`TextArea`（多行字符串）、`Radio`（字符串选项）；Input/TextArea 单值最多 16,384 字节，文本不会被当作代码执行。Radio 选项使用 `标签,值` 或仅标签的格式。`value` 是默认值，`configuration` 保存覆盖值，重置会移除对应覆盖并重新使用默认值。最多 64 项 / 64 KiB，后台校验控件与值类型；表单无法加载任意组件。

例如仓库清单中的开关：

```json
{"key":"EnableCityName","title":"保留城市名","description":"保留识别到的城市","component":"Switch","value":true}
```

基础 `configuration_schema` 从 WebUI 1.2.1 开始支持；本轮新增字段和控件需要 1.3.0。原安装的参数会保留，不会因为新版本默认值改变而静默重置。可选 `conflicts` 是最多 32 个其他插件 ID 的数组，不允许重复或引用自身；任意一方声明冲突，后台都会阻止同时启用保存，并在流水线执行前检查所选插件。

配置插件在原有 mixin 和配置脚本后执行；随后仍规范化 HTTP 配置并强制应用管理所需的原生 API 设置。生成失败不覆盖已有配置文件。脚本可调整配置数据，但不直接改变系统路由表、dnsmasq、防火墙或运行中的核心。

安装和编辑只编译语法，不执行脚本。声明的函数是否存在、数据输出及业务语义在实际触发时检查；仍应预览配置并使用对应核心的配置检查验证。

## 手动诊断网络接口

```js
// 元数据：triggers: ["on::manual"], manual_target: "diagnostic"
function onRun() {
  const result = Plugins.fetch("https://www.cloudflare.com/cdn-cgi/trace", {
    method: "GET",
    route: configuration.Route || "configured"
  });
  console.log("HTTP " + result.status);
  return result;
}
```

`Plugins.fetch(url, {method, route})` 仅支持 GET/HEAD，默认 GET。返回 `{status, body, elapsed_ms, route, truncated}`；HTTP 非 2xx 仍返回状态码，网络错误则抛出异常。大响应截断时 `truncated: true`，不能按完整内容解析或据此宣称检测成功。错误的 `code` 可为 `invalid`、`policy`、`tls`、`timeout` 或 `network`；`tls` 仅表示证书验证失败，不自动断言某个根证书缺失。

`Plugins.measureDownload(url, {route, max_bytes})` 只发 GET，默认采样 1 MiB，最多 8 MiB。返回 `{status, bytes, elapsed_ms, bytes_per_second, route, truncated, incomplete}`，不返回响应正文。非 2xx 或连接建立失败会报错；已经读到部分数据后发生读取失败或单请求超时，会返回 `incomplete: true` 的样本。达到采样上限而响应未结束会标记 `truncated`。耗时包含请求建立过程，这只是短时当前出口采样，不代表线路极限带宽或逐节点排名。

两者的 `route` 允许 `configured`（遵循 WebUI 后端网络设置）、`direct`（直连）、`core`（通过当前运行核心的 HTTP/mixed 入站）。`core` 不可用时报错，不静默直连、不切换核心 selector。它们只接受无 URL 用户名/密码的 HTTP(S) 地址，不接受自定义请求头、正文或 Cookie，不附带 WebUI 的 GitHub Token，不使用下载镜像；保留设置的 User-Agent。最多跟随 3 次同源重定向，不允许跨源或 HTTPS 降级。

诊断由已认证管理员主动执行，能够访问脚本指定的公网或内网地址，**没有域名白名单隔离**。这不是对任意不可信代码的防 SSRF 沙箱。IP 查询会向指定第三方发送查询 IP，连通和媒体检查也会访问对应服务；先检查插件源码和目标，再在路由器上运行。

## 资源与并发

- 最多 32 个插件；单源码 256 KiB、参数 64 KiB，持久化总量 4 MiB。
- 普通订阅/配置处理单次输入/输出最多 8 MiB，最多 10,000 个订阅节点；单钩子 2 秒，整次流水线含排队最多 5 秒。
- 手动诊断含排队最多 20 秒；单请求最多 5 秒。每次诊断最多 8 次 HTTP 请求，重定向也计数；fetch 正文每次最多 64 KiB、合计最多 256 KiB，失败响应的已读内容也消耗预算；报告最多 256 KiB。
- 每次诊断最多一次下载测速，采样最多 8 MiB，与 fetch 共用请求次数和总时限；超限会报错，不自动追加采样。
- 同时只运行一个插件流水线，调用栈上限 512；日志最多 100 行/32 KiB，单行 4 KiB。
- 每次执行使用固定安装快照。多页面编辑使用 revision 检查，冲突会提示重新读取，避免静默覆盖。
- Goja 的时间、栈和序列化大小限制不等同于强内存隔离；管理员应只安装可信源码，尤其在内存较小的路由器上。

## 独立目录维护

独立插件仓库使用 `index.json`（`apiVersion: webui.plugins/v1`）和 `plugins/*.js`。清单保存元数据、默认参数、相对路径与 SHA256；这些发行文件只维护在插件仓库，不复制到主仓库或嵌入主程序。SHA256 用于核对清单与文件一致性，来源信任依赖仓库维护权限和 HTTPS。

打开插件中心时，没有有效目录缓存才请求独立仓库的 `index.json`；已有缓存时先显示缓存，手动「检查更新」再请求新的元数据。目录解析与元数据校验通过后才替换缓存，失败保留有效缓存并提示原因，无缓存则返回空目录和明确警告。此过程不请求 `plugins/*.js`，不安装、不更新任何插件，也没有主程序内置目录回退。

用户点击某一项的「安装」或「更新」后，主程序只下载选中的 JS，并依次核对清单中的 SHA256、编译检查语法，再保存安装快照；任一步失败均不写入新安装或替换旧源码。目录缓存只保存元数据，和 `data/plugins.json` 中含源码的已安装插件分别存储。目录缓存只能用于离线浏览，离线时不能从中心新安装或更新；已安装插件仍可离线执行。刷新目录、请求失败和主程序升级均不会替换已安装源码。

该交互遵循原版 GUI 的分阶段流程：[打开空中心只刷新目录](https://github.com/GUI-for-Cores/GUI.for.SingBox/blob/f0de129e3c7ee0278eff12f13bbfa435fcc8e57d/frontend/src/views/PluginsView/components/PluginHub.vue#L58-L92)，[目录更新只读取 JSON](https://github.com/GUI-for-Cores/GUI.for.SingBox/blob/f0de129e3c7ee0278eff12f13bbfa435fcc8e57d/frontend/src/stores/plugins.ts#L662-L687)，用户选择添加后才[下载所选源码](https://github.com/GUI-for-Cores/GUI.for.SingBox/blob/f0de129e3c7ee0278eff12f13bbfa435fcc8e57d/frontend/src/stores/plugins.ts#L548-L607)。WebUI 在保存前另行检查摘要和语法，不复制桌面安装事件或文件宿主。

旧版本曾随附插件目录，1.3.0 还采用过随附条目与下载缓存合并；这两项均已在 1.3.1 取消。旧版包含全量源码的目录缓存不作为新版元数据缓存使用；升级后需要重新获取 `index.json`，不会将旧目录缓存中的脚本自动安装，也不会改动已有安装。

编辑 JS、递增清单中的版本后，在主程序源码目录运行：

```sh
go run ./cmd/plugin-check -write-index ../WEBUI.for.SingBox-Plugins
go run ./cmd/plugin-check ../WEBUI.for.SingBox-Plugins
```

校验器与实际安装共用解析器，限制文件大小，拒绝非预期路径与符号链接，只编译源码不执行。将 JS 与新清单提交到独立插件仓库后，用户即可检查并更新，无需重编译 WebUI。发布主程序不再同步或打包插件源码；新增宿主接口能力仍需升级主程序。

本功能采用纯 Go 后端，可随主程序面向已有路由器架构构建；这不代表完成实机验证。未提供桌面系统代理、托盘、UWP 等插件 API，AdGuardHome、FRP 等独立守护进程的生命周期应交给 procd/systemd。
