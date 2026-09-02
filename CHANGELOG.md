# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [0.1.11] - 2026-09-02

### Added

- **适配包 0.6.0-2022.1**：vendor 官方 `com.unity.pipeline` 0.6.0-exp.1（rev `27af91a9`）。留下 `batch` / `run_script` / `commandLine`+`argv` / 瘦信封 serializer / ArgProblem / Runtime Pipeline settings。第一轮不裁 IlInterpreter 树（Runtime/Editor asmdef 引用，编译门需要），也不接官方 `unity mcp`。
- **`PipelineAnalytics` 2022 stub**：Unity 6 `IAnalytic`/`AnalyticInfo` 在 2022.3 编译失败；`#else` 空操作，调用点保留。隔离工程 2022.3.23f1 编译门通过。
- **upipe 默认 `"verbose": true`**：对抗 0.6 瘦信封丢掉 `command`/`executedAt`。回归：`TestExecSendsVerboseTrue`。
- **`upipe eval` / `eval-file --job`**：长 eval 可分离，不经过 `execSimple`（避免漏到 recompile/audit/test）。
- **`upipe audit --categories` / `--output`**：透传包参数（此前固定空 `{}`）。
- **SKILL §4.5**：detached job 主路径（提交 / 等待 / 取消 / progress）；PROTOCOL.md 对齐 `canceled` 与 cancel `id`。

### Fixed

- **`upipe job-cancel` 请求体字段**（正确性）：服务端 `HandleJobCancelRequest` 只读 JSON `id`，客户端此前发 `jobId` → 400。现在双写 `id`（权威）+ `jobId`（兼容），返回 `JobResponse`。回归：`TestCancelJobBodyUsesId` / `TestJobCancelSendsIdNotJobId`。
- **job 取消终态与退出码 22**：包状态是美式 `canceled`，CLI 只等英式 `cancelled` 且 Poll 把 TerminalFail 一律映成 exit 20——取消会空转到 timeout（21）。新增 `PollConfig.TerminalCancel` → `ErrJobCancelled`（22）；`failed` 仍 20。回归：`TestJobCanceledAmericanSpellingExits22` / `TestJobCancelledBritishSpellingAlsoExits22` / `TestJobFailedStillExits20`。
- **未设 `--timeout` 时同步 exec 被服务端 60s 掐断**：body 省略 timeout 会落到 `CommandExecutionRequest.Timeout ?? 60000`，客户端 round-trip 却是 10min。现在未指定也发送 10 分钟（`DefaultRoundTripTimeout`）。回归：`TestExecSendsDefaultTimeoutWhenUnset`。
- **0.6 vendor 冲掉演进层测试代码（3 处恢复 + 1 处隔离修复）**：上游 0.6 重写 Tests 文件时丢掉本仓 0.5.x 的演进层用例辅助——`ComponentCommandsTests` 的 `TokenHolderBehaviour`（token 字段豁免回归）、`SceneCommandsTests` 的 `MarkActiveSceneDirty()` 与 `RemoveSceneFromBuild(confirm: true)`；编译门 vendored-tests 首轮即暴露。另补 `SceneCommandsTests` SetUp 脏场景丢弃（batch/component 套件留下的脏场景会让 create_scene 守门级联拒绝后续全部用例）。回归实测：BatchCommandTests 28/28（含事务回滚族）、CommandLineBinder/Tokenizer 82/82、CommandsTests 组合 63 通过/0 失败/4 URP 跳过（编译门工程无 URP，预期）。

## [0.1.10] - 2026-08-26

### Fixed

- **守门文档与 0.5.2 行为全面同步**（工作流研究 14 代理 / 对抗验证 8 条漂移全存活）：scenes.md/prefabs.md 补齐 5 个守门命令的 confirm 记载与恢复路径（此前 agent 按文档调用必被拒一次且无恢复指引）；materials.md 记载 token 接缝边界；SKILL.md §0.5 守门表扩列 + 退出码补 22/50 + tgz 示例占位符；RECIPES §2 扩列 + 新增拒绝消息签名表（消息特征→守门→恢复动作）。

