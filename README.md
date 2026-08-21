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
./build/bin/webui.for.singbox.server --addr 0.0.0.0:9090
```

## 容器部署

版本发布时，GitHub Actions 会构建 `amd64`、`arm64` 和 `arm/v7` 镜像并推送到 GitHub Container Registry。稳定版本同时提供完整版本号、主次版本号、主版本号和 `latest` 标签；预发布版本只提供完整版本号和对应的提交 SHA 标签。

拉取并运行最新稳定版本：

```bash
docker pull ghcr.io/hvvvvvvv/webui.for.singbox:latest

docker run -d \
  --name webui-for-singbox \
  --restart unless-stopped \
  -p 9090:9090 \
  -v webui-for-singbox-data:/app/data \
  -e GFS_HOST=0.0.0.0 \
  -e GFS_PORT=9090 \
  ghcr.io/hvvvvvvv/webui.for.singbox:latest
```

Web UI 默认监听 `0.0.0.0:9090`，持久化数据保存在 `/app/data`。如修改 `GFS_PORT`，需要同步调整 `docker run` 的容器端口映射。需要固定版本时，可将 `latest` 替换为具体版本标签，例如 `1.1.3`。

容器包首次发布后默认为私有。仓库维护者需要在 GitHub Packages 的包设置中将其可见性改为 Public，之后用户无需登录 GHCR 即可拉取。

## 系统服务

同一二进制可以注册为 Windows、Linux 或 macOS 的系统级服务。安装和卸载服务需要管理员权限；启动、停止和重启通常也需要管理员权限。

安装服务时可以指定监听地址。该地址会写入服务启动参数；安装操作只注册服务，不会立即启动：

```bash
# Linux / macOS
sudo ./webui.for.singbox service install --addr 0.0.0.0:9090
sudo ./webui.for.singbox service start

# Windows（管理员 PowerShell）
.\webui.for.singbox.exe service install --addr 0.0.0.0:9090
.\webui.for.singbox.exe service start
```

支持的管理命令如下：

```text
webui.for.singbox service install [--addr 0.0.0.0:9090]
webui.for.singbox service uninstall
webui.for.singbox service start
webui.for.singbox service stop
webui.for.singbox service restart
webui.for.singbox service status
```

`status` 输出 `running`、`stopped` 或 `not-installed`。卸载正在运行的服务时，程序会先正常停止服务再删除注册信息。

服务直接引用执行 `install` 命令时的二进制绝对路径，并继续把二进制所在目录作为数据目录。安装后不要移动或删除二进制；如需更换位置，应先卸载，再从新位置重新安装。服务会随系统启动，并使用系统默认的高权限服务账户运行。

运行日志写入平台的系统服务日志：Windows 使用事件查看器，Linux 使用对应服务管理器的日志（systemd 环境可使用 `journalctl -u webui.for.singbox`），macOS 可查看 launchd/system log 以及 `/var/log/webui.for.singbox.*.log`。
