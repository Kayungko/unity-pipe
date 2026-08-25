---
name: unity-mcp-bridge
description: Unity MCP 执行契约。使用工程登记的 MCP 业务工具/read_console 与 manage_scene 结构化证据/AI 资产生成（generate_image、generate_model、generate_audio、import_model）/manage_script Roslyn 增量改码/manage_profiler 时使用；含 Bridge 启动、双 harness 接入步骤与写行为契约。Do NOT use：序列化资产默认写、编译-测试主循环、烘焙、构建（走 unity-pipeline）；不拉起第二个 8080 hub；工程不在 co-use 白名单时不使用。
---

# Unity MCP Bridge（执行契约）

仅当当前工程命中全局路径白名单（co-use，见 CLAUDE.md §13 / AGENTS.md 6.4，如 `E:\P4\SGAME_*`）时适用；不依赖也不要求工程内声明文件。

## 1. 前置与启动

- 启动入口用工程登记的菜单/脚本（如 SGAME「策划自用/Codex/启动 Unity MCP Bridge」）；Auto-Start 默认关。
- 8080 是 uvx 拉起的单实例 python 进程：**禁止起第二个 hub**。
- 域重载后按 0/1/3/5/10/30s 重连梯恢复；窗口内命令会丢，需重试。

## 2. Codex 接入

`~/.codex/config.toml` 的 `[mcp_servers.*]` HTTP 条目（user-local，不得提交 P4）。原生 MCP 工具直接调用。

## 3. AI Mana 接入

AI Mana 无静态 MCP 注入点时，走 HTTP 直连 `http://127.0.0.1:8080/mcp` 的 streamable JSON-RPC：
initialize → tools/list → tools/call（PowerShell/curl POST）。健康探测轮询 `/mcp`；
不可达按 unity-router 降级链处理，不自起 hub；hub 崩溃时通知用户而非静默绕过。
（SGAME 2026-08-20 实测：initialize 返回 Mcp-Session-Id 与 capabilities，tools/list 正常。）

## 4. 能力概览

35 内建工具 + 19 资源；工程业务工具以工程台账为准（如 `Assets/Editor/Codex/TOOLS.md`），不复述清单，保持薄。

## 5. 写行为契约

- 只读默认；写工具先 dry_run/preview，需具体目标参数 + 用户指令方可应用。
- 序列化写入仅限台账登记条目，默认 dry_run。
- 永不提交 P4；不从 MCP 工具更新飞书/云端文档。
- MCP 派发器无 settle 门：冷导入 busy 时轮询 `upipe exec editor_status` 至 ready（status 字段）再重试。

## 6. 域重载互斥

apply_text_edits/recompile 会清掉 pipeline 在途队列；代码修改权单占，改完用 `upipe exec editor_status` 等双信道就绪再继续。

## 7. 禁令

不起第二个 8080 hub；不用未确认在线的其他 REST 后端；不手改序列化 YAML。
