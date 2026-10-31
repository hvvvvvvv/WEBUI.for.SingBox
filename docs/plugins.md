# WebUI 脚本插件

顶部「插件」提供与桌面版相近的独立管理页面：网格/列表、插件中心、检查更新、添加、源码、更多菜单和手动运行。插件是实际 JavaScript 源码，保存在路由器的 `data/plugins.json`，由 Go 后端执行。浏览器关闭时，定时更新订阅仍会触发插件。

插件源独立维护于 [Ghostdehole/WEBUI.for.SingBox-Plugins](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins)。官方桌面版使用 [GUI-for-Cores/Plugin-Hub](https://github.com/GUI-for-Cores/Plugin-Hub)，其 Generic/GFS/GFC 脚本依赖不同的桌面 API；本项目采用独立的后端接口，不能直接导入这些源码。

## 使用

1. 打开「插件 → 插件中心」，安装所需插件。随程序附带的目录离线可用。
2. 安装默认启用，但安装、编辑或检查更新本身不会运行脚本。更多菜单可编辑参数、触发器、启停、顺序或删除插件。
3. 更新订阅时，按页面顺序执行启用的订阅插件；已有订阅脚本先运行。插件失败时，此订阅的旧缓存和元数据保留。
4. 「美化节点名称 → 运行」选择已有缓存的订阅，在后端处理并保存结果，不重新下载订阅、不自动重启核心。检查生成配置后按需要手动重启核心。
5. 「检查更新」刷新独立仓库目录；点击插件的「更新」才替换已安装源码。更新保留参数、启停状态、触发器和顺序，会覆盖本地源码改动；自行修改的插件可改用新 ID 保存。

| 插件 | 功能 |
| --- | --- |
| 防代理重名 | 为冲突名称添加后缀，修复订阅内部出站引用 |
| 更好的代理名称显示 | 按名称关键词添加地区旗帜、可选正则移除关键词与添加索引 |
| 美化节点名称 | 清理空格、添加订阅名或地区旗帜、统一地区名称；支持手动触发 |
| 重复节点移除 | 合并除名称外完整配置相同的节点，保留凭据/TLS/协议不同的节点 |

命名插件同步订阅内部的 `detour`、`default`、`outbounds` 引用。原始重名无法区分的引用指向第一个同名节点；配置中手工填写的旧名称不会自动重写，应检查生成结果。地区识别只根据名称，不查询 IP 定位。

参数通过 JSON 编辑。更好的名称显示中 `EnableIndexProxyName` 为 `"1"` 时给所有节点加索引，其他值仅消除名称冲突；`RemoveKeywords` 是 JavaScript 正则表达式。美化插件的 `EnableUnifyRegionName` 支持 `不启用`、`统一为中文`、`统一为英文`。

## 开发接口

源码使用普通 JavaScript 函数，无 `import`/`export`。为元数据中声明的触发器实现对应函数：

| 触发器 | 函数 | 输入与返回值 |
| --- | --- | --- |
| `on::subscribe` | `onSubscribe(proxies, subscription)` | sing-box 出站对象数组；订阅元数据 |
| `on::generate` | `onGenerate(config)` | 完整 sing-box 配置对象 |
| `on::manual` | `onRun(proxies, subscription)` | 所选已缓存订阅的出站数组与元数据 |

返回新数组/对象，或原地修改参数并不返回值。`undefined` 表示使用修改后的输入；`null` 和错误类型会被拒绝。每次调用创建独立 JS 环境，不能靠全局变量跨调用保存状态。纯 JS 的立即兑现 Promise 可用，但没有事件循环、网络请求或定时器。

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

全局 `configuration` 是插件参数对象。`console.log/info/warn/error/debug` 与 `Plugins.Log` 可记录受限日志，手动运行显示结果。没有文件系统、Shell、网络、Wails、桌面窗口和前端 Store API。

配置插件在原有 mixin 和配置脚本后执行；随后仍规范化 HTTP 配置并强制应用管理所需的原生 API 设置。生成失败不覆盖已有配置文件。脚本可调整配置数据，但不直接改变系统路由表、dnsmasq、防火墙或运行中的核心。

安装和编辑只编译语法，不执行脚本。声明的函数是否存在、数据输出及业务语义在实际触发时检查；仍应预览配置并使用对应核心的配置检查验证。

## 资源与并发

- 最多 32 个插件；单源码 256 KiB、参数 64 KiB，持久化总量 4 MiB。
- 单次输入/输出最多 8 MiB，最多 10,000 个订阅节点；单钩子 2 秒，整次流水线含排队最多 5 秒。
- 同时只运行一个插件流水线，调用栈上限 512；日志最多 100 行/32 KiB，单行 4 KiB。
- 每次执行使用固定安装快照。多页面编辑使用 revision 检查，冲突会提示重新读取，避免静默覆盖。
- Goja 的时间、栈和序列化大小限制不等同于强内存隔离；管理员应只安装可信源码，尤其在内存较小的路由器上。

## 独立目录维护

仓库使用 `index.json`（`apiVersion: webui.plugins/v1`）和 `plugins/*.js`。清单保存元数据、默认参数、相对路径与 SHA256。一次刷新必须全部校验通过，失败继续使用旧缓存或程序附带目录。SHA256 用于核对清单与文件一致性，来源信任依赖仓库维护权限和 HTTPS。

编辑 JS、递增清单中的版本后，在主程序源码目录运行：

```sh
go run ./cmd/plugin-check -write-index ../WEBUI.for.SingBox-Plugins
go run ./cmd/plugin-check ../WEBUI.for.SingBox-Plugins
```

校验器与实际安装共用解析器，限制文件大小，拒绝非预期路径与符号链接，只编译源码不执行。提交 JS 与新清单后，用户可自行检查并更新，无需为每次插件更新重编译 WebUI。主程序 `plugin-catalog/` 是离线快照，发布主程序时可同步更新。

本功能纯 Go 构建，覆盖主程序已有路由器架构；是否能实际开启 TUN/透明代理仍取决于固件能力，见 [路由器部署说明](routers.md)。未提供桌面系统代理、托盘、UWP 等插件 API。
