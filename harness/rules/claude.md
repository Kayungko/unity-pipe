# CLAUDE.md §13 模板 — Unity CLI / Pipeline 操作规范

> 贴入 `~/.claude/CLAUDE.md`，作为其 §13（或新增章节）。替换 `{{...}}` 占位符（见本目录 [README.md](README.md)）。

```markdown
## 13. Unity CLI / Pipeline 操作规范

进行 Unity 开发时，默认通过自维护的 `upipe` CLI（github.com/Kayungko/unity-pipe 私有仓，本地 `{{UNITY_PIPE_REPO}}`，二进制在 `{{UPIPE_BIN}}`）驱动编辑器内 `com.unity.pipeline` 包，使用 `unity-pipeline` 技能。官方 `unity` CLI 若在 PATH 可兼容使用，但不是依赖。

例外（co-use）：由全局路径白名单触发，不依赖也不要求工程内任何声明文件（禁止为启用 co-use 而创建或修改工程文档）。当前白名单：`{{CO_USE_WHITELIST}}`（depot `{{CO_USE_DEPOT}}`）。白名单内工程：Pipeline 为主信道与序列化资产唯一默认写信道；Unity MCP 仅限只读证据采集、AI 资产生成、Roslyn 增量改码，以及工程台账登记的业务工具（如 `Assets/Editor/Codex/TOOLS.md`——只读引用，禁止改写）。MCP 写工具默认 dry_run/preview；MCP 工具永不提交 P4。白名单外工程一律 pipeline-only，不使用 Unity MCP。

- Unity 操作规范统一遵循全局版 `{{UNITY_GUIDE}}`（harness 配置不落工程，工程根不再放 UNITY-GUIDE.md）。
- 场景、Prefab、Material、ScriptableObject、`.meta` 等序列化资产禁止手动创建、修改或删除，必须通过 Pipeline 命令（co-use 下此红线不变）。
- 可用命令以当前实例的 `upipe commands`（`GET /api/commands` 动态自发现）为准，不假定 Pipeline 能力。
- upipe 的退出码与 `--json` 单信封契约见 `unity-pipeline` 技能 §0：agent 按退出码分支（10 无实例 / 11 重试耗尽 / 12 认证 / 13 busy / 20 命令失败…），不解析文本。
- 适配包安装/升级：`upipe pkg-install --source <tgz>`（仓库 `go run ./package/build` 产出）；对 P4 只读工作区会拒绝并提示 p4 操作，upipe 永不代跑 p4。upipe 缺失时回退官方 unity CLI 或 HTTP 直连（端口文件 + Bearer，详见 docs/PROTOCOL.md）。
```
