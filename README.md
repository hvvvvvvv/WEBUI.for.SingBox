# webui.for.singbox

`webui.for.singbox` 是一个用于管理 sing-box 的浏览器 Web UI。它可以用于管理配置文件、订阅、规则集、定时任务，以及查看和控制运行状态。

这个分支主要面向透明代理场景：将管理界面从桌面应用形态调整为可通过浏览器访问的 Web 服务，方便部署在网关、旁路由、软路由、服务器或容器环境中，用于远程维护 sing-box 的配置和运行状态。

本项目基于 [GUI-for-Cores/GUI.for.SingBox](https://github.com/GUI-for-Cores/GUI.for.SingBox) 修改而来。感谢上游项目提供的原始界面、配置模型和规则集仓库等。

## 相比上游的主要改动

- 将应用调整为由 Go HTTP 后端提供服务的 Web UI。
- 移除了 Wails 桌面端、托盘和插件相关代码。
- 更新了项目名称、发布信息和检查更新地址，使其指向当前仓库。
- 更适合透明代理部署场景，可作为远程管理界面运行在网关、旁路由或服务器上。

## 构建

需要准备：

- Node.js
- pnpm
- Go
- Buf，仅在重新生成 protobuf 代码时需要
- make。Linux/macOS 可使用系统包管理器安装；Windows 需要安装可用的 make，并确保 PowerShell 可用

构建二进制执行文件：

```bash
git clone https://github.com/hvvvvvvv/WEBUI.for.SingBox.git
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

```bash
docker pull ghcr.io/hvvvvvvv/webui.for.singbox:latest

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
  ghcr.io/hvvvvvvv/webui.for.singbox:latest
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
