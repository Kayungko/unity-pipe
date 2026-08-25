# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added

- **`docs/RECIPES.md`**：agent 实测配方（SGAME 2022 生产工程验证）——回包判据字段表（eval 返回值在 `data.result.result`、层级树在 `result.roots`）、写面三种守门模式判据、eval 禁忌（System.Tuple 冲突 / `\n` 转义链断裂 / 2022 无 GetMonoBehavioursWithMissingScriptGUID）、LoadPrefabContents 只读拆解模板、missing script 精确定位配方、820 prefab 双轨普查策略（本地 YAML 全量 + eval 抽查权威）、Lua 字节码拼接加载对废弃反推的限制。
- `unity-pipeline` SKILL.md 新增 §0.5：回包值字段映射与写面守门速查（exit 0 ≠ 业务成功），链接 RECIPES.md。
- 场景对象写命令参数表补全（源码核对）：`rename_gameobject` 用 `target`+`name`、`set_transform` 坐标为 float 数组 `[x,y,z]`；`target` 裸字符串的六种解析优先级（globalId/assetPath/guid/instanceId/整数/hierarchyPath）。
- 勘误：hierarchy path 解析"失败"根因是 **Git Bash MSYS 路径改写**（`/Name` → `C:/Program Files/Git/Name`），非包 bug——`MSYS_NO_PATHCONV=1` 或 `//Name` 即可；SKILL.md Gotchas 与 RECIPES.md §4.2 已记录。
- RECIPES.md 已知限制补充：`find_gameobjects` 按 name 过滤实测未命中新建顶层对象（原因未查，eval 遍历替代）。

## [0.1.5] - 2026-08-25

### Added

- **`upipe instances`**：列出本机全部运行中的 pipeline 实例（工程名/路径/Unity 版本/端口/PID/心跳/TCP 可达性）。实例登记表在 `~/.upipe/projects.txt`（`pkg-install` 成功时自动登记，不落工程内配置）；存活判定以 TCP 探活为准、心跳龄为参考（空闲编辑器不刷心跳但端口可达，仍算在线）；`--all` 额外显示疑似残留（端口关闭且心跳过期的旧端口文件）。
- **`upipe plugin install --upgrade-rules`**：规则模板升级从「手动删块 × 双侧重跑」收敛为一条命令——与最新模板渲染做 hash 比对（一致跳过），整块替换前自动备份，旧块中用户自定义的行逐行列出提醒重新应用（不静默合并），**白名单/depot 配置自动保留**不被重置为 pipeline-only 默认；与 `--apply-rules` 互斥。
- README 新增「升级 upipe」节：`gh release download` 单鉴权面升级路径（含回滚与 SHA256 校验）。

### Fixed

- `instances` 判定修正：TCP 可达优先于 30s 心跳阈值（首版实现按心跳判定会把空闲编辑器误判为离线——实测 SGAME 编辑器空闲 2h 仍在线）。

### Tests

- registry 登记去重/容错单测；upgrade 全生命周期（missing-block/up-to-date/upgraded+备份+自定义行报告/dry-run 不落盘）与白名单保留单测；假 HOME 端到端验证（初装→模板变更→dry-run 预览→升级→白名单保留+新内容落盘+备份存在）。

## [0.1.4] - 2026-08-25

### Added

- **`upipe version`**：自报 CLI 版本（`-ldflags` 构建注入）与 vendored 适配包版本；发版产物新增 `upipe.exe.sha256` 伴随文件（此前只有 tgz 有）。
- **`scripts/import-from-p4`**：P4 工程紧急热修反向通道——只读 diff 工程内包与仓库 vendored 树（M/A/D 清单 + 小型行级 diff 提示），永不调 p4、永不写盘、永不 commit；对 SGAME 工程实测输出与已知状态精确吻合（94 meta + 3 签名文件差异）。

### Fixed

- **`--timeout` 打通客户端 HTTP 超时**：此前只映射进请求体，transport 层硬编码 120s round-trip 上限会把超 2 分钟的 eval（构建/烘焙/批量资产处理）在客户端掐断并误报为连接错误；现在 `--timeout` 同时驱动服务端命令超时与客户端 round-trip deadline（+30s 余量，上限对齐服务端 24h）。
- **tar 路径穿越防护**：`pkg-install` 的 tar 条目名此前仅 TrimPrefix 无逃逸检查，恶意/畸形 tgz 理论上可写任意路径；现在拒绝绝对路径、`..` 引用与 Windows 盘符条目。

### Changed

- `pkg-status` 的 vendoredRef 由硬编码字符串改为单一常量源（`commands.VendoredPkgRef`），包版本升级不再需要手动同步两处。
- 文档-代码落差清理：MIGRATION.md 的 daemon 条目改口「未实现」（UPIPE_DAEMON 从未在代码中留位）；`import-from-p4.py`（规划中）替换为已实现的 Go 工具。

### Tests

- `internal/transport`：长超时请求不被 120s 裁剪、短超时正确失败、24h 上限钳制、零超时回退默认——回归锁定 timeout 修复。
- `internal/commands`：`pkg-install` 写盘路径（tar 分类/穿越拒绝/畸形输入/apply 幂等）与 `plugin install` 写盘路径（备份落 skills 目录外、SHA256 校验、重装落新内容）——全仓唯二文件系统写副作用命令从零测试到全覆盖。

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

[Unreleased]: https://github.com/Kayungko/unity-pipe/compare/v0.1.5...HEAD
[0.1.5]: https://github.com/Kayungko/unity-pipe/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/Kayungko/unity-pipe/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/Kayungko/unity-pipe/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Kayungko/unity-pipe/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Kayungko/unity-pipe/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.0
