# com.unity.pipeline Unity 2022 适配记录

上游基线：`com.unity.pipeline` **0.6.0-exp.1**（Unity Registry tgz，upstream revision `27af91a943315a5702ac7679241399b649d6a20d`），官方要求 Unity 6.0+。扫描证据：`.tmpfiles/upstream-0.6-scan/`（registry `dist-tags.latest` = 0.6.0-exp.1，2026-09-02）。

本仓库 vendored 版本为 **0.6.0-2022.26**，适配 Unity 2022.3.23f1。回移功能以官方 1.0.1 为参照，基线仍是 0.6.0-exp.1。

版本命名依据：SemVer 比较预发布版本时，纯数字标识符的优先级低于非数字标识符。`2022.2` 低于 `exp.1`。适配包通过 `pkg-install` 写入工程的 `Packages/com.unity.pipeline/`，UPM 使用这个 embedded 包。

## 补丁分层

| 层 | patch | 语义 | 上游升级时 |
|---|---|---|---|
| 兼容层 | `patches/0001-adapt-unity-2022.patch` | 纯 Unity 2022 API 兼容（package.json unity 版本、asmdef 去 InputSystem、PhysicsMaterial 别名、rawRenderQueue、GraphicsSettings、4 个 PluginImporter meta） | **须重放**到新树，冲突处审读 API 差异 |
| 演进层 | `patches/0002-evolution.patch` | 本仓功能演进：token 接缝剥离 + ArbitraryKeys 豁免、prefab/场景命令族 confirm 守门、test_status 陈旧结果修复、BuildOptions U6 API 规避、包内 CHANGELOG/版本号、Tests 演进用例 | **须保留**——新树重放 0001 后应用本层；与本仓新演进冲突时以演进层为准合并 |
| 回移层 | `patches/0003-editor-lifecycle.patch` 至 `patches/0006-profiler-and-release.patch` | Play Mode、等待、连接、Console、测试状态、截图、Prefab 字段和性能分析 | 按序重放并核对 Unity 2022 API 差异 |
| Windows 共存 | `patches/0007-windows-editor-ports.patch` | Windows Editor 自动端口与旧版通配监听错开，并串行完成端口分配和绑定 | 重放后复测新旧版本并行启动和域重载 |
| 测试状态同步 | `patches/0008-test-status-synchronization.patch` | 串行化测试结果文件读写 | 重放后复测并发轮询 |
| 异步等待与描述文件权限 | `patches/0009-async-wait-and-descriptor-permissions.patch` | 条件等待、同帧截图、命令副作用和 Editor 描述文件权限门 | 复测 Unity 2022 的截图、域重载与权限失败 |
| 性能诊断 | `patches/0010-profiler-diagnostics.patch` | 慢帧排序、帧内标记链、GC 分配调用栈和 Player 内存快照 | 复测 Unity 2022 的 IL2CPP 开发 Player 数据 |
| 两阶段测试与复杂字段 | `patches/0011-combined-tests-and-fields.patch` | 跨域重载合并测试结果，兼容复杂字段输入 | 复测 Unity 2022 Test Framework 1.1.33 和 Prefab 保存重载 |
| 等待与 Editor 退出 | `patches/0012-waits-and-editor-quit.patch` | 延迟目标、嵌套成员、值变化、截图与暂停，以及退出前的场景和 Prefab 保存选择 | 复测 Unity 2022 同帧证据、域重载和退出回读 |
| Profiler 会话与汇总 | `patches/0013-profiler-sessions-and-summary.patch` | 复用 .data 捕获，保护当前 Profiler 历史，按帧区间汇总 CPU 帧耗时与 GC 分配 | 复测切换确认、外部历史变更、域重载失效和统计结果 |
| Profiler 提前保存 | `patches/0014-profiler-stop-and-save.patch` | 提前保存已捕获的帧，复用自动保存路径，保留取消和历史保护 | 复测 Editor 与开发 Player 的提前保存、重复停止、写入失败和重载 |
| Profiler 线程与 marker 聚合 | `patches/0015-profiler-thread-markers.patch` | 按线程和帧区间聚合，区分累计耗时与重叠区间，定位原始样本 | 复测同名 marker、重叠样本、过滤和精确下钻 |
| 构建校验与卡点诊断 | `patches/0016-build-validation-and-dialog-status.patch` | 拒绝无效构建配置，回传已解析的构建计划和对话框检测能力 | 复测无写入预览、Player 构建与 Windows 模态对话框 |
| 异步脚本求值 | `patches/0017-async-eval-and-compile-only.patch` | 直接等待脚本中的 await、只编译和源诊断、协作式取消 | 复测同步兼容、主线程续跑、超时、取消和重载重连 |
| 自定义命令注册 | `patches/0018-dynamic-command-registration.patch` | 自定义静态和绑定实例命令加入既有命令目录，支持撤销 | 复测命令发现、注册冲突、域重载和 IL2CPP Player |
| Profiler 计数器与元数据 | `patches/0019-profiler-counters-and-marker-info.patch` | 读取本地捕获中的非 GPU 计数器及 marker 原生元数据 | 对照 Editor 与 IL2CPP Player 捕获，验证原始单位、真实零值及缺测帧 |
| 自定义命令结果 | `patches/0020-custom-command-results.patch` | 结构化成功与失败、终态日志和跨 await 的后台任务取消 | 对照 Editor 与 IL2CPP Player 的直接执行、后台任务和原有返回类型 |
| Editor 状态快照 | `patches/0021-editor-status-snapshot.patch` | 回传 Editor 阶段、Auto Refresh 和会话内代码加载次数 | 复测冷启动、编译失败、域重载和模态弹窗 |
| Profiler 样本重叠 | `patches/0022-profiler-overlaps.patch` | 查找两个 CPU marker 的时间交集，定位原始样本并计算逐帧去重覆盖 | 在 Editor 与 IL2CPP Player 捕获中对照原始纳秒时间戳 |
| 自定义命令二进制输入与输出 | `patches/0023-custom-command-binary-io.patch` | 绑定字节输入、返回二进制结果并沿用任务与日志契约 | 复测哈希、空数据、退出码与 stdout 中断 |
| 后台任务实时日志 | `patches/0024-live-job-logs.patch` | 分级日志与终态共享序号，支持续读及保留窗口提示 | 隔离 Editor 和 IL2CPP Player 验证取消、超时、失效和二进制 SHA256 |
| 变更选测与失败重跑 | `patches/0025-exact-test-selection.patch` | 保留跨重载请求，按程序集、阶段和用例全名精确筛选 | 隔离 Editor 验证 Git/P4 选测、同名用例和失败重跑 |
| 本地 Scene Preview | `patches/0026-scene-preview.patch` | Prefab AssetBundle 打包、专用开发 Player 构建及 PNG 输出 | 用 Unity 2022.3 Windows IL2CPP 开发 Player 验证渲染、组件保留和进程回收 |
| 屏幕空间 Canvas 预览 | `patches/0027-canvas-preview.patch` | 专用 Player 中渲染 UI Prefab，支持横竖屏 PNG | 回归 Overlay、Camera、嵌套 Canvas、错误回传及 3D 默认预览 |
| World Space Canvas 预览 | `patches/0028-world-canvas-preview.patch` | 按画布世界坐标和预览宽高比取景 | 复测旋转、缩放、多个独立画布和零面积失败回传 |
| 场景替换预览 | `patches/0029-scene-replacement-preview.patch` | 运行时替换 SpriteRenderer 或 Renderer 材质槽并同帧截图 | 验证 Built-in 场景、目标与周边像素、序列化资产哈希和进程回收 |
| UGUI Image 替换预览 | `patches/0030-ugui-image-preview.patch` | 为专用 Player 保留可选 UGUI 替换入口，复用场景预览和同帧截图 | 验证 Overlay、Camera Canvas、遮罩和九宫格，以及未安装 UGUI 工程的编译 |
| Editor 截图与缩略图等待 | `patches/0031-editor-capture-readiness.patch` | 按需打开 Game View，校验截图对应的视图和显示器，限时等待缩略图 | 复测窗口关闭、严格新帧、首次缩略图和超时后的文件状态 |

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
| 1 | `package.json` | `unity: 6000.0 → 2022.3`；`version: 0.6.0-exp.1 → 0.6.0-2022.2` |
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
- `Editor/Commands/Capture/CaptureCommands.cs` — Game View 截图返回帧状态，严格模式拒绝未确认帧
- `Editor/Commands/RecompileCommand.cs` — 有未解决的编译错误时保留失败状态及已收集的诊断
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
- 2026-09-02：vendor 0.6.0-exp.1 → 0.6.0-2022.1。隔离编译门（2022.3.23f1）零 CS 错误；vendored-tests 回归（含手工加录 0.6 新测试）：BatchCommandTests 28/28（事务回滚族全过）、CommandLineBinder/Tokenizer 82/82、CommandsTests 组合 63 通过 / 0 失败 / 4 URP 跳过（编译门工程无 URP，预期）。回归中发现并修复上游 0.6 测试重写冲掉的三处演进层测试代码（TokenHolderBehaviour / MarkActiveSceneDirty / RemoveSceneFromBuild confirm）+ SceneCommandsTests SetUp 脏场景隔离。SGAME 真实工程升级实测待做（tgz 已备）。

## 上游升级流程

1. 下载新版本 tgz → 覆盖 `package/upstream/`（一次性 commit）。
2. 复制 `upstream/` 全树为 `com.unity.pipeline/`，重放兼容层 `0001`（冲突处人工审读 API 差异），再应用演进层 `0002`（以本仓演进为准合并）。
3. 版本号递增（如 `0.7.0-2022.1`），更新本文件并重新生成 `patches/` 快照（`go test ./...` 中的 `patches_sync_test.go` 会强制核对）。
4. `go run ./package/build` 重打 tgz，跑冒烟回归。
