# 测试

所有测试、测试辅助代码和测试运行配置集中在 `tests/`。

- `frontend/`：前端回归测试，按功能目录分类。
- `testdata/`：Go 回归测试，目录与生产包的相对路径对应。
- `run.go`：生成临时 Go overlay，并在原包上下文中执行测试。
- `vitest.config.ts`：限定前端测试搜索范围，复用前端依赖与源码别名。

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
pnpm --dir frontend exec oxlint ../tests/frontend ../tests/vitest.config.ts
pnpm --dir frontend exec oxfmt ../tests/frontend ../tests/vitest.config.ts
```

真实 sing-box 测试通过 `SING_BOX_114_PATH`（1.14.0，配置生成、订阅解析）和 `SINGBOX_NATIVE_TEST_BINARY`（1.14.2，原生 API 与生命周期）指定核心可执行文件，未提供时会明确跳过。路径应为绝对路径。HTTP 长流测试需要约 61 秒，`-short` 会跳过。

保留标准很严格：测试必须覆盖实际的数据损坏、并发时序、认证边界、进程或流生命周期、失败回滚，或用独立协议向量/真实核心检验兼容性。默认值、简单 getter、机械字段搬运、源码文本检查、仅验证 mock 自身行为的包装测试，以及已经被更强行为测试覆盖的重复用例均删除。不以测试数量或覆盖率作为保留理由。
