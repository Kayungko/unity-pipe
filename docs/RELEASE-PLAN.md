# unity-pipe 版本开发计划

## 已发布基线

**v0.1.34**（2026-11-27，`c3084cc`）已发布。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.34)

- `asset-check` 检查缺失、无效或孤立的 `.meta`、重复 GUID，以及文本 Prefab、Scene 中无法解析的引用。结果包含资产、节点、组件和字段。
- `asset-impact` 支持 Git 提交区间、P4 已提交 Changelist 及快照对比。它报告直接和间接受影响的 Prefab、Scene，并标出本次新增的失效引用。
- 验收已通过。隔离 Unity 2022.3.23f1 工程中，Git 与 P4 报告一致。移动材质后没有新增失效引用，删除材质后定位到 5 处失效引用，下一次变更新增了 2 处 GUID 歧义。现有 JSON 契约通过回归，11 个发布附件回下载后 SHA256 全部一致。

**v0.1.35**（2026-11-27，`6c06dde`）已发布。`asset-summary` 支持 Markdown 和 JSON，隔离快照的 Git、P4 报告一致。11 个附件回下载后 SHA256 全部一致，Windows CLI 的两种输出与验收记录一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.35)

**v0.1.36**（2026-11-27，`142d91b`）已发布。`asset-blame` 支持 Git 和 P4 字段历史，隔离工程验证了同名节点、跨移动追溯、字段不存在和历史不足。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.36)

**v0.1.37**（2026-11-27，`978db42`）已发布。`asset-conflict` 支持 Git 和 P4 三方只读比较，隔离工程验证了独立和同值修改、异值冲突及删除修改冲突。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.37)

**v0.1.39**（2026-11-27，`8669713`）已发布，一并交付 v0.1.38 的 `profiler_overlaps`。CPU 样本重叠已用隔离 Editor 和 IL2CPP 开发 Player 的捕获复验。自定义命令二进制标准输入与输出已用隔离 Editor 验收，Player 二进制链路列入 v0.1.40。11 个发布附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.39)

**v0.1.40**（2026-11-27，`c21e5f8`）已发布。后台任务支持实时日志和按序号续读。隔离 Editor 与 Windows IL2CPP 开发 Player 验证了续读和二进制往返，11 个发布附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.40)

**v0.1.41**（2026-11-27，`5a9b354`）已发布。Git / P4 变更选测、按失败用例重跑和 NUnit XML 已在隔离 Unity 2022.3.23f1 上验收。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.41)

**v0.1.42**（2026-11-27，`87cb216`）已发布。临时 batchmode Editor 支持自定义命令和 EditMode、PlayMode 测试，验证了已有 Editor 保护、失败、超时、Ctrl+C 和日志回传。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.42)

**v0.1.46**（2026-11-28，`eaa6630`）已发布，包含 v0.1.43 至 v0.1.45。隔离 Unity 2022.3.23f1 验证了临时 batchmode Editor 构建，以及 Windows x64 IL2CPP 开发 Player 的 3D、屏幕空间和 World Space Canvas 预览。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.46)

**v0.1.47**（2026-11-28，`f37bd52`）已发布。文本 `.asset` 对象、字段和引用变化已用隔离 Unity 2022.3.23f1 的 Git / P4 快照验收。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.47)

**v0.1.48**（2026-11-28，`9e75763`）已发布。Windows Runner 模板和脚本已验证选测、失败留档、超时回收与 IL2CPP 恢复构建。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.48)

**v0.1.49**（2026-11-28，`b8ded8b`）已发布。UnityYAMLMerge 已在隔离工程验证 Git 和 P4 Snapshots 的三方合并预演及候选回读。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.49)

**v0.1.50**（2026-11-28，`4a093a5`）已发布。隔离 Unity 2022.3.23f1 Built-in IL2CPP Player 验证了 SpriteRenderer 的 Sprite 和 Renderer 材质槽替换、同帧截图及进程回收。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.50)

**v0.1.51**（2026-11-28，`3280850`）已发布。隔离 Unity 2022.3.23f1 Built-in IL2CPP 工程验证了 UGUI Image 的 Sprite 和 Material 替换、Overlay 与 Camera Canvas、嵌套遮罩、九宫格和进程回收。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.51)

**v0.1.52**（2026-11-28，`e2dbefd`）已发布。隔离 Unity 2022.3.23f1 图形 Editor 验证了 Game View 按需捕获、资产缩略图等待和严格新帧检查。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.52)

**v0.1.53**（2026-11-28，`cc2bd9f`）已发布。隔离 Unity 2022.3.23f1 工程验证了 URP 14 单 Base 相机及 Base/Overlay 堆栈的场景替换预览。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.53)

