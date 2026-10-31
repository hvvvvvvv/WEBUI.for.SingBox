# 路由器兼容性与上游集成记录

## 比较基线

2026-10-31 检查的提交：

- 审阅开始时的 fork 基线：`Ghostdehole/WEBUI.for.SingBox`，`8d2dc2ee83c395d709a68a40b9ffb53e570bf8cd`。
- 直接上游：[hvvvvvvv/WEBUI.for.SingBox](https://github.com/hvvvvvvv/WEBUI.for.SingBox)，同为 `8d2dc2ee83c395d709a68a40b9ffb53e570bf8cd`，没有遗漏的新增提交。
- 最初上游：[GUI-for-Cores/GUI.for.SingBox](https://github.com/GUI-for-Cores/GUI.for.SingBox)，`f0de129e3c7ee0278eff12f13bbfa435fcc8e57d`。
- 官方插件参考：[GUI-for-Cores/Plugin-Hub](https://github.com/GUI-for-Cores/Plugin-Hub)，`205ecacb0742a185947252cb91e5b11541c32a94`；路由器适配源码单独维护于 [WEBUI.for.SingBox-Plugins](https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins)。

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

直接上游已有 sing-box 1.14 配置迁移、DNS 响应规则、HTTP 客户端、直接入站与经过认证的原生 API 就绪检查等对应能力，未重复叠加桌面实现。Wails 窗口/托盘、桌面系统代理与系统 DNS 权限等更新不适用于路由器 Web 服务；桌面布局和编辑器新视图未整体搬入。插件管理页面现已恢复，并使用后端 JavaScript 接口适配订阅/配置/手动触发；详见 [插件说明](plugins.md)。

## 路由器相关改进

- OpenWrt procd 参数安全引用、前台进程监管和退出宽限。
- Linux amd64、arm64、armv7、mips、mipsle 静态构建；MIPS 核心资源使用 softfloat 名称。
- 更新仓库可在构建时指定，CI 自动指向当前 fork，避免更新成另一个仓库的二进制。
- 新增 Windows/Linux CI 测试、Linux race 检测和五种架构交叉编译。
- 核心解压使用独立临时目录，失败后清理，避免路由器空间不足时留下半解压文件。
- 服务及交互模式均在请求退出旧程序之前完成更新准备，验证临时目录交接，拒绝重复更新请求；准备或助手启动失败保留旧程序运行。

具体部署步骤及能力边界见 [路由器说明](routers.md)。当前版本号为 `1.3.0`；源码提交并不等于 GitHub 已发布此版本或容器镜像。

## 1.3.0 插件路由器适配

随附目录扩展为 16 个插件：4 个订阅处理、8 个配置生成、4 个手动诊断。按真实依赖将相近需求合并：出站链管理统一中继与节点链，核心 URLTest 承担基础自动选优，当前出口的限量下载采样替代桌面批量切换和外部测速程序。AdGuardHome 仅对接现有 DNS 服务；FRP 等守护进程交给 procd/systemd，不复制桌面服务管理器。

配置生成器默认关闭或使用空规则，保留当前配置行为。透明代理插件要求显式 LAN 接口和实际出站标签，拒绝替换已有 TUN 及冲突的路由标记；只生成配置，不写系统防火墙、不替换 dnsmasq。核心启动后的自动路由/重定向仍依赖固件权限和 sing-box 实现，未经过真实路由器验证。

新增 `TextArea` 字符串设置、`manual_target: "diagnostic"` 和 `conflicts` 元数据。旧安装应先升级主程序；三种命名策略若同时启用，应先禁用多余策略再更新插件。冲突声明按双向检查，拒绝启用保存，并在同一流水线执行前拒绝冲突组合；旧存储加载仍允许管理员进入页面修复。

手动诊断不需要订阅、不写缓存，使用受限 `Plugins.fetch`（GET/HEAD）和 `Plugins.measureDownload`。整次含排队最多 20 秒、单请求 5 秒，最多 8 个 HTTP 请求；fetch 单正文 64 KiB、总正文与报告各 256 KiB；下载采样最多一次、8 MiB。接口限制同源重定向并隔离 GitHub Token、下载镜像和代理凭据错误信息。有限下载样本、网页可达性与完整带宽/媒体解锁是不同结论，报告保留截断、不完整或未知状态。

这些接口供已认证管理员主动运行可信脚本，可访问指定的内网地址，没有域名白名单隔离。Goja 的超时、栈和数据限制不是强内存隔离；没有开放 Shell、文件系统、任意请求头或桌面 API。完整用法和参数见 [插件说明](plugins.md)。

## 验证方法

```sh
pnpm --dir frontend test
pnpm --dir frontend build
go run ./tests ./... -count=1 -timeout=180s
# 需要 C 编译器，用于支持 race 的平台
go run ./tests -race -short ./... -count=1 -timeout=180s
```

真实核心测试使用 `tests/README.md` 中的两个环境变量指定经发布摘要校验的 sing-box 1.14.0 / 1.14.2。测试使用临时目录和本地监听，不应指向生产数据目录。交叉编译只证明目标代码可构建，不能代替 OpenWrt 固件实机验证。

1.1.5 基线验证：前端 19 个文件 / 80 项测试、类型检查、生产构建、ESLint 和 oxlint 均通过；完整 Go 测试（含两个真实核心和 61 秒长流测试）通过，随后新增的更新准备失败回归也通过。`pnpm audit`（含开发依赖）和 `govulncheck` 均未报告剩余已知漏洞。protobuf lint 通过。

1.2.0 插件版本本机验证：前端 23 个文件 / 93 项测试、类型检查、生产构建、ESLint 和 oxlint 通过；完整 Go 测试（含上述真实核心与长流测试）通过。新增回归验证实际 JS 执行、插件顺序与更新、超时和数据限制、失败保留旧订阅/配置、修订冲突、目录校验和离线回退。浏览器已核对独立插件页、插件中心安装和源码入口。插件仓库 Actions 使用主程序解析器检查 JS 与 SHA256；主程序 CI 包含 Linux race、符号链接/FIFO 校验与五架构构建。

1.2.1 设置适配本机验证：前端 33 个文件 / 129 项测试、类型检查、生产构建、ESLint 和 oxlint 通过；完整 Go 测试（含上述两个真实核心和长流测试）通过；两个插件目录各 4 个脚本的源码与 SHA256 校验通过。新增覆盖插件参数默认值与覆盖、并发保存/失败保留草稿、页面可见性、左右编辑面板生命周期、网络代理与令牌重定向边界、核心运行快照、RSS 取消及迟到响应、服务端路径预览。三方操作逻辑对照见 [GUI 适配说明](gui-parity.md)。

1.3.0 本机验证：前端 33 个文件 / 131 项测试、类型检查、生产构建、ESLint、oxlint，以及完整 Go 测试（含两个真实核心和长流测试）通过。插件仓库 56 项行为测试与 19 组真实 Goja 向量通过；两个 Windows 核心分别通过 7 项配置检查，Linux 专用 TUN 检查移交 Ubuntu CI，未关闭 `auto_redirect` 来绕过平台限制。新增覆盖诊断请求出口、凭据隔离、同源重定向、响应/下载预算、真实 5 秒慢响应、取消、冲突与 TextArea，以及 142 地区名称、流量提示、临时节点 ID、出站/端点依赖环和 IPv6 DNS 回环。浏览器已验证安装互斥、后端诊断报告与多行设置持久化；ARM64、ARMv7、MIPSLE softfloat 静态编译通过。CI 以对应提交的实际结果为准，未完成路由器实机验证。

此前 `3327290` 的主程序 Actions 已通过 Windows/Linux 测试、Linux race 和五架构静态构建。Linux 五个发布目标、Windows amd64/arm64/386、macOS amd64/arm64 已完成交叉编译。当前 Windows 环境没有 C 编译器，因此未本地执行 Go race；Linux race 检查已加入 CI。Docker 镜像和路由器实机测试尚未执行。
