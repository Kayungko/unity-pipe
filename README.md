# unity-pipe

自维护的 Unity Pipeline 薄客户端（`upipe`）+ Unity 2022 适配版 `com.unity.pipeline` 包分发 + AI harness 插件包。

**不依赖** Unity 官方闭源 `unity` CLI 二进制，也不依赖任何第三方安装仓库。所有能力直接来自编辑器内 `com.unity.pipeline` 包的 HTTP API：

- 端口文件：`<project>/Library/Pipeline/.unity-pipeline-port`（port + evalToken，域重载后重写）
- 认证：`Authorization: Bearer <evalToken>`
- 命令面：`GET /api/commands` **动态自发现**——包升级新增命令，upipe 零改动可用
- 执行：`POST /api/exec`（含分离式 job 模式）+ 轮询端点

## 构建

```powershell
go build -o dist/upipe.exe ./cmd/upipe
```

纯 Go 标准库，无第三方依赖；`go test ./...` 运行全部测试（协议单元 + httptest mock 端到端 + 分层 lint）。

## 快速上手

```powershell
# 探活（端口文件 + /api/status + editor_status）
upipe status --project-path E:\path\to\project

# 列出编辑器暴露的全部命令（动态自发现）
upipe commands --project-path E:\path\to\project

# 执行任意命令（--set 值自动做 JSON 标量类型推断）
upipe exec set_autotick --set enable=true
upipe exec eval --set "code=return 2 + 2;"
```

省略 `--project-path` 时从当前目录向上寻找有端口文件的工程。

## 退出码契约（agent 分支依据）

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 2 | 用法错误 |
| 10 | 未发现运行中的实例 |
| 11 | 域重载重试梯耗尽仍不可达 |
| 12 | 认证失败（重启编辑器刷新 token） |
| 13 | busy / settling / job 队列满（可重试） |
| 20 | 命令执行失败（含测试失败） |
| 21 | 超时 |
| 22 | job 已取消 |
| 30 | 协议不兼容（包版本过旧/过新） |
| 50 | 内部错误 |

`--json` 时 stdout 恒为单一信封：`{"ok": bool, "error": {code, kind, message, retryable, data}?, "data": ...}`，人类可读信息全走 stderr。

## 架构

```
cli → commands → protocol ← transport
```

- `internal/protocol` — 纯逻辑（数据模型/端点封装/发现/重试梯/轮询状态机），零 IO，可机械移植
- `internal/transport` — net/http 实现，唯一 socket IO 点，keep-alive 连接复用
- `internal/commands` — 子命令的"等待策略"封装
- `internal/cli` — 参数分发 + 信封渲染 + 退出码

分层由 `layering_test.go`（go/parser import lint）强制执行。

## 目录

- `package/` — Unity 2022 适配版 com.unity.pipeline 自维护分发（vendored 唯一真相源 + upstream 基线 + 补丁快照）
- `harness/` — AI harness 插件包（unity-pipeline / unity-router / unity-mcp-bridge 技能 + 安装器）
- `docs/` — 协议实测文档、2022 适配记录、迁移路径

## 适配包与插件包

```powershell
# 查询工程已装的 pipeline 包版本
upipe pkg-status --project-path E:\path\to\project

# 安装/升级适配包（diff 计划，先 --dry-run 预览；P4 只读工作区会拒绝并提示 p4 操作）
upipe pkg-install --project-path E:\path\to\project --source dist\com.unity.pipeline-0.5.0-2022.1.tgz

# 安装 harness 技能到 AI Mana（~/.claude/skills）与 Codex（~/.codex/skills）
upipe plugin install          # 在仓库目录内执行；旧技能自动备份并 SHA256 校验
```

适配包版本 `0.5.0-2022.1` 高于官方 `0.5.0-exp.1`（semver 预发布段排序），UPM 不会用 registry 版本"覆盖升级"。适配细节见 `package/ADAPTATION-2022.md`。

## 许可

- upipe 代码：MIT
- `package/com.unity.pipeline`：Unity Technologies 原包（内部 git revision `17df0ac8`），仅私有内部使用，**禁止将本仓库转为 public**。详见 LICENSE-NOTES.md
