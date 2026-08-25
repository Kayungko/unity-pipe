# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

## [0.1.3] - 2026-08-25

### Added

- **`upipe plugin install --apply-rules`**：装技能的同时把路由规则模板自动追加进全局 harness 文档（`~/.claude/CLAUDE.md` / `~/.codex/AGENTS.md`），替代此前"AI 读懂 README 再手动贴模板"的非确定性步骤。本机路径占位符自动填充；`--co-use-whitelist` / `--co-use-depot` 可选参数缺省时写入 pipeline-only 安全默认；追加内容用 `unity-pipe:rules` 注释标记包裹，重复执行幂等跳过；已有文档改前自动备份（`.upipe-bak-<时间戳>`）。
- README 安装提示词第 3/4 步合并为一条 `--apply-rules` 命令；手动安装与 `harness/rules/README.md` 同步更新。

## [0.1.2] - 2026-08-25

### Added

- **全局路由规则模板**（`harness/rules/`）：`claude.md` / `agents.md` 两份可直接粘贴的规则段（对应 `~/.claude/CLAUDE.md §13` 与 `~/.codex/AGENTS.md 6.4`），关闭「skills 装了但规则没落地」的部署缺口；`plugin install` 完成后输出 rules 模板路径与粘贴指引。
- `docs/ARCHITECTURE.md`：架构内容从 README 拆出独立成篇。

### Changed

- **README 重构为新手手册**：价值主张先行、Release 优先安装、新增「和你的 AI 对话」示例与术语表；抽象链路图替换为一段可直接复制的 **AI 安装提示词**，手动安装降级为故障排查细节；冒烟测试提示词与对话示例可视化为可复制块。
- 视觉标识刷新：SVG logo 与自定义徽章条（亮/暗双主题）、静态 SVG 架构图（去掉 GitHub mermaid 视图外壳）。

### Fixed

- `test` 判定改按 summary 计数器（`passed`/`failed`/`skipped`）裁决异步测试结果，不再依赖部分场景缺失的 `success` 布尔字段——此前通过的测试可能被误判为失败。

### Added

- 上述误判场景的回归测试（httptest mock 端到端）。
- `docs/PROTOCOL.md` 补充协议陷阱：`run_tests` 响应 #5 缺 `success` 字段、#6 新建 `.cs` 需先 `AssetDatabase.Refresh()` 触发导入。

## [0.1.0] - 2026-08-25

首个可用版本：Go 薄客户端 CLI + Unity 2022 适配包分发 + 双 harness 技能包。

### Added

- **CLI 核心**（`upipe`）：`status` / `commands` / `exec` 直通编辑器 `com.unity.pipeline` HTTP API；纯 Go 标准库，零第三方依赖，单二进制。
- **完整命令面**：`eval` / `eval-file` / `recompile` / `test` / `audit` 异步轮询封装，`job` 分离模式，`progress`，`pkg-status` / `pkg-install`（diff 计划安装，P4 只读目标拒绝），`plugin install`。
- **退出码契约**：10/11/12/13/20/21/22/30/50 稳定语义，`--json` 单信封 `{"ok", "error", "data"}`，stdout 恒可 `| jq`。
- **域重载跟随**：重试梯每轮重读端口文件，自动跟随编辑器重载后的端口/token 重写。
- **Unity 2022 适配包** `0.5.0-2022.1`：基于官方 `0.5.0-exp.1`（upstream revision `17df0ac8`），9 文件适配 2022.3；semver 预发布段 `2022.1 > exp.1` 防 registry 误覆盖；`go run ./package/build` 产出 tgz + SHA256。
- **Harness 插件包**：`unity-pipeline` / `unity-router` / `unity-mcp-bridge` 三技能，一条命令安装到 AI Mana 与 Codex 双侧，旧版自动 SHA256 备份。
- **分层强制**：`layering_test.go`（go/parser import lint）强制 `cli → commands → protocol ← transport` 单向依赖；`protocol` 层零 IO，为移植边界。
- **文档**：`docs/PROTOCOL.md`（HTTP 协议实测参考）、`docs/MIGRATION.md`（自 rocwood / 官方 CLI 迁移）、`package/ADAPTATION-2022.md`（适配全记录）。

[Unreleased]: https://github.com/Kayungko/unity-pipe/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/Kayungko/unity-pipe/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Kayungko/unity-pipe/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Kayungko/unity-pipe/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.0
