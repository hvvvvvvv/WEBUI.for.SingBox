# OpenWrt / GL.iNet 路由器部署

WebUI 是管理服务，sing-box 是独立的代理核心。WebUI 的 Linux 发布包使用 `CGO_ENABLED=0`，不依赖 glibc 或 musl 动态加载器；核心下载器会单独选择对应的 sing-box 资源。

## 选择架构

先在路由器 SSH 中检查，而不是仅根据型号或固件名称选择：

```sh
uname -m
uname -r
cat /etc/openwrt_release
```

| 设备输出 / 平台 | WebUI 发布包后缀 | 说明 |
| --- | --- | --- |
| `aarch64` | `linux-arm64.zip` | GL-MT3600BE 优先检查此目标 |
| `armv7l` | `linux-armv7.zip` | ARMv7，不能用于 ARMv5/ARMv6 |
| `x86_64` | `linux-amd64.zip` | x86 软路由 |
| 32 位 MIPS，大端 | `linux-mips.zip` | 使用 softfloat |
| 32 位 MIPS，小端 | `linux-mipsle.zip` | 使用 softfloat |

部分 MIPS 固件的 `uname -m` 不能区分字节序，需要结合固件的软件包架构信息（如 `opkg print-architecture`）确认。WebUI 不提供 ARMv5/ARMv6 或 MIPS64 发布包。极小内存/闪存设备仍需自行评估核心、规则集和连接数的资源占用。

