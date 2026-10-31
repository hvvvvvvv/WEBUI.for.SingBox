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

本分支启动核心要求 sing-box 1.14.2 或更新版本。服务模式的应用更新会先完整解压并验证候选程序，再停止旧服务；准备阶段遇到损坏包或磁盘写满会保留运行中的服务。直接前台运行的交互模式在更新失败后仍可能需要手动重新启动旧程序，路由器建议使用上述服务模式。

## TUN、DNS 和固件差异

先确认核心版本能够启动，再启用透明代理。WebUI 构建成功不代表固件已提供所有内核能力：

```sh
test -c /dev/net/tun && echo 'TUN device available'
command -v nft
```

TUN 缺失时，需要与当前固件内核严格匹配的 `kmod-tun`；从固件自己的软件源安装。启用 `auto_redirect` 还需要 sing-box 所需的 nftables 支持。厂商固件的防火墙实现可能与同版本的原生 OpenWrt 不同，请按 [sing-box TUN 文档](https://sing-box.sagernet.org/configuration/inbound/tun/)检查；本项目不会默认打开 `auto_redirect` 或替你改写路由器防火墙。

路由器通常已有 dnsmasq 或 AdGuard Home 占用 53 端口。新增 DNS 入站时应使用未占用的监听地址/端口，再按网络规划配置转发或 DNS 劫持，避免直接与现有服务争抢端口。GL.iNet VPN、策略路由、其他代理程序也可能接管流量；先明确由哪一个服务管理目标流量。

WebUI 根据实际核心版本处理 TUN `stack`：1.14 保留现有配置，1.15 及以后不再输出已弃用的字段。乐观 DNS 缓存为可选项，旧配置不自动启用；开启后不能同时禁用 DNS 缓存或缓存过期。

## 备份与回滚

更新前停止服务，备份原程序和整个 `data/` 目录。需要回滚时停止新服务，恢复匹配的程序和数据备份后重新启动。包含同名节点的不同订阅现在会生成唯一标签；首次升级时核心原有的节点选择缓存可能回退到组默认项，确认一次选择即可。

本次验证包括 Windows 自动化测试、实际 sing-box 核心测试及 Linux 各目标交叉编译。尚未在 GL-MT3600BE 或其他路由器上执行硬件测试；首次部署需确认重启后自启、LAN/DNS 连通性和停止服务后的路由恢复。
