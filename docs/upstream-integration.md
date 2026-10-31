# 路由器兼容性与上游集成记录

## 比较基线

2026-10-31 检查的提交：

- 当前 fork：`Ghostdehole/WEBUI.for.SingBox`，`8d2dc2ee83c395d709a68a40b9ffb53e570bf8cd`。
- 直接上游：[hvvvvvvv/WEBUI.for.SingBox](https://github.com/hvvvvvvv/WEBUI.for.SingBox)，同为 `8d2dc2ee83c395d709a68a40b9ffb53e570bf8cd`，没有遗漏的新增提交。
- 最初上游：[GUI-for-Cores/GUI.for.SingBox](https://github.com/GUI-for-Cores/GUI.for.SingBox)，`f0de129e3c7ee0278eff12f13bbfa435fcc8e57d`。

两个产品在共同祖先 `d336666` 后已分别发展成 Web 服务与桌面程序。本次逐项移植适用的更新，保留 Web 服务、认证、服务管理和路由器部署方式，没有整体合并桌面分支。

## 修复审阅发现

| 问题 | 修复 |
| --- | --- |
| Windows 已退出核心被误判为存活、阻止清理/重启 | 区分不存在的 PID、临时退出竞争和无法确认身份的错误；释放进程句柄 |
| 规则集 HTTP 错误响应覆盖有效缓存 | 状态码、超时、大小和完整读取检查通过后才保存 |
| 批量订阅刷新失败时污染元数据 | 使用独立副本，脚本和缓存成功后才提交元数据 |
| 空 `default_domain_resolver` 导致配置无效 | 未选择解析器时省略字段 |
| 不同订阅同名节点互相覆盖 | 稳定、全局唯一标签，同步重写组成员及节点引用 |
| 局部修改响应吞掉其他客户端的资源事件 | 区分完整快照版本和状态高水位，补拉遗漏的完整列表 |
| 前端五字段 cron 在后端不执行 | 统一五/六字段、星期别名和周日 7；按日期跳转计算预览，避免逐秒扫描耗费路由器 CPU |
| 认证密钥读取失败时错误放行、写失败可能清空密钥 | 读取错误或内容损坏时拒绝登录，原子写入并在失败时保留旧密钥 |

## 移植与适配

下表提交均来自最初上游；相同功能在本分支按现有 Go/Connect RPC 架构适配。

| 上游提交 | 本分支处理 |
| --- | --- |
| `d822e9f` | FakeIP 不能作为 DNS final，前后端一致校验 |
| `e94de19`、`a25b403` | 应用/核心下载流式 SHA256 校验、临时文件、成功后替换 |
| `9b7a60c` | TUN stack 按实际核心版本处理，包括预发布版本 |
| `f5a8933` | UTF-8 优先，Windows 控制台/OEM 代码页回退，Unix 保持 UTF-8 |
| `1143a50` | Confirm 最终 HTML 与表格单元格 DOMPurify 净化、外链处理 |
| `bcee8fa` | 中文等 IME 输入合成期间避免更新/误提交 |
| `b80e02f` | 规则集搜索支持多关键词与忽略大小写 |
| `2b720b4` | App 直接导入组件，减少循环依赖 |
| `a7158dc` | 编辑器异步格式化不覆盖新输入，保留多选区和方向 |
| `bc52489` | 出站排序界面使用最新订阅名称 |
| `e74a2dd` | DNS 乐观缓存，包含 schema、持久化、UI 与互斥校验 |
| `62d22c1` | 适用的按钮/开关/弹窗/菜单/选择器语义及键盘操作 |
| `0bec634` | pnpm 和 Release Actions 更新 |

同步了适用的 Vue、CodeMirror、DOMPurify、构建工具等依赖，并完成类型检查和回归测试。当前前端仍为单 HTML，未移植针对人工拆分多个 chunk 的 `strictExecutionOrder` 设置。

依赖审计还修复了 `source-map-js`、测试/检查工具链及 Go `x/text`、`x/sys` 的已知漏洞。`frontend/pnpm-workspace.yaml` 将三个已核对调用方式的 lint 依赖限定到修补后的 minimatch 11.0.1，并对上游固定旧版本的包设置补丁覆盖；TypeScript ESLint 同步到支持 TypeScript 6 的版本。安装和 Docker 构建必须同时使用该配置与锁文件。

直接上游已有 sing-box 1.14 配置迁移、DNS 响应规则、HTTP 客户端、直接入站与经过认证的原生 API 就绪检查等对应能力，未重复叠加桌面实现。Wails 窗口/托盘、桌面系统代理与系统 DNS 权限、桌面插件等更新不适用于路由器 Web 服务；桌面布局和编辑器新视图未整体搬入。

## 路由器相关改进

- OpenWrt procd 参数安全引用、前台进程监管和退出宽限。
- Linux amd64、arm64、armv7、mips、mipsle 静态构建；MIPS 核心资源使用 softfloat 名称。
- 更新仓库可在构建时指定，CI 自动指向当前 fork，避免更新成另一个仓库的二进制。
- 新增 Windows/Linux CI 测试、Linux race 检测和五种架构交叉编译。
- 核心解压使用独立临时目录，失败后清理，避免路由器空间不足时留下半解压文件。
- 服务及交互模式均在请求退出旧程序之前完成更新准备，验证临时目录交接，拒绝重复更新请求；准备或助手启动失败保留旧程序运行。

具体部署步骤及能力边界见 [路由器说明](routers.md)。版本号推进至 `1.1.5`，本地提交并不等于 GitHub 已发布此版本或容器镜像。

## 验证方法

```sh
pnpm --dir frontend test
pnpm --dir frontend build
go run ./tests ./... -count=1 -timeout=180s
# 需要 C 编译器，用于支持 race 的平台
go run ./tests -race -short ./... -count=1 -timeout=180s
```

真实核心测试使用 `tests/README.md` 中的两个环境变量指定经发布摘要校验的 sing-box 1.14.0 / 1.14.2。测试使用临时目录和本地监听，不应指向生产数据目录。交叉编译只证明目标代码可构建，不能代替 OpenWrt 固件实机验证。

本次本机验证：前端 19 个文件 / 80 项测试、类型检查、生产构建、ESLint 和 oxlint 均通过；完整 Go 测试（含两个真实核心和 61 秒长流测试）通过，随后新增的更新准备失败回归也通过。`pnpm audit`（含开发依赖）和 `govulncheck` 均未报告剩余已知漏洞。protobuf lint 通过。

Linux 五个发布目标、Windows amd64/arm64/386、macOS amd64/arm64 已完成交叉编译。当前 Windows 环境没有 C 编译器，因此未本地执行 Go race；Linux race 检查已加入 CI。Docker 镜像和路由器实机测试尚未执行。