### Added

- **技能触发面双语化**：SKILL.md description 补齐生产高频任务词（prefab/场景/序列化资产/烘焙/build/截图/jobs）+ 中文触发短语——消除 AI Mana 中文环境漏触发导致的裸探索。
- **消除包内旧技能冲突源**：`package/com.unity.pipeline/.claude/skills` 此前是上游原件（描述闭源 unity CLI 契约，与正版技能在包目录 session 实际竞争）——现为同步副本，`skill_sync_test.go` 锁定一致。
- **RECIPES 新配方**：脏场景恢复流、安全覆盖三步配方、批量 confirm 三档语义、detached jobs（§5.5）、settling + test 轮询判据（§5.6）。
- **发现配方**：`upipe commands --full --query <name>` 单往返直取 schema（命令发现 3 跳→2 跳），替换指向不存在章节的死链。

## [0.1.9] - 2026-08-26

### Fixed

- **共享 flag 预解析的 stderr 噪声泄漏**：`upipe pkg-install -source x.tgz` 这类携带子命令专属 flag 的调用，会先打印 `flag provided but not defined: -source` + usage 再正常执行成功——cli.Main 的共享 flag 预解析是 best-effort（子命令自己的解析才是权威），其 flagset 现在静默（`io.Discard`），单/双横线形式均零噪声。回归锁定：`TestSharedFlagPrepassSilentForSubcommandFlags`。

## [0.1.8] - 2026-08-26

### Fixed

- **test_status 陈旧结果**（包，正确性）：`run_tests` 异步状态文件此前只在完成时写——运行未启动成功（如主线程派发超时）时，上一次运行的 COMPLETED 文件残留磁盘，后续每次 `test_status` 轮询都把旧结果当本次返回（实测三个不同 filter 返回同一份数据）。现在启动命令同时原子写入 `status=running`，轮询者只能观察本次运行的生命周期；`CheckForPendingTests` 改为读内容判断（running 文件在 play-mode 域重载后正确重挂 collector）。真机验证：三个 filter 分别返回 9/15/19 且全绿，行为在 domain reload 后保持。
- **`create_prefab_variant` 覆盖守门缺失**（包，深度评审 P1）：与 create_prefab 同构的拒绝守门（已存在资产默认拒绝，需 confirm=true），此前 confirm 为纯占位而 SaveAsPrefabAsset 静默覆盖任意已存在路径。

### Added

- **场景族写面守门**（包）：`open_scene`/`create_scene` 非 additive 模式在有未保存修改时拒绝（提示先 save_scene 或 confirm=true——脚本化 Single 模式直接丢弃编辑且不可恢复）；`create_scene` 对已存在场景资产默认拒绝覆盖；`remove_scene_from_build` 需 confirm（build list 变更立即落盘且场景索引加载会静默坏）。SceneCommandsTests 24/24（含 7 个新守门用例），真机 2022.3 验证。
- **vendored-tests harness**（`scripts/vendored-tests`）：把 vendored 测试子集搭进任意 Unity 2022.3 工程的可重复工具（--setup/--teardown，manifest 追踪精确还原）——无 Unity 许可 CI 的替代验证链路。
- **CI 双平台矩阵**：windows-latest 加入（开发/发版平台此前反而不在 CI；v0.1.7 盘符缺陷正是单平台盲区所致）。
- **契约锁定测试**：退出码契约（skill §0 的 10/11/12/13/20 快照）、版本三源（package.json ↔ VendoredPkgRef ↔ dist tgz 名）、pkg-install 包装层（P4 只读门/dry-run/注册挂点）、tgz 构建布局（含 CLAUDE.md/CODEOWNERS/.claude 剔除）。
- **patch 快照双层治理**：`patches/0002-evolution.patch`（演进层）+ `patches_sync_test.go` 漂移守门（vendored 漂移出 patch 登记即 fail build）；ADAPTATION-2022.md 记录兼容层/演进层两类升级策略。

