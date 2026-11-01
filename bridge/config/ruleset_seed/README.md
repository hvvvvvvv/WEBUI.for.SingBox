# 默认规则集的离线初始文件

六份公共规则用于冷启动，在线更新仍由核心每 24 小时执行。准确快照、文件大小和 SHA-256 见 [manifest.json](manifest.json)；数据来源及完整许可见 [NOTICE.md](NOTICE.md)。

`source/` 保留同一上游提交的 JSON 源文件。程序只嵌入 `.srs` 与清单，不把 JSON 加载到运行内存。发行压缩包与容器的 `licenses/ruleset-seed/` 同时提供源文件、许可和本次构建的固定源码链接。

维护时先审核固定提交及来源许可，准备包含期望 SHA-256 的清单，然后在仓库根目录运行：

```sh
node scripts/update-ruleset-seed.mjs --commit 6fc21bcb856169abdf8589e47d6ee210d95ea09d --core /path/to/sing-box --manifest bridge/config/ruleset_seed/manifest.json
```

加 `--verify-only` 可离线检查现有资源。更新器不选择 latest、不发布版本；全部下载、摘要及核心解码检查成功后才替换目录，失败保留原资源。更换数据来源时也应人工更新 NOTICE、许可证及上游构建记录。
