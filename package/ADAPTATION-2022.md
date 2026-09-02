# com.unity.pipeline Unity 2022 适配记录

上游基线：`com.unity.pipeline` **0.6.0-exp.1**（Unity Registry tgz，upstream revision `27af91a943315a5702ac7679241399b649d6a20d`），官方要求 Unity 6.0+。扫描证据：`.tmpfiles/upstream-0.6-scan/`（registry `dist-tags.latest` = 0.6.0-exp.1，2026-09-02）。

本仓库 vendored 版本：**0.6.0-2022.1**（适配 Unity 2022.3.23f1；隔离编译门见本轮 cowork 目录，未进 SGAME/P4）。

版本命名依据：semver 预发布段字典序 `"2022.1" > "exp.1"`，UPM 视 vendored 版本高于 registry 版本，不会被"升级"覆盖。

## 补丁分层（upstream → vendored 的两类 diff，升级时处理策略不同）

| 层 | patch | 语义 | 上游升级时 |
|---|---|---|---|
| 兼容层 | `patches/0001-adapt-unity-2022.patch` | 纯 Unity 2022 API 兼容（package.json unity 版本、asmdef 去 InputSystem、PhysicsMaterial 别名、rawRenderQueue、GraphicsSettings、4 个 PluginImporter meta） | **须重放**到新树，冲突处审读 API 差异 |
| 演进层 | `patches/0002-evolution.patch` | 本仓功能演进：token 接缝剥离 + ArbitraryKeys 豁免、prefab/场景命令族 confirm 守门、test_status 陈旧结果修复、BuildOptions U6 API 规避、包内 CHANGELOG/版本号、Tests 演进用例 | **须保留**——新树重放 0001 后应用本层；与本仓新演进冲突时以演进层为准合并 |

守门：根级 `patches_sync_test.go` 断言「upstream+patches 与 vendored 树的实际差异文件集合」完全登记在 patch 链中——改了 vendored 而不重新生成快照会 fail build（对齐 `recipes_sync_test.go` 的治理模式）。

## 0.6 第一轮裁剪表

官方 0.6 相对 0.5 新增很多文件。本轮 **留下**（进 tgz / 编译门）：

- `Editor/Commands/Batch/*`、`Editor/Commands/Scripts/RunScriptCommand.cs`
- `Runtime/Common/CommandLineBinder.cs`、`CommandLineTokenizer.cs`、`ExecResponseSerializer.cs`、`CommandExecutionInfo.cs`
- `Runtime/Models/ArgProblem.cs`
- `Editor/Commands/ProjectSettings/RuntimePipelineSettingsCommands.cs`
- `Runtime/IlInterpreter/` + `Runtime/Analyzers/IlInterpreterAnalyzer.dll`（Runtime/Editor asmdef 硬引用；第一轮不裁，否则编译门必红。**不承诺 IL2CPP 热重载可用**）

本轮 **不跟、不作为能力承诺**（文件可仍在树里，因切断引用会更大）：

- 官方闭源 `unity` CLI / `unity mcp` / Hub-CI
- `GET /api/jobs`（官方 0.6 也没有）
- DialogStateMirror 的 trunk-build 对话框端点（AUTHAPI-64 changelog：Requires a recent enough trunk build）
- Analytics / eval telemetry 作为产品能力（代码在树内，默认不作为 upipe 表面）

Player：`RuntimePipelineManager` 已被 `RuntimePipelineDriver`/`Bootstrap` 替换。0001/0002 从未改过 Manager；第一轮只测 Editor 服务器。

## 完整改动清单（兼容层：upstream → vendored，git diff 快照见 patches/0001-adapt-unity-2022.patch）

