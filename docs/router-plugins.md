# 路由器插件：使用、维护与上游比较

本功能面向网络设备的透明代理。独立目录为 [Ghostdehole/WEBUI.for.SingBox-Plugins](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins)，随程序还内置一份离线目录。插件使用声明式 JSON，在后端生成配置时执行；关闭浏览器后配置与核心照常工作。

## 三个仓库的取舍

2026-10-31 比对基线：GUI.for.SingBox 为 f0de129，hvvvvvvv/WEBUI.for.SingBox 为 8d2dc2e，本分支在此前修复基线 fe5e36d 上增加此功能。

| 能力 | 上游情况 | 本分支处理 |
| --- | --- | --- |
| 插件管理和独立目录 | GUI 桌面版有，WEBUI 上游移除了桌面插件 | 恢复管理入口与独立目录，采用新的路由器协议 |
| 配置/订阅变换、规则集更新 | 两个上游都有可借鉴实现 | 保留现有后端配置、订阅与计划任务；插件统一接入配置生成流水线 |
| TUN、客户端分流、DNS | WEBUI 已有配置模型；GUI 有配置相关脚本 | 提供可安装、可启停的路由器预设，引用现有 TUN 和出站 |
| 服务自启、低闪存写入、静态构建 | 需要后端和设备适配 | 保留 procd、日志保留设置、Linux 多架构构建 |
| 桌面窗口、托盘、UWP、系统代理、壁纸 | 依赖 Wails/桌面会话 | 不恢复 |
| 流量统计、规则集精选、AdGuard Home | 官方插件有类似功能 | 可继续适配；后台统计需控内存/落盘，外部服务需处理端口和服务生命周期 |