## [0.1.7] - 2026-08-26

### Fixed

- **`--timeout` 单位矛盾**（正确性）：请求体 `timeout` 由按秒发送改为按毫秒发送——服务端按毫秒消费（`CommandExecutionRequest.Timeout` → `dispatcherTimeoutMs`），此前 `--timeout 60s` 实际只给主线程 60ms 预算。`docs/PROTOCOL.md` 同步固化毫秒契约。回归锁定：`TestExecSendsTimeoutInMillis`。
- **`DefaultTimeout` 从未生效**：未设 `--timeout` 时现在真正应用 10 分钟预算作为 round-trip 上限（`protocol.DefaultRoundTripTimeout`，不依赖 options 包），不再退回 transport 默认 120s——长 eval/构建不再被静默掐断并误报为连接错误。回归锁定：`TestExecAppliesDefaultRoundTripTimeout`。
- **重试梯耗尽丢失 busy 语义**：梯耗尽时按最后错误类型传播——持续 busy（503/429）保持 exit 13（可重试），仅连接类错误才是 exit 11；此前 ~49s 队列满被误报为 "editor unreachable"，agent 走错恢复路径。回归锁定：`TestReloadTolerantExhaustionPropagatesBusy`。
- **`ErrBusy` 丢弃服务端信封**：503/429 响应 body 现以 `json.RawMessage` 进入 `error.data`，agent 可读到 settling/队列详情。回归锁定：`TestErrBusyCarriesServerEnvelope`。
- **`create_prefab` 静默覆盖已存在资产**（包，破坏性安全修正）：目标 `.prefab` 已存在时默认拒绝（`ArgumentException`，消息指明需 `confirm=true`）——资产覆盖不可 Undo，此前同路径二次保存直接 `SaveAsPrefabAssetAndConnect` 覆盖。回归锁定：`CreatePrefab_ExistingPath_RefusesWithoutConfirm` / `CreatePrefab_ExistingPath_WithConfirm_Overwrites`（ViaClient + CommandDirect，SGAME 2022.3 实测）。
- **`save_prefab_contents` confirm 参数描述与行为不符**（包，文档性）：描述暗示 false 时拒绝重写，实际从不消费——改为与其余命令一致的 "Reserved for parity" 措辞（重写已被 authoring root 约束）。

### Added

- **`upipe eval` / `eval-file` 新增 `--eval-timeout <毫秒>`**：直接透传服务端命令级超时（默认服务端 5000ms），消灭长 eval 必须绕道 `upipe exec eval --set timeout=<ms>` 的暗道；越界值（<0 或 >24h）客户端 exit 2 快速失败。独立命名是因为共享 `--timeout` 是 Go duration（`30s`/`2m`）语义且同时驱动 round-trip 预算。
- **CI（GitHub Actions）**：`.github/workflows/ci.yml`——push/PR main 触发 `go build/vet/test ./...` 全量（layering 与 recipes 同步锁定测试自动纳入）。C# 侧（package/com.unity.pipeline）因无 Unity 许可暂无法 runner 编译，注释标注待 Unity GameCI 接入。
- **CLI 自解释**：顶层 `upipe --help` 打印完整命令表；子命令 `--help` 输出真实用法与 flag 列表（此前只有一行 `flag: help requested`）。
- **`commands --query` 生效**：客户端按 name/描述子串过滤（compact 与 `--full` 模式均支持，未知字段原样保留），删除"解析后即弃"的 M2 注释。
- **实例登记成真**：任意命令成功执行后 best-effort 登记 `~/.upipe/projects.txt`（带去重）——`upipe instances` 的 hint 文案此前声称"跑一次即登记"但唯一调用点在 pkg-install，现在文案与实现一致。
- **prefab 命令族 confirm 守门**（包）：7 个命令统一补 `confirm` 参数——`create_prefab` 为覆盖守门（见上），其余 6 个（instantiate/variant/apply/revert/unpack/save_prefab_contents）为 parity 声明（场景侧可单步 Undo 或已受 authoring root 约束），参数面与 material 族对齐。
- **legacy token 回显收敛到管道接缝统一剥离**（包）：`BasePipelineServer.ExecuteCommandByName` 入口与 `ExtractCommandParameters` 字典参数两级剥离 `"token"` 键（unity-tools 血统客户端会把 auth token 回显进参数对象），删除 `set_material_properties` / `set_import_settings` 内 3 处逐命令 workaround。回归锁定：`Set_ViaClient_TokenKeyInProperties_IsStrippedAtPipelineSeam`。
- **`[CliArg(ArbitraryKeys = true)]` 逃生口**（包）：键来自用户命名空间的字典参数豁免接缝剥离——`set_component_properties.properties`（任意组件序列化字段名）与 `set_serialized_field.value` 已声明，用户组件上名为 `token` 的字段不再被静默丢弃；schema 固定字典（material/import settings）保持默认剥离。回归锁定：`SetComponentProperties_ViaClient_TokenKey_IsPreservedForArbitraryKeys`。

