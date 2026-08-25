---
name: unity-router
description: Unity 编辑器操作路由与信道选择。操作Unity/改 prefab 或场景/跑测试/Unity自动化/烘焙但不确定走 pipeline 还是 unity-mcp 时使用；只做适用性检查、探活、分工判定与降级链，不直接操作编辑器。Do NOT use：已明确单一信道时（直接用 unity-pipeline 或 unity-mcp-bridge）。
---

# Unity Router（信道路由）

只做适用性检查、探活、分工判定与降级链，不直接操作编辑器。执行入口为全局技能
`unity-pipeline`（Pipeline 信道）与 `unity-mcp-bridge`（MCP 信道）。

## 0. 工程适用性检查（先于一切）

co-use 由全局路径白名单触发（定义在 CLAUDE.md §13 / AGENTS.md 6.4），不读、不要求、不创建工程内声明文件：

- 当前工程路径命中白名单（如 `E:\P4\SGAME_*`）→ 进入 §1 以下路由。
- 未命中 → **仅 pipeline**，不使用 Unity MCP，本技能其余各节不适用。

## 1. 信道探活

- Pipeline：`upipe status`（合并实例发现 + /api/status + editor_status；多实例时用 `--project-path` 显式指定）。端口描述符在 `Library/Pipeline/.unity-pipeline-port`。
- MCP：HTTP 探 `http://127.0.0.1:8080/mcp`（initialize，端口以工程配置为准）。hub 未运行时用工程登记的启动入口；禁止各自 uvx 起第二个 hub。
- 其他 Unity REST 后端（如 8090 类）：未确认在线不探测、不使用。

## 2. 能力分工表

| 场景 | 信道 |
|---|---|
| 序列化资产写（scene/prefab/material/.meta）、Assets 下移动/重命名/导入 | Pipeline 唯一 |
| 编译→测试主循环、烘焙、构建、热重载、无人值守长任务、Player 面、ProjectSettings | Pipeline |
| 工程登记的 MCP 业务工具（台账，如 `Assets/Editor/Codex/TOOLS.md`） | MCP（写类默认 dry_run） |
| AI 资产生成、Profiler、Roslyn 增量改码 | MCP（代码修改权单占） |
| 结构化 Editor 证据（read_console/manage_scene/resource 面） | MCP 只读优先 |
| 通用只读查询/截图/状态 | 默认 Pipeline；需结构化 JSON 时改 MCP |
| 重叠 CRUD（gameobject/scene 等） | 归当前任务主线一方独占，另一方只读 |

## 3. 写权限不变式

- 序列化资产默认写信道仅 Pipeline（红线，co-use 下不变）。
- MCP 写工具仅限工程台账逐条登记，默认 dry_run/preview，确认后方可应用。
- 代码/脚本修改权同一时间由一方单占。
- 两侧均不提交 P4；p4 操作走 p4-ops.js 或人工专责。

## 4. 域重载协同

任一侧改码后：`upipe exec editor_status` 轮询至 ready（status 字段），并等 MCP 重连（0/1/3/5/10/30s 梯）再继续；重载瞬间两侧在途命令均丢，需重试。写密集阶段避免人工 Ctrl+Z（全局唯一 Undo 栈混排两面操作）。

## 5. 降级链

1. MCP 不可达 → Pipeline 等价能力；无等价则只读/停摆。
2. Pipeline 不可达 → MCP 仅承接已登记只读证据采集；**一切序列化写入停摆**，直至 Pipeline 恢复。
3. 两信道均不可达 → 仅纯文本编辑，并明确声明验证缺口。

## 6. 导航

工程自有 spec/台账体系按其治理文档导航；本技能不复述工程清单，执行技能不回引本 router。
