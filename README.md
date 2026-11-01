# WEBUI.for.SingBox

面向透明代理场景的 sing-box 浏览器 Web UI，基于 GUI.for.SingBox 修改，适合部署在网关、旁路由、软路由、服务器或容器环境中进行远程管理。

本项目基于 [GUI-for-Cores/GUI.for.SingBox](https://github.com/GUI-for-Cores/GUI.for.SingBox) 的界面与配置模型移植，使用 Go HTTP 后端管理 [sing-box](https://github.com/SagerNet/sing-box)，持续吸收适用于网络设备的上游改进。

路由器部署请先阅读 [OpenWrt / GL.iNet 部署说明](docs/routers.md)。本分支的修复、上游移植范围及验证记录见 [集成说明](docs/upstream-integration.md)。

原版 GUI、直接上游和当前仓库的设置与操作对照见 [GUI 操作逻辑适配](docs/gui-parity.md)。

主程序正式版版本号跟随 `GUI-for-Cores/GUI.for.SingBox` 最新稳定版（当前为 `1.28.1`），发布流程会核对官方稳定版，阻止版本号不一致的发布。功能仍以本仓库说明为准；sing-box 核心和独立插件库分别维护版本。

下载安装包见 [最新正式版](https://github.com/Ghostdehole/WEBUI.for.SingBox/releases/latest)，本次变更与同版本更新方法见 [1.28.1 发布说明](docs/releases/1.28.1.md)。主程序内置默认六个规则集的离线初始文件，发行物附有对应来源、源码和许可；核心及插件按需单独安装。升级前备份原程序和整个 `data/` 目录。

## 相比上游的主要改动

- 将应用调整为由 Go HTTP 后端提供服务的 Web UI。
- 移除了 Wails 桌面端和托盘；恢复独立「插件」页面，脚本通过 Go 后端在路由器上执行。
- 更新了项目名称、发布信息和检查更新地址，使其指向当前仓库。
- 更适合透明代理部署场景，可作为远程管理界面运行在网关、旁路由或服务器上。
- 默认六个规则集无需首次联网下载即可加载，之后仍由核心每 24 小时更新；更新失败保留已有规则。升级后旧配置中的标准默认来源也自动适配，明确设置的自定义来源和初始路径保持不变。见 [规则集启动保护](docs/gui-parity.md#首次启动不依赖规则下载)。
- 独立「安全」设置：访问密码、可选 TOTP 两步验证和一次性恢复码；登录与设置支持深浅主题和窄屏。使用与恢复见 [认证说明](docs/auth.md)。
- 恢复官方规则类型与动作控件、首页临时核心设置及通用日志控制；新浏览器默认显示概览、配置、订阅、插件，已有偏好保持不变，背景图片仅保存在当前浏览器。具体行为与验证边界见 [GUI 操作逻辑适配](docs/gui-parity.md)。

## 插件

顶部「插件」入口提供网格/列表、插件中心、源码编辑、参数配置、启停、排序、检查更新与手动运行。[WEBUI.for.SingBox-Plugins](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins) 是插件中心唯一的发行来源，独立维护插件清单与源码。主程序只提供下载、管理和执行接口，不包含或随二进制发行插件源码。

独立插件仓库当前维护 14 个职责分离的插件，六个旧独立包已从目录和源码发行中移除。订阅节点整理统一命名、去重与名称/协议筛选；网络诊断统一出口、IP 与站点检查；DNS 分流和客户端分流各自负责已有服务/出站的策略引用。其他插件负责透明代理入站、代理端口、Hosts、AdGuardHome 对接、规则集、出站链、URLTest、配置覆盖、媒体检查和限量测速。用户已安装的脚本不会自动删除或替换，迁移方法见 [插件说明](docs/plugins.md)。

升级时先更新主程序，再从插件中心选择所需插件。中心仅获取元数据，点击加号才下载并安装所选脚本；不再提供兼容区或旧包更新，已缓存的旧条目也会过滤。迁移时先导出参数并停用旧项，再安装归并后的插件并核对设置；互斥检查会阻止新旧插件同时处理同一职责，已有源码、参数和启停状态不会自动改变。

打开插件中心时，已有有效目录缓存就先显示缓存；没有缓存时只获取独立仓库的 `index.json` 元数据列表。「检查更新」也只刷新列表，不下载 JS、不安装插件。点击某个插件的「安装」或「更新」，才下载该插件源码，通过 SHA256 和语法校验后保存。列表获取失败时保留有效缓存；无缓存则显示空目录及明确警告。

已安装插件保存在设备上，可离线执行；目录缓存只能离线浏览，安装或更新仍需联网下载所选源码。旧版含源码的目录缓存不会被用作当前元数据缓存，升级后会重新获取列表，不改变已安装插件。主程序升级或目录刷新均不会自动替换已安装源码。

这是适配 WebUI 的 JavaScript 插件接口，支持订阅、配置生成、手动触发。官方桌面插件需要适配，不能直接使用依赖 Wails、窗口、Shell 或桌面存储的源码。普通钩子没有网络能力；诊断仅提供有限 GET/HEAD 和下载采样，管理员主动运行时可以访问指定内网地址，没有域名白名单隔离。Goja 不提供强内存隔离，请只安装可信脚本。用法、限额与开发接口见 [插件说明](docs/plugins.md)。

透明代理插件只生成配置，不写系统防火墙、不替换 dnsmasq；核心启动后才按配置设置自动路由和重定向。AdGuardHome、FRP 等独立服务应由 procd/systemd 管理。已通过 OpenWrt ARM64 虚拟机及隔离 DNS/TUN 验证，尚未验证 GL-MT3600BE 实机固件；部署前请核对 TUN、转发、DNS 和防火墙条件。

## 构建

需要准备：

- Node.js 24
- pnpm
- Go 1.26（以 `go.mod` 为准）
- Buf，仅在重新生成 protobuf 代码时需要
- make。Linux/macOS 可使用系统包管理器安装；Windows 需要安装可用的 make，并确保 PowerShell 可用

构建二进制执行文件：

```bash
git clone https://github.com/Ghostdehole/WEBUI.for.SingBox.git
cd WEBUI.for.SingBox

make build
```

运行：

```bash
./build/bin/webui.for.singbox.server --addr 0.0.0.0:9090 --log-level info --log-days 7
```

## 手动安装 sing-box core

如果运行环境无法访问 GitHub，因而不能在 Web UI 中在线下载 core，可以先通过其他可用网络获取与运行平台、CPU 架构相匹配的 sing-box 发布包，解压后将其中的 core 可执行文件手动放到 WebUI 可执行文件所在目录下的 `data/sing-box` 目录中（目录不存在时请自行创建）。

文件名必须符合以下约定：

- 稳定版：`data/sing-box/sing-box`，Windows 下为 `data/sing-box/sing-box.exe`
- 测试版（Alpha）：`data/sing-box/sing-box-latest`，Windows 下为 `data/sing-box/sing-box-latest.exe`

Linux 和 macOS 用户还需为文件添加执行权限：

```bash
# 稳定版
chmod +x data/sing-box/sing-box

# 测试版（Alpha）
chmod +x data/sing-box/sing-box-latest
```

如果使用容器部署，对应的容器内目录为 `/app/data/sing-box`；使用上述示例中的命名卷时，需要将 core 放入该卷对应的目录。替换已有 core 前请先停止正在运行的 core，放置完成后刷新 Web UI 或重启服务即可识别本地版本。

## 容器部署

拉取并运行最新稳定版本：

镜像由正式版发布流程构建，`latest` 对应最新稳定发布；也可使用明确的版本标签。

```bash
docker pull ghcr.io/ghostdehole/webui.for.singbox:latest

docker run -d \
  --name webui-for-singbox \
  --restart unless-stopped \
  --cap-add NET_ADMIN \
  --device /dev/net/tun:/dev/net/tun \
  --sysctl net.ipv4.ip_forward=1 \
  --sysctl net.ipv6.conf.all.forwarding=1 \
  -p 9090:9090 \
  -v webui-for-singbox-data:/app/data \
  -e GFS_HOST=0.0.0.0 \
  -e GFS_PORT=9090 \
  ghcr.io/ghostdehole/webui.for.singbox:latest
```

上面的容器示例将流量限定在容器网络空间中，发布 `9090` 只暴露管理页面，并不会自动接管宿主或 LAN 客户端流量。需要整网透明代理时，应明确将流量送入容器，或在隔离评估后使用 Linux host 网络；两者的路由、防火墙和端口影响不同。部署角色与检查项见 [场景兼容性表](docs/routers.md#部署角色)。可以在镜像名后追加 `--log-days 0` 等启动参数；`GFS_HOST` 支持 IPv4 和 IPv6 字面量。

## 系统服务

同一二进制可以注册为 Windows、Linux 或 macOS 的系统级服务。安装和卸载服务需要管理员权限；启动、停止和重启通常也需要管理员权限。

安装服务时可以指定监听地址、后端日志级别和日志文件保留天数。这些值会写入服务启动参数；安装操作只注册服务，不会立即启动：

```bash
# Linux / macOS
sudo ./webui.for.singbox service install --addr 0.0.0.0:9090 --log-level info --log-days 7
sudo ./webui.for.singbox service start

# Windows（管理员 PowerShell）
.\webui.for.singbox.exe service install --addr 0.0.0.0:9090 --log-level info --log-days 7
.\webui.for.singbox.exe service start
```

支持的管理命令如下：

```text
webui.for.singbox service install [--addr 0.0.0.0:9090] [--log-level info] [--log-days 7]
webui.for.singbox service uninstall
webui.for.singbox service start
webui.for.singbox service stop
webui.for.singbox service restart
webui.for.singbox service status
```

`status` 输出 `running`、`stopped` 或 `not-installed`。卸载正在运行的服务时，程序会先正常停止服务再删除注册信息。

`--log-level` 支持 `debug`、`info`、`warn` 和 `error`，默认为 `info`。该参数采用最低级别门槛语义，并在进程启动后保持不变。修改已安装服务的日志级别时，需要重新安装服务：

```bash
webui.for.singbox service uninstall
webui.for.singbox service install --log-level warn
```

`--log-days` 指定应用日志文件保留的本地自然日数量，默认为 `7`。例如 `7` 表示保留当天及之前 6 天；设置为 `0` 或负数会关闭日志文件的创建、滚动和清理，但不会删除已有文件。修改已安装服务的保留天数同样需要卸载后重新安装。

服务直接引用执行 `install` 命令时的二进制绝对路径，并继续把二进制所在目录作为数据目录。安装后不要移动或删除二进制；如需更换位置，应先卸载，再从新位置重新安装。服务会随系统启动，并使用系统默认的高权限服务账户运行。

应用日志始终以单行、无颜色格式写入标准输出。启用文件日志时，相同内容会同时追加到二进制所在目录的 `data/logs/app/yyyy-MM-dd.log`，并在启动及跨日后的首次写入时清理过期日期文件。文件日志发生创建、写入或清理错误时，程序会继续运行并保留标准输出。