| # | 文件 | 改动 |
|---|---|---|
| 1 | `package.json` | `unity: 6000.0 → 2022.3`；`version: 0.6.0-exp.1 → 0.6.0-2022.1` |
| 2 | `Runtime/Unity.Pipeline.asmdef` | 移除 `Unity.InputSystem` 引用（保留 `Unity.Pipeline.IlInterpreter`） |
| 3 | `Editor/Commands/Assets/AssetCommands.cs` | `#if !UNITY_6000_0_OR_NEWER` 守卫，回退 `PhysicsMaterial = PhysicMaterial` 别名 |
| 4 | `Editor/Commands/Materials/MaterialCommands.cs` | `rawRenderQueue` 守卫回退 `renderQueue` |
| 5 | `Editor/Commands/ProjectSettings/GraphicsSettingsCommands.cs` | `defaultRenderPipeline` 守卫回退 `renderPipelineAsset` |
| 6 | `Editor/PipelineAnalytics.cs` | Unity 2022 无 `IAnalytic`/`AnalyticInfo`：`#else` 空操作 stub，调用点保持可编译 |
| 7-10 | `Runtime/Plugins/CodeAnalysis/*.dll.meta` ×4 | PluginImporter v3 列表格式 → v2（GUID 保留） |

演进层（`patches/0002-evolution.patch`）要点：

- `Runtime/Common/BasePipelineServer.cs` — legacy token 回显的管道接缝两级剥离（含 `DispatchCommandOnMainThread` / batch 路径）
- `Runtime/Common/CliArgAttribute.cs` / `ParameterInfo.cs` / `CommandRegistry.cs` — `ArbitraryKeys` 豁免机制
- `Editor/Commands/Prefabs/PrefabCommands.cs` — prefab 命令族 confirm 守门
- `Editor/Commands/Scenes/SceneCommands.cs` — 场景命令族守门（脏场景保护 + 路径覆盖 + build list 移除需 confirm）
- `Editor/Testing/PipelineTestRunner.cs` — test_status 陈旧结果修复（启动即写 running 状态）
- `Editor/Commands/Build/BuildCommand.cs` — 补 2022.3 可用的 CompressWithLz4/HC/StrictMode（不引入 U6 `CompressWithLz4HCNoChunks`）
- `Editor/Commands/Materials/MaterialCommands.cs` / `Assets/AssetImportCommands.cs` — 移除逐命令 token workaround
- `Tests/Editor/**` ×4 + `CHANGELOG.md` — 回归用例与版本簿记

`UNITY_6000_5_OR_NEWER` 在 0.6 约数十处，多为 `[NoAutoStaticsCleanup]` 或已有 `#else`（如 `PipelineUtils.GetLoadedAssemblyPath`）。2022 上这些被预处理器去掉，**不是** 0001 的主工作量。

## 已知取舍

- **Tests/ 未部署**：上游测试引用 U6 API / InputSystem.TestFramework，移植成本高。vendored 树保留 Tests 供未来适配参考；`build/` 打 tgz 时剔除，`upipe pkg install` 同样跳过。
- **.cs.meta 的 MonoImporter 段**：Unity 2022 导入时自动在 `.cs.meta` 追加 `MonoImporter:` 段（upstream 的裸 v2 + guid 是合法的），属导入副作用，非适配改动。
- `.signature` / `.attestation.p7m`：上游签名文件对本地修改后的包已失效，分发包中剔除（vendored 树保留作溯源）。
- **upipe 默认 `verbose: true`**：官方 0.6 瘦信封默认丢掉 `command`/`executedAt`/`executionTimeMs`；客户端始终请求完整信封。

## 验证记录

- 2026-08-20：SGAME（Unity 2022.3.23f1，E:\P4\SGAME_o_jiarongke_6099）对 **0.5.x-2022.1** 编译零错误 + `editor_status` ready。
- 2026-08-25：`upipe` 对该 0.5 实例全命令面实测通过。
- 2026-09-02：vendor 0.6.0-exp.1 → 0.6.0-2022.1。隔离编译门 / 冒烟见 Phase C/D（未完成前不得声称 0.6 已跟上）。

## 上游升级流程

1. 下载新版本 tgz → 覆盖 `package/upstream/`（一次性 commit）。
2. 复制 `upstream/` 全树为 `com.unity.pipeline/`，重放兼容层 `0001`（冲突处人工审读 API 差异），再应用演进层 `0002`（以本仓演进为准合并）。
3. 版本号递增（如 `0.7.0-2022.1`），更新本文件并重新生成 `patches/` 快照（`go test ./...` 中的 `patches_sync_test.go` 会强制核对）。
4. `go run ./package/build` 重打 tgz，跑冒烟回归。
