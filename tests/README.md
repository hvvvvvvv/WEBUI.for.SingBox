# 测试

所有测试、测试辅助代码和测试运行配置集中在 `tests/`。

- `frontend/`：前端回归测试，按功能目录分类。
- `testdata/`：Go 回归测试，目录与生产包的相对路径对应。
- `run.go`：生成临时 Go overlay，并在原包上下文中执行测试。
- `vitest.config.mts`：限定前端测试搜索范围，复用前端依赖与源码别名，显式使用 ESM 配置。

Go 会跳过 `testdata` 目录。overlay 让测试继续访问包内接口、使用原包的工作目录和平台构建约束，无需给生产代码增加测试专用导出，也不会把文件复制回生产目录。直接执行 `go test ./...` 不会加载这些测试，请使用下面的入口。

在仓库根目录运行：

```bash
make test           # Go race 检测 + 前端测试
make test-go        # 仅 Go
make test-frontend  # 仅前端

# 传递原生 go test 参数，指定包、用例或快速运行
go run ./tests -race ./bridge/kernel -run TestRestart -count=1
go run ./tests -short ./... -count=1

# 前端测试已移出 frontend/src，单独检查与格式化
frontend/node_modules/.bin/oxlint tests/frontend tests/vitest.config.mts
frontend/node_modules/.bin/oxfmt tests/frontend tests/vitest.config.mts
```

真实 sing-box 测试使用下列环境变量，路径应为绝对路径；未设置时明确跳过：

- `SING_BOX_114_PATH`：1.14.0 历史兼容向量（配置生成、订阅解析）。
- `SING_BOX_TEST_BINARY`：默认配置、六个规则集的本地下载/缓存、规则/全局路径及通用设置。
- `SINGBOX_NATIVE_TEST_BINARY`：原生 API、实际启动/停止生命周期及 DNS 乐观缓存；校验实际版本，不硬编码旧版本。
- `SING_BOX_COMPAT_BINARY`：按已安装版本适配 TUN 字段，以 `check` 验证三个 DNS 模式，不启动 TUN。

CI 的 `core-compatibility` 分别下载并校验 1.14.4 与 1.15.0-alpha.13，将后三个变量指向对应核心，执行配置、核心及 HTTP 包的完整测试。监听、代理、DNS 和规则集服务均使用临时目录/本地测试端点；真实 TUN 测试另在独立网络命名空间中执行。不要将测试指向生产数据目录。HTTP 长流测试需要约 61 秒，`-short` 会跳过。

实际插件源码只在独立的 `Ghostdehole/WEBUI.for.SingBox-Plugins` 仓库维护。设置 `WEBUI_PLUGIN_REPO` 为该仓库本地检出的绝对路径，可以运行 `TestExternal*` 的命名、去重和订阅节点 ID 集成测试；未设置时明确跳过，指定无效目录则失败。默认插件引擎测试使用最小合成脚本，目录下载测试使用本地 HTTP 服务，不访问公网。主程序 CI 将独立仓库检出到临时 `.plugin-tests/` 后设置此变量；该目录不提交、不嵌入程序，也不进入 Docker 构建上下文。

保留标准很严格：测试必须覆盖实际的数据损坏、并发时序、认证边界、进程或流生命周期、失败回滚，或用独立协议向量/真实核心检验兼容性。默认值、简单 getter、机械字段搬运、源码文本检查、仅验证 mock 自身行为的包装测试，以及已经被更强行为测试覆盖的重复用例均删除。不以测试数量或覆盖率作为保留理由。
