# 迁移指南

## 1. 从 rocwood / 官方 unity CLI 迁移到 upipe

### 背景

| 路径 | 是什么 | 局限 |
|---|---|---|
| 官方 `unity` CLI | 闭源单二进制（~100MB），经 Unity CDN 安装 | `pipeline install` 硬性要求 Unity 6+；国区 CDN 重定向会装到旧版 0.1.x |
| rocwood/unity-cli-2022-mod | 官方安装器的 MIT 修改版（指定版本 + 跳过校验） | 仍下载并依赖官方闭源二进制；不解决 2022 支持 |
| **unity-pipe (`upipe`)** | Go 单二进制直连 Pipeline HTTP API + 自维护 2022 适配包 | 编辑器安装/认证能力交给 Unity Hub（不冲突） |

### 命令对照

| 旧（unity CLI） | 新（upipe） |
|---|---|
| `unity pipeline list` | `upipe status` |
| `unity pipeline install` | `upipe pkg-install --source <tgz>`（配合 `go run ./package/build`） |
| `unity command`（列举） | `upipe commands` |
| `unity command <name> --param v` | `upipe exec <name> --set k=v`（值自动类型推断） |
| `unity command eval "…"` | `upipe eval "…"` |
| `unity command eval_file <f>` | `upipe eval-file <f>` |
| `unity command recompile` + 手动轮询 `recompile_status` | `upipe recompile`（自动轮询至终态） |
| `unity command run_tests --async_tests true` + 手动轮询 `test_status` | `upipe test [--mode] [--filter]`（自动轮询） |
| `unity command audit` + 手动轮询 | `upipe audit` |
| — | `upipe job <id>` / `upipe progress`（job 模式一等公民） |

行为差异注意：

1. **退出码即语义**：upipe 稳定退出码（20=命令失败等），脚本/agent 按码分支，无需 grep 输出。
2. **`--json` 单信封**：stdout 恒为一个 JSON 对象（`{ok, error?, data}`），官方 CLI 的输出格式不再相关。
3. **官方 CLI 无需卸载**：两者共存不冲突（官方 CLI 连的是同一服务）；全局规范已把 upipe 设为默认入口。

### 已装官方 CLI 的处置

`%LOCALAPPDATA%\Unity\bin\unity.exe`（~100MB）可以保留作为应急备用，也可以直接删除——upipe 不依赖它。

## 2. 已有 2022 适配安装的接管

如果你已经手动把适配过的 `com.unity.pipeline` 装进了工程（例如 SGAME 的 P4 内安装）：

```powershell
upipe pkg-status --project-path <project>          # 确认当前版本（如 0.5.0-exp.1）
upipe pkg-install --project-path <project> --source dist\com.unity.pipeline-0.5.0-2022.1.tgz --dry-run
# 预览 diff 计划（added/updated/unchanged），确认后去掉 --dry-run
```

- P4 / Perforce 工作区：文件只读时 `pkg-install` 会**拒绝执行并提示先 p4 edit/reconcile**——upipe 永不代跑 p4
- Git 工作区：直接执行，diff 计划仅覆盖变更文件并打印清单
- 工程内紧急热修：`go run ./scripts/import-from-p4 -project <工程路径>` 只读 diff 工程内包与仓库 vendored 树（M/A/D 清单 + 小型行级 diff 提示），人工 review 后把接受的变化拷回 vendored 树再 commit——**仓库永远是唯一真相源，单向分发到工程**；该工具永不调 p4、永不写盘

## 3. Harness 技能迁移

旧技能（unity CLI 语法版本）→ upipe 化版本：

```powershell
cd <unity-pipe 仓库>
upipe plugin install          # 双侧安装（~/.claude/skills + ~/.codex/skills），旧版自动备份到 skills-backup/
```

变更点速览：

- `unity-pipeline`：全部命令示例换 upipe 语法，新增退出码表（§0）与 HTTP 直连应急回退（§6）
- `unity-router` / `unity-mcp-bridge`：仅探活与 editor_status 命令替换；co-use 白名单、MCP 契约不变
- 全局规范（CLAUDE.md §13 / AGENTS.md 6.4 / UNITY-GUIDE.md）已同步切换 upipe 为默认入口
- 规则文本的仓库镜像见 `harness/rules/`（claude.md / agents.md 模板 + README 变量表）——新机器部署时贴入对应全局文档即完成整套安装；两侧规则须手动保持镜像

## 4. 未来：编辑器管理能力迁移路线（M6+ 规划）

官方 CLI 的编辑器安装/模块管理/认证能力，计划通过包装 Unity Hub CLI 增量迁移（挂 `commands` 层新模块，不动 protocol 层）：

| 官方能力 | 迁移路径 | 优先级 |
|---|---|---|
| 编辑器列表/安装/升级 | 包装 Unity Hub CLI（`--headless`）→ `upipe editor list/install` | 按需 |
| 模块管理 | 同上 | 按需 |
| Unity 账号认证 | **不迁**——agent 驱动编辑器不需要；Hub 自行管理 | — |
| daemon 常驻进程 | 未实现（纯文档构想；本地 HTTP 冷连接 <10ms，暂无需求） | 性能有需要时再评估 |

原则：upipe 的能力面始终以「agent 驱动编辑器」为中心，官方 CLI 的"全家桶"职能不追求一一对应。