**v0.1.54**（2026-11-28，`5261d86`）已发布。Editor Mono、Editor Interpreter 和 Windows IL2CPP 开发 Player 验证了同文件 partial 类、未改方法、签名拒绝及回滚。11 个附件回下载后 SHA256 全部一致。 [发布记录](https://github.com/Kayungko/unity-pipe/releases/tag/v0.1.54)

## 开发顺序与验收

未发布版本号为计划，按顺序推进。各版用隔离的 Unity 2022.3.23f1 工程或对应版本生成的快照验收。GPU 继续暂缓，SGAME 先不动。

| 版本 | 交付 | 验收标准 |
| --- | --- | --- |
| v0.1.35 | `asset-summary` 输出可用于 Git PR 或 P4 CL 评审的 Markdown，并提供结构化 `--json`。复用资产影响和结构差异，汇总资产增删移动、节点与字段变化、间接受影响的 Prefab/Scene 和新增失效引用。 | 同一批 Git 与 P4 快照的报告内容一致。覆盖资产移动、嵌套 Prefab、同名节点、先前已有的失效引用、未扫描文件和零变更。Markdown 的首个报告章节列出新增失效引用，JSON 保持单信封。 |
| v0.1.36 | 字段历史追溯。由 Prefab 或 Scene 的对象和字段定位最近一次修改，回传提交或 CL、作者和时间。 | 对照 Git 提交和 P4 Changelist 原记录验证。同名节点能区分，资产移动后可继续追溯，字段不存在或历史不足时状态明确。 |
| v0.1.37 | 三方冲突解释。读取基线和双方快照，按资产、节点及字段展示分歧。 | 使用 Git 和 P4 快照验证双方改不同字段、同字段同值、同字段异值，以及一方删除另一方修改。报告可回溯到三侧原始值。执行前后工作状态一致。 |
| v0.1.38 | Profiler 重叠样本分析。对已有捕获中的线程和帧区间计算时间重叠，并定位原始样本。 | 隔离 Editor 和 IL2CPP 开发 Player 的已保存捕获都能复验。验证同线程重叠、不同线程同一时间段、边界相接和缺测帧。结果与样本时间一致。 |
| v0.1.39 | 自定义命令支持二进制标准输入与输出。 | 用含 NUL 字节的样本验证往返哈希，测空输入、输出中断、超时与取消。结构化日志走 stderr，原有 `--json` 模式通过回归。 |
| v0.1.40 | 后台自定义命令实时分级日志。`upipe job` 按日志序号续读，日志走 stderr。日志过期或任务失效时给出明确状态。 | 隔离 Unity 2022.3.23f1 Editor 验证任务结束前收到日志、断线续读无重复和遗漏、并发读取互不影响、保留窗口溢出和域重载失效。回归取消、超时、失败、`--json` 单信封和 `--stdout` SHA256，并在隔离 IL2CPP 开发 Player 验收日志和二进制往返。 |
| v0.1.41 | Git / P4 变更影响到测试程序集的选测、失败用例重跑和 NUnit XML。 | 同一批 Git / P4 快照选出一致的测试程序集。验证依赖变更、脚本删除或移动、无匹配、无法判定时的保守回退。NUnit 汇总与现有终态结果一致。 |
| v0.1.42 | 启动临时 batchmode Editor 的 CI 入口，先覆盖自定义命令与测试，后续扩展构建。 | 无常驻 Editor 的隔离工程可启动、执行并正常退出。验证编译失败、启动失败、超时、取消、日志落盘及进程回收。 |
| v0.1.43 | 临时 batchmode Editor 的 Player 构建。沿用预检、确认和 BuildReport，等待同一构建 ID。 | 隔离 Unity 2022.3.23f1 验证预检及未确认时无产物、真实构建及正确输出、构建失败回传、超时与取消后回收进程，以及后续构建恢复。 |
| v0.1.44 | Unity 2022.3 本地 Scene Preview。首版在独立开发 Player 中预览一个 Prefab，随包携带依赖清单。 | Windows x64 上回装正式包。验证按目标平台打包、嵌套 Prefab 和传递依赖、重复加载、缺失依赖、损坏包、宿主场景和进程回收。失败沿用 `--json` 单信封和退出码 `20`。 |
| v0.1.45 | 本地 Scene Preview 支持屏幕空间 Canvas 和横竖屏 PNG。 | 隔离 Unity 2022.3.23f1 IL2CPP 开发 Player 验证 Overlay、Camera、嵌套 Canvas 和布局回读，验证没有受支持绘制目标时返回 `20`。回归 3D 默认尺寸、取消和清理。 |
| v0.1.46 | 本地 Scene Preview 支持独立 World Space Canvas。 | 隔离 Windows x64 IL2CPP 开发 Player 验证旋转、缩放、多个根画布和横竖屏取景，零面积返回 `20`。回归屏幕空间 Canvas、3D 和进程回收。 |
| v0.1.47 | `asset-diff` 和 `asset-summary` 支持文本 `.asset` 的对象、字段、数组差异，以及各侧引用解释。 | 隔离 Unity 2022.3.23f1 生成同一批 Git / P4 快照，覆盖子对象、数组、同文件引用、目标移动和删除、真实二进制资产提示。回归旧 `--json` 和退出码。 |
| v0.1.48 | Windows Runner CI 模板和结果留档，复用选测、NUnit XML 和 batchmode 构建。 | 验证零匹配、测试失败时仍归档结果和日志、构建失败退出码、超时回收。 |
| v0.1.49 | `asset-merge-preview` 使用 UnityYAMLMerge 预演 Prefab 和 Scene 三方合并。 | 用 Unity 2022.3.23f1 验证双方独立编辑和同值修改、同字段冲突、删除修改冲突。回读候选文件及相对双方的差异，核对源工作区状态。 |
| v0.1.50 | 在隔离 Player 的 Built-in 宿主场景按完整节点路径替换 SpriteRenderer 的 Sprite 或 Renderer 的指定材质槽，用场景相机输出同帧前后 PNG。 | 用隔离 2022.3.23f1 Windows x64 IL2CPP 开发 Player 核对目标和周边像素、资产哈希、失败回传及正常退出、超时和 Ctrl+C 的进程回收，回归 Prefab 预览。URP 单独评估兼容性。 |
| v0.1.51 | 在 Built-in 场景按节点预览 UGUI Image 的 Sprite 和 Material，先验收 Sprite，再补 Material。 | 在隔离 2022.3.23f1 IL2CPP Player 验证 Overlay、Camera Canvas、嵌套遮罩与九宫格，核对同帧前后 PNG、目标外像素、源资产哈希及超时和 Ctrl+C 后的进程回收。回归现有预览、`--json` 和退出码。 |
| v0.1.52 | Editor 截图支持按需打开 Game View，资产缩略图支持限时等待。 | 验证 Game View 关闭、暂停无新帧、冷启动缩略图生成和超时。回传来源与帧状态，回归严格新帧选项。 |
| v0.1.53 | URP 14 场景预览先支持 SpriteRenderer 和 Renderer 材质槽替换。URP 的 UGUI 与 Prefab 预览单独验证。 | 新建隔离 2022.3.23f1 工程验收单 Base 相机、Base/Overlay 堆栈、两种替换和目标外像素，回归 Built-in。验收后按 `AGENTS.md` 归档并清理工程与 Unity Hub 条目。 |
| v0.1.54 | Method Reload 兼容修复，沿用现有 HotReload 入口。 | Editor 和 IL2CPP 开发 Player 分别验证 partial 类、未改方法、签名变化拒绝及恢复。 |
| v0.1.55 | batchmode 构建通过 `--config` 从项目的 `.upipe/builds.json` 读取目标、场景、选项与输出路径。 | 两套配置实际构建并启动 Player 核对启动场景和开发标志。验证预检解析值、配置语法或 Unity 校验失败时的 JSON 和退出码，以及构建前后 Editor 设置回读。 |
| 暂缓 | GPU 计数器和耗时分析。 | 恢复前先确认目标开发 Player 能稳定采到 GPU 数据，再检验单位以及零值和缺测的区分。 |

### Scene Preview 可行性记录

Unity 2022.3.23f1 的隔离实验采用 AssetBundle。Windows x64 IL2CPP Development Player 从依赖清单加载嵌套 Prefab 及其引用的 TextAsset，在临时场景中实例化，随后卸载场景及本次加载的 Bundle。源资产和 `.meta` 的哈希一致，宿主场景保持激活。不存在的源资产、缺失依赖和损坏包均返回结构化退出码 `20`，同一 Player 随后再次加载成功。实验运行于 `-batchmode -nographics`，画面和项目专有组件仍待验收。

产品入口采用显式启用的独立预览 Player，以进程隔离项目脚本的全局状态。构建与预览须使用匹配的目标平台，并在回装验收中确认包内组件所需类型保留。官方 Project Service 的内容归档和浏览器预览另需 Unity 服务链路。 [Unity 2022.3 AssetBundle 文档](https://docs.unity3d.com/2022.3/Documentation/Manual/AssetBundles-Dependencies.html) · [Unity Pipeline 官方说明](https://discussions.unity.com/t/unity-pipeline-1-0-0-pre-1-is-out/1741564)

## 交付检查

- 新增读操作沿用退出码 `0`、`2`、`20`、`21`。报告里有问题时，读取成功仍返回 `0`。其他命令继续遵守现有退出码契约。
- 候选版记录源码提交、验收工程版本、测试结果及包的 SHA256。发布后回下载全部附件，逐个核对候选包与发布文件。