官方插件位于 [GUI-for-Cores/Plugin-Hub](https://github.com/GUI-for-Cores/Plugin-Hub)，桌面版默认源是 [generic.json](https://github.com/GUI-for-Cores/Plugin-Hub/blob/main/plugins/generic.json) 与 [gfs.json](https://github.com/GUI-for-Cores/Plugin-Hub/blob/main/plugins/gfs.json)。其插件在前端动态加载 JS，并通过全局 Plugins 访问 Bridge、Stores 等桌面能力，见 [官方加载代码](https://github.com/GUI-for-Cores/GUI.for.SingBox/blob/main/frontend/src/stores/plugins.ts)。本项目不设置这些默认源，也不执行这些脚本。

官方 [共享到 OpenWrt 插件](https://github.com/GUI-for-Cores/Plugin-Hub/blob/main/plugins/GFS/plugin-share-profile-to-openwrt.js) 是桌面生成配置后临时 HTTP 分享，不是 OpenWrt 的防火墙或 procd 管理器；其旧地址字段和拓扑假设也不能直接套到当前版本。

## 使用

1. 新建或编辑配置，先配置并启用一个 TUN 入站，保留可用的 DNS 和出站。
2. 打开配置编辑器第八页「路由器插件」，查看环境检查，安装所需插件。
3. 选择实际入站/出站标签，填写接口或客户端 CIDR。新安装的插件默认禁用，配置完成后再启用。
4. 预览完整生成配置，保存后手动重启核心。核心启动路径会运行现有 sing-box check 预检；预检失败不会先停止正在运行的核心。预检成功不等于运行时网络一定可用，也不代表已实现网络故障后的自动回滚。

同一页面可导入/导出插件 JSON、调整客户端策略顺序、禁用或移除插件。管理页面只编辑草稿；保存、更新目录或安装插件不自动重启核心。

### 首批执行能力

- **tun-routing**：选中现有 TUN，启用 auto_route，设置 strict_route 和显式选择的 auto_redirect；可限制进入的接口并增加排除地址。空接口列表保留原有限制，非空列表替换；排除地址与原有 route_exclude_address 合并去重。保留手动指定的出口，未指定时启用 auto_detect_interface，关闭对网关本机进程的查找。拒绝把出口或纳入接口指回 TUN 本身。
- **source-routing**：按指定 TUN 中的客户端 IPv4/IPv6 CIDR，将流量路由到当前生成配置中的一个出站。出站必须存在且无歧义。可添加多个实例，列表靠前的客户端策略优先。
- **dns-hijack**：仅对指定 TUN 中的 53 端口流量添加 hijack-dns。所有此类插件规则先于客户端分流规则；之后才是原有 route.rules。不会改写 DNS 服务器或额外占用 53 端口。

客户端策略优先于原有路由规则，包括原有的私网直连策略。按需在 TUN 排除地址中保留管理网段、NAS、VPN 内网等，避免改变其路径。插件不会新建或启用 TUN，也不会替用户选择节点、地区分流政策或上传配置。

### 固件与 DNS 边界

插件本身不执行 UCI、防火墙、路由或 Shell 命令。**启用对应配置并启动 sing-box 后，sing-box 会根据 auto_route / auto_redirect 管理系统路由或防火墙规则。**

auto_redirect 默认关闭；当前受支持核心的该能力需要 Linux、auto_route 与对应 nftables 支持，不能凭发现 nft 命令就认定固件已满足条件。GL-MT3600BE 的厂商固件不能直接当作新版 fw4；OpenWrt 自 22.03 才默认切到 fw4。还要检查与其他 VPN、代理和 routing_mark/default_mark 的冲突。[sing-box TUN 文档](https://sing-box.sagernet.org/configuration/inbound/tun/)、[OpenWrt 22.03 说明](https://openwrt.org/releases/22.03/notes-22.03.0)

发往路由器自身 LAN IP / loopback 的 53 请求通常仍由 dnsmasq 或其他 DNS 服务接收，不能仅凭 TUN DNS 插件承诺全 LAN 接管。需要完整整合时，应规划 dnsmasq/GL DNS Proxy 到独立 DNS 监听端口的转发，并验证 bootstrap、IPv6 RA/DHCP 分发和回环；本版不自动修改这些设置。[GL.iNet DNS 文档](https://docs.gl-inet.com/router/en/4/interface_guide/dns/)

诊断读取平台、实际 CAP_NET_ADMIN、TUN 字符设备、固件标识、nft/iptables/fw4 是否可查找、IPv4/IPv6 转发和 53 端口 socket 概况。它不运行命令、不修改设置、不绑定端口、不向外网请求；单次响应有时间和读取大小限制。诊断通过仍不能替代设备上的 LAN/DNS 连通性与停止后恢复测试。

## 独立目录更新

WebUI 默认读取已校验缓存，没有缓存时使用内置目录。只有点击检查目录更新才访问本项目独立仓库；不会自动扫描官方桌面插件源。目录和插件都有限额、超时及协议校验，每个下载文件必须匹配索引 SHA256；任一失败保留上一次完整目录。

目录更新不会改动配置。已安装实例保存完整 manifest 快照和启用状态；用户手动更新实例时保留自身 config 参数，只更新兼容协议/能力的元信息和版本。要采用新预设的默认参数，需要重新安装该预设。API 或执行能力发生改变时，先升级主程序，不能通过插件源注入新的执行代码。

离线缓存位于 data/.cache/router-plugin-catalog.json；实例随 data/profiles.yaml 一起保存、复制和备份。不要把含真实内部网络参数的导出误提交到公开仓库。

## 插件协议 v1

所有字段大小写固定，禁止未知字段、重复 JSON 键、null 和可执行脚本。例子：

~~~json
{
  "apiVersion": "webui.router/v1",
  "id": "example.device-routing",
  "name": "Device routing",
  "description": "Route selected clients through a chosen outbound.",
  "version": "1.0.0",
  "kind": "source-routing",
  "config": {
    "inbound": "",
    "source_ip_cidr": [],
    "outbound": ""
  }
}
~~~

apiVersion 固定；id 为 1–64 个英文字母、数字、点、下划线或短横线，首字符为字母或数字；version 使用语义版本。manifest 的 id 标识目录项目，每次安装的实例另有独立 id，因此允许多个同类客户端策略。

| kind | config 必需字段 |
| --- | --- |
| tun-routing | inbound 字符串、auto_redirect 布尔、strict_route 布尔、include_interface 字符串数组、exclude_address CIDR 数组 |
| source-routing | inbound 字符串、source_ip_cidr CIDR 数组、outbound 字符串 |
| dns-hijack | inbound 字符串 |

模板和禁用草稿可以保留空引用；启用时必须填写引用和 source-routing 的客户端列表。标签最长 128 字节，接口名最长 15 字节、最多 16 个；CIDR 列表最多 256 项，重复网段会被拒绝。单个 manifest 最大 16 KiB；每个配置最多 32 个实例、manifest 总计不超过 256 KiB；启用插件时生成配置和输出限制为 8 MiB。超限返回错误，不截断配置。

后端应用顺序为：原配置生成 → 混入 → 用户配置脚本（如启用）→ 声明式路由器插件 → HTTP 客户端规范化和管理 API 约束。插件使用独立配置副本；校验或引用失败时不留下部分变更。

目录索引协议为 webui.router.catalog/v1；每项包含 plugins/*.json 路径与小写 SHA256，最多 32 项，不允许跨目录路径或重定向。使用主程序实际校验器维护：

~~~sh
go run ./cmd/router-plugin-check -write-index ../WEBUI.for.SingBox-Plugins
go run ./cmd/router-plugin-check ../WEBUI.for.SingBox-Plugins
~~~

主程序内置目录位于 router-plugins/catalog/，供离线使用；独立仓库是在线目录的维护源。发布主程序前可将已审核的新模板同步到内置目录。禁止直接把官方 JS 换个扩展名放进此源。
