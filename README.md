# webui.for.singbox

`webui.for.singbox` 是一个用于管理 sing-box 的浏览器 Web UI。它可以用于管理配置文件、订阅、规则集、定时任务，以及查看和控制运行状态。

这个分支主要面向透明代理场景：将管理界面从桌面应用形态调整为可通过浏览器访问的 Web 服务，方便部署在网关、旁路由、软路由、服务器或容器环境中，用于远程维护 sing-box 的配置和运行状态。

本项目基于 [GUI-for-Cores/GUI.for.SingBox](https://github.com/GUI-for-Cores/GUI.for.SingBox) 修改而来。感谢上游项目提供的原始界面、配置模型和 sing-box 管理体验。

## 相比上游的主要改动

- 将应用调整为由 Go HTTP 后端提供服务的 Web UI。
- 移除了 Wails 桌面端、托盘和插件相关代码。
- 增加了 HTTP 和 Connect RPC 桥接服务，用于前后端交互。
- 增加了 protobuf 生成代码，包括 Go 服务端代码和前端 TypeScript 客户端代码。
- 更新了项目名称、发布信息和检查更新地址，使其指向当前仓库。
- 更适合透明代理部署场景，可作为远程管理界面运行在网关、旁路由或服务器上。

## 技术栈

- 前端：Vue 3、TypeScript、Vite、Pinia
- 后端：Go
- RPC / 代码生成：Buf、Protocol Buffers、Connect
- 运行形态：独立 Web 服务二进制文件

## 构建

需要准备：

- Node.js
- pnpm
- Go
- Buf，仅在重新生成 protobuf 代码时需要

构建前端和后端：

```bash
git clone https://github.com/hvvvvvvv/WEBUI.for.SingBox.git
cd WEBUI.for.SingBox

cd frontend
pnpm install --frozen-lockfile
pnpm build

cd ..
go build -trimpath -ldflags="-s -w" -o webui.for.singbox.server .
```

运行：

```bash
./webui.for.singbox.server --addr 0.0.0.0:9090
```

然后在浏览器打开：

```text
http://127.0.0.1:9090
```

## Docker

构建镜像：

```bash
docker build -t webui.for.singbox .
```

运行容器：

```bash
docker run --rm -p 9090:9090 -v webui-for-singbox-data:/app/data webui.for.singbox
```

## Proto 代码生成

```bash
buf generate
```

生成文件会输出到：

- `gen/`
- `frontend/gen/`

## 发布与检查更新

应用会从当前仓库检查最新 GitHub Release：

```text
https://api.github.com/repos/hvvvvvvv/WEBUI.for.SingBox/releases/latest
```

Release 资产文件名需要符合应用内检查更新的规则：

```text
webui.for.singbox-${os}-${arch}.zip
```

示例：

```text
webui.for.singbox-windows-amd64.zip
webui.for.singbox-windows-arm64.zip
webui.for.singbox-darwin-amd64.zip
webui.for.singbox-darwin-arm64.zip
webui.for.singbox-linux-amd64.zip
```

发布 `1.0.0` 版本：

```bash
git tag -a 1.0.0 -m "1.0.0"
git push origin main
git push origin 1.0.0
```

推送标签后，GitHub Actions 会构建并上传符合命名规则的发布文件。

## 许可证

本项目遵循上游项目的许可证。详情见 [LICENSE](LICENSE)。