[GL-MT3600BE 官方资料](https://static.gl-inet.com/www/images/products/datasheet/mt3600be_datasheet_20260213.pdf)列出 OpenWrt 21.02 和 Linux 5.4.281；实际安装的固件可能不同。不要把型号相同理解成内核模块、防火墙和软件包源完全相同。设备操作见 [GL.iNet 用户指南](https://docs.gl-inet.com/router/en/4/user_guide/gl-mt3600be/)。

## 部署角色

| 场景 | WebUI 管理服务 | 透明代理生效的额外条件 |
| --- | --- | --- |
| 主网关 | 绑定管理 LAN 地址、procd 自启、持久化 data | 实际 LAN 接口、转发、TUN/规则能力；与厂商 VPN/多 WAN 策略明确分工 |
| 旁路由 | 管理地址固定，认证与主路由分别管理 | 客户端网关/DNS 或主路由策略必须将目标流量送来；仅启动 WebUI 不会改变 DHCP |
| x86/ARM 软路由 | 使用对应架构静态程序，procd/systemd 管理 | 核对物理接口、VLAN、排除管理网段及停止服务后的路由恢复 |
| Linux 服务器 | systemd、独立目录及 HTTPS/VPN 远程访问 | 作为实际网关才涉及 LAN 接管；仅管理自身流量时限制入站与路由范围 |
| 容器 bridge 网络 | 持久卷、管理端口映射、可传启动参数 | TUN/路由默认作用于容器网络空间；需独立设计客户端到容器的流量路径 |
| Linux 容器 host 网络 | 与宿主共用端口，独立 data，避免监听冲突 | TUN/NET_ADMIN 会影响宿主网络；排除管理连接并核对宿主防火墙 |
| Windows/macOS | 对应系统服务可运行 WebUI 与受支持核心 | Linux 的 `auto_redirect`、nftables/fw4 不适用，不能承诺复用路由器 TUN 配置 |

配置生成插件默认关闭或使用空规则。DNS 分流引用已有 DNS 服务，客户端分流引用已有出站，TUN 插件负责捕获范围；不要让多个插件或系统服务重复创建同一入口/策略。路由器/手机时间同步后才启用 TOTP，详见 [访问与安全](auth.md)。

## 安装和服务

将对应发布包在电脑上解压，将其中的 `webui.for.singbox` 上传到路由器的持久目录，例如 `/opt/webui/`。确认该目录位于可执行、可写的持久存储上；外置磁盘需要在服务启动前挂载。程序使用同目录的 `data/` 保存配置、订阅、规则集和核心。不要将它放到重启即清空的 `/tmp`。

在路由器 SSH 中执行以下命令，先将 `LAN_IP` 改为实际管理网段地址：

```sh
cd /opt/webui
chmod +x webui.for.singbox
LAN_IP='你的路由器LAN地址'
./webui.for.singbox service install --addr "$LAN_IP:9090" --log-level info --log-days 0
./webui.for.singbox service start
./webui.for.singbox service status
```

通过 `http://路由器LAN地址:9090` 打开界面，并设置管理密钥。仅向可信管理网络开放该端口；远程访问可通过已有 VPN 或 HTTPS 反向代理。`--log-days 0` 关闭应用日志落盘，输出仍由 procd 收集，可使用 `logread` 查看，以减少闪存写入；已有日志不会自动删除。核心日志设置独立于此参数。

OpenWrt 服务使用 procd 直接监控前台进程，自动重启异常退出的服务，停止时预留 35 秒供 Web 请求退出及核心清理路由。服务参数逐项引用，支持含空格的路径。变更地址、日志参数或安装目录时，先 `service uninstall`，再从目标位置重新安装。升级旧安装后也应重新安装服务，以应用新版 procd 脚本。

核心可通过界面下载；无法访问 GitHub 时，按 [README 的手动安装说明](../README.md#手动安装-sing-box-core)上传。在线更新必须通过发布资源提供的 SHA256 校验后才会替换文件，校验失败或缺少摘要时保留旧文件。自编译可用 `make build APP_REPOSITORY=owner/repository` 指定自己的更新仓库；GitHub 发布流程自动使用当前仓库。

本分支启动核心要求 sing-box 1.14.2 或更新版本。应用更新会先完整解压并验证候选程序，再启动更新助手并退出旧程序；服务模式和直接运行模式均适用。准备阶段遇到损坏包、磁盘写满、请求取消或助手启动失败时，旧程序继续运行；同一时间只接受一个更新应用请求。路由器建议使用上述服务模式，以获得自启和进程监管。

## TUN、DNS 和固件差异

先确认核心版本能够启动，再启用透明代理。WebUI 构建成功不代表固件已提供所有内核能力：

```sh
test -c /dev/net/tun && echo 'TUN device available'
command -v nft
```

TUN 缺失时，需要与当前固件内核严格匹配的 `kmod-tun`；从固件自己的软件源安装。启用 `auto_redirect` 还需要 sing-box 所需的 nftables 支持。厂商固件的防火墙实现可能与同版本的原生 OpenWrt 不同，请按 [sing-box TUN 文档](https://sing-box.sagernet.org/configuration/inbound/tun/)检查；本项目不会默认打开 `auto_redirect` 或替你改写路由器防火墙。

路由器通常已有 dnsmasq 或 AdGuard Home 占用 53 端口。新增 DNS 入站时应使用未占用的监听地址/端口，再按网络规划配置转发或 DNS 劫持，避免直接与现有服务争抢端口。GL.iNet VPN、策略路由、其他代理程序也可能接管流量；先明确由哪一个服务管理目标流量。

GL.iNet 的[硬件加速说明](https://docs.gl-inet.com/router/en/4/interface_guide/hardware_acceleration/)指出，加速路径与依赖内核网络栈/防火墙的功能存在取舍。透明代理、统计或客户端策略没有按预期命中时，应核对固件的加速设置和实际经过的流量路径；WebUI 不会自行切换厂商加速、VPN、DHCP 或防火墙开关。

WebUI 根据实际核心版本处理 TUN `stack`：1.14 保留现有配置，1.15 及以后不再输出已弃用的字段。乐观 DNS 缓存为可选项，旧配置不自动启用；开启后不能同时禁用 DNS 缓存或缓存过期。

## 备份与回滚

更新前停止服务，备份原程序和整个 `data/` 目录。需要回滚时停止新服务，恢复匹配的程序和数据备份后重新启动。包含同名节点的不同订阅现在会生成唯一标签；首次升级时核心原有的节点选择缓存可能回退到组默认项，确认一次选择即可。

## 隔离兼容性验证

1.4.0 的 CI 包含 Linux x64/ARM64、Windows、macOS 原生测试及其他发布目标交叉编译；容器验证 IPv6 监听、额外参数传递和管理接口启动。完整 ARM64 QEMU 虚拟机分别启动官方 OpenWrt 21.02.7 与 24.10.4 镜像，使用四个模拟 CPU、512 MiB RAM、独立磁盘及私有用户态 NAT。前者覆盖 GL-MT3600BE 厂商固件所用的 OpenWrt 21.02/Linux 5.4 系列环境，后者覆盖较新的 OpenWrt 环境；均不是厂商固件镜像。

虚拟机检查实际 procd 安装/启动/停止/重启、密码/TOTP/恢复码/会话撤销，以及 ARM64 Goja 插件与 sing-box 1.14.4 配置校验。镜像与核心下载均核对固定 SHA256，入口为 `tests/compat/openwrt-vm.sh`，日志保存在对应 Actions 的构建产物中。每次检查是否通过，以该提交 Actions 结果为准。

这不会连接用户正在使用的设备，也不会接入真实 LAN 或修改宿主防火墙。未模拟 MTK 硬件加速、无线、厂商 VPN、多 WAN 或真实多客户端转发吞吐量；通过虚拟机检查不等于已在 GL-MT3600BE 实机验证透明代理。实际部署仍需在维护窗口确认 LAN/DNS 连通性、TUN/防火墙能力和停止服务后的路由恢复。