### Removed

- 死 flag `--verbose`：注册时承诺 "human progress notes on stderr" 但全仓无消费者，直接删除。

## [0.1.6] - 2026-08-25

### Added

- **`docs/RECIPES.md`**：agent 实测配方（SGAME 2022 生产工程验证）——回包判据字段表（eval 返回值在 `data.result.result`、层级树在 `result.roots`、find_gameobjects 结果在 `gameObjects`）、写面三种守门模式判据、eval 禁忌（System.Tuple 冲突 / `\n` 转义链断裂 / 2022 无 GetMonoBehavioursWithMissingScriptGUID）、LoadPrefabContents 只读拆解模板、missing script 精确定位配方、820 prefab 双轨普查策略（本地 YAML 全量 + eval 抽查权威）、Lua 字节码拼接加载对废弃反推的限制。
- `unity-pipeline` SKILL.md 新增 §0.5：回包值字段映射与写面守门速查（exit 0 ≠ 业务成功），链接 RECIPES.md。
- 场景对象写命令参数表补全（源码核对）：`rename_gameobject` 用 `target`+`name`、`set_transform` 坐标为 float 数组 `[x,y,z]`；`target` 裸字符串的六种解析优先级（globalId/assetPath/guid/instanceId/整数/hierarchyPath）。
- **技能自包含**：RECIPES.md 打包为 `harness/skills/unity-pipeline/references/RECIPES.md` 随插件分发；新增 `recipes_sync_test.go` 锁定权威版与分发副本一致，编辑 `docs/RECIPES.md` 后未重新打包会 fail build。

### Fixed

- 勘误（文档）：hierarchy path 解析"失败"根因是 **Git Bash MSYS 路径改写**（`/Name` → `C:/Program Files/Git/Name`），非包 bug——`MSYS_NO_PATHCONV=1` 或 `//Name` 即可；SKILL.md Gotchas 与 RECIPES.md §4.2 已记录。
- 勘误（文档）：`find_gameobjects` 按 name 过滤工作正常——此前误报"未命中"是调用方读错回包字段（正确字段 `gameObjects`，已入 §1 判据表）。

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

[Unreleased]: https://github.com/Kayungko/unity-pipe/compare/v0.1.10...HEAD
[0.1.10]: https://github.com/Kayungko/unity-pipe/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/Kayungko/unity-pipe/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/Kayungko/unity-pipe/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/Kayungko/unity-pipe/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/Kayungko/unity-pipe/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/Kayungko/unity-pipe/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/Kayungko/unity-pipe/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/Kayungko/unity-pipe/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Kayungko/unity-pipe/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Kayungko/unity-pipe/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.0
