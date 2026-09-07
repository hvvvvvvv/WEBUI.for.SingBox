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

## Core 生命周期与日志

Core 由后端独占管理，不再接管已经运行的 core。每个 core 使用同一后端二进制的隐藏 `__core_guard` 子命令作为守护进程，负责启动前检查、进程控制和 stdout/stderr 采集；它没有网络监听接口。界面、状态和内存监控中的 PID 是实际 core PID，不是守护进程 PID。

正常退出、系统服务停止和自更新都会先禁止新的 core 启动/重启并停止调度，再停止 core、收完尾日志，最后关闭其他资源。Core 收到平台退出信号后最多等待 10 秒，随后强制终止并最多等待 5 秒确认，应用整体关闭预算为 20 秒。后端崩溃或被单独强制结束时，守护进程通过控制管道断开检测退出并立即终止 core；日志通道阻塞或断开不会阻止这一清理。正常关闭会保存未换行的尾日志，强制结束后端不保证尚未送达后端的日志落盘。

同一数据目录只允许一个后端运行。`data/backend.lock` 和 `data/sing-box/core.lock` 是操作系统锁文件，不要在进程运行时删除它们；锁会随进程退出自动释放。启动时最多等待上一守护进程清理 15 秒。进程归属记录位于 `data/sing-box/process.json`，包含会话、PID、创建时间、可执行路径及后端/守护身份。旧版 `pid.txt` 只有在可执行文件属于当前 core 目录、参数指向当前生成配置且没有活跃管理后端时才会用于停止遗留进程。发现 PID 复用、其他目录进程、身份不符或无法核验时，会拒绝启动并提示人工检查，不会向未知进程发送终止信号。

自更新先启动独立更新 helper，再进入统一关闭流程。Helper 确认旧后端、core 和守护进程均已退出后才替换文件，确认失败会中止更新。更新后重新读取 YAML，**仅按 `autoStartKernel` 决定是否启动 core**，不会额外恢复更新前的运行状态。

### Core 日志保留配置

在“全局设置 → 高级设置 → Core 日志保留天数”编辑并保存，或修改 `<程序目录>/data/config.yaml` 顶层配置：

```yaml
coreLogDays: 0
```

- 默认 `0`：不创建、写入、滚动或清理 core 日志文件，保留已有文件；仍持续读取 core 输出并提供现有前端实时日志。
- 正整数 `N`：保留今天及之前 `N-1` 个本地自然日。例如 `7` 包含今天和此前 6 天，`1` 仅保留今天。
- 合法范围为 `0` 至 `2147483647`，负数、小数、字符串和越界值无效。非法 YAML 启动时报错，非法保存请求不会修改已存配置。旧 YAML 缺少该字段时补齐为 `0`。
- 前端保存后立即生效，不重启 core，也不标记需要重启；main 和 alpha 共用此配置。手动编辑 YAML 下次启动生效，不监听文件变化。
- 改为 `0` 立即关闭文件并停止清理；重新启用仅保存之后的新日志，不补录历史。

文件路径为 `<程序目录>/data/logs/core/yyyy-MM-dd.log`，日期取守护进程接收输出时的本地日期，重启后追加写入。启用、正数保留天数改变及跨日首条日志到达时清理过期文件；只有收到日志才创建当天文件，午夜不会创建空文件。仅清理严格匹配有效日期名称的过期普通 `.log` 文件，保留未来日期、其他名称、目录和符号链接。目录权限为 `0755`，文件权限为 `0644`（实际权限仍受系统权限机制约束）。

Core 启动检查、启动、运行和退出输出直接来自合并后的 stdout/stderr，不增加后端 `/logs` 订阅；现有前端实时日志功能不变。文件内容采用统一单行可读格式：

```text
2026-09-06T14:25:36.218 CST INFO  component=core operation=startup msg="INFO[0000] sing-box started (0.428s)" profile_id="default" pid=1234
2026-09-06T14:25:37.105 CST DEBUG component=core operation=runtime msg="DEBUG[0001] outbound connection established" profile_id="default" pid=1234
```

`operation` 为 `check / startup / runtime / shutdown`。时间精确到毫秒并附本地时区标识；消息保留原文、去除 ANSI 控制序列并转义引号/换行，支持超过 64 KiB 的行，未知级别为 `UNKNOWN`。保存 core 已输出的全部级别，不受后端 `--log-level` 影响，也不改变 Profile 自身的日志设置。Core 内容只写入 core 文件，不混入应用 stdout 或 app 日志；不做脱敏，因此启用前请注意磁盘访问权限。

创建或写入失败不会影响 core 运行：经应用 Logger 报告一次错误，后续日志最多每分钟触发一次重试，恢复后报告一次成功；清理和关闭失败报告警告。Core 文件不支持历史浏览、下载、压缩或按大小滚动。

### 生命周期回归测试

```bash
go test ./...
go vet ./...
go test -race ./bridge/platform ./bridge/storage ./bridge/logging ./bridge/config ./bridge/kernel ./bridge/appupdate ./bridge
pnpm --dir frontend test
pnpm --dir frontend type-check
pnpm --dir frontend build-only
```

`.github/workflows/core-lifecycle.yml` 在 Linux、Windows 和 macOS 原生环境执行真实子进程生命周期测试。跨平台编译只能验证编译兼容性，不能代替对应系统的退出行为验收。
