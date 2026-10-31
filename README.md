# webui.for.singbox

`webui.for.singbox` 是一个用于管理 sing-box 的浏览器 Web UI。它可以用于管理配置文件、订阅、规则集、脚本插件、定时任务，以及查看和控制运行状态。

这个分支主要面向透明代理场景：将管理界面从桌面应用形态调整为可通过浏览器访问的 Web 服务，方便部署在网关、旁路由、软路由、服务器或容器环境中，用于远程维护 sing-box 的配置和运行状态。

本项目 fork 自 [hvvvvvvv/WEBUI.for.SingBox](https://github.com/hvvvvvvv/WEBUI.for.SingBox)，最初源自 [GUI-for-Cores/GUI.for.SingBox](https://github.com/GUI-for-Cores/GUI.for.SingBox)。感谢两个上游提供的 Web 服务、原始界面、配置模型和规则集仓库等。

路由器部署请先阅读 [OpenWrt / GL.iNet 部署说明](docs/routers.md)。本分支的修复、上游移植范围及验证记录见 [集成说明](docs/upstream-integration.md)。

原版 GUI、直接上游和当前仓库的设置与操作对照见 [GUI 操作逻辑适配](docs/gui-parity.md)。

## 相比上游的主要改动

- 将应用调整为由 Go HTTP 后端提供服务的 Web UI。
- 移除了 Wails 桌面端和托盘；恢复独立「插件」页面，脚本通过 Go 后端在路由器上执行。
- 更新了项目名称、发布信息和检查更新地址，使其指向当前仓库。
- 更适合透明代理部署场景，可作为远程管理界面运行在网关、旁路由或服务器上。

## 插件

顶部「插件」入口提供网格/列表、插件中心、源码编辑、参数配置、启停、排序、检查更新与手动运行。插件源码由独立仓库 [WEBUI.for.SingBox-Plugins](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins) 管理，可单独维护更新。

1.3.0 随附 16 个适配插件：订阅命名与去重，出站链、配置覆盖、规则集引用、透明代理、独立代理端口、Hosts DNS、AdGuardHome 对接及核心 URLTest，以及出口/IP、媒体可达性和限量下载诊断。订阅插件随更新自动执行，配置生成器默认关闭或使用空规则；定时订阅处理不依赖浏览器保持打开。手动诊断无需选择订阅，也不修改节点缓存或切换全局节点。

升级时先更新主程序至 1.3.0，再更新插件。如果旧版本同时启用了多种命名插件，请先禁用多余策略；防代理重名、更好的代理名称显示和美化节点名称最多启用一个。新版本增加多行 TextArea 参数、互斥检查和独立诊断报告。

首次加载旧插件目录缓存时，会合入新版程序随附的新增插件和严格更高的 SemVer 版本，保留较新或无法比较版本的缓存条目，让新增插件离线可用；不会自动更新已安装源码。合并仍受目录容量限制，达到限制时会提示。

这是适配 WebUI 的 JavaScript 插件接口，支持订阅、配置生成、手动触发。官方桌面插件需要适配，不能直接使用依赖 Wails、窗口、Shell 或桌面存储的源码。普通钩子没有网络能力；诊断仅提供有限 GET/HEAD 和下载采样，管理员主动运行时可以访问指定内网地址，没有域名白名单隔离。Goja 不提供强内存隔离，请只安装可信脚本。用法、限额与开发接口见 [插件说明](docs/plugins.md)。

透明代理插件只生成配置，不写系统防火墙、不替换 dnsmasq；核心启动后才按配置设置自动路由和重定向。AdGuardHome、FRP 等独立服务应由 procd/systemd 管理。本轮尚未在真实路由器验证，部署前请核对固件的 TUN、转发、DNS 和防火墙条件。

## 构建

需要准备：

- Node.js
- pnpm
- Go
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

以下镜像地址对应当前 fork，需要先在该仓库成功运行发布流程；本地源码提交不会自动生成远端镜像。

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
