<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
  <img src="assets/logo.svg" width="400" alt="upipe · Unity Editor Pipeline">
</picture>

**自维护的 Unity Pipeline 薄客户端 · Unity 2022 适配包分发 · AI Harness 插件包**

[![Go 1.23+](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white)](go.mod)
[![Unity 2022.3](https://img.shields.io/badge/Unity-2022.3-111827?style=for-the-badge&logo=unity&logoColor=white)](package/ADAPTATION-2022.md)
[![Zero dependencies](https://img.shields.io/badge/dependencies-zero-16A34A?style=for-the-badge)](#开发)
[![Agent ready](https://img.shields.io/badge/agent-ready-7C3AED?style=for-the-badge)](#ai-提示词大全日常这样用)

[快速开始](#快速开始) · [AI 提示词大全](#ai-提示词大全日常这样用) · [术语表](#术语小词典) · [架构设计](docs/ARCHITECTURE.md) · [协议参考](docs/PROTOCOL.md) · [更新日志](CHANGELOG.md)

</div>

---

## 这是什么

**装完之后，你只要开着 Unity 工程，把下面这句发给 AI 就行——全程不用碰命令行：**

```text
帮我编译这个工程，跑一遍编辑器测试，把结果告诉我。
```

背后发生的事：你（说人话）→ AI 助手 → upipe.exe → 正在运行的 Unity Editor，AI 负责中间所有命令。

- upipe 是一个命令行小工具：AI 用它指挥 Unity 编译、跑测试、改场景、装包；
- 你不需要学任何命令——那是 AI 的活。只想看一眼命令长什么样的，翻到「手动模式」（快速开始最后一节）；
- 前提只有一条：**Unity Editor 自己打开着**（upipe 不会替你启动 Unity）。

> 📌 本仓库为**私有仓库**：下载安装包前需先获得仓库所有者的 GitHub 访问授权。

## 快速开始

### 前置条件

- 一个会动手干活的 AI 助手：**AI Mana / Claude Code** 或 **Codex**（下称 harness，AI 能跑命令、能改文件）；
- 一个能正常打开的 Unity 2022.3 工程；
- 本仓库的 GitHub 访问授权（私有仓库，向仓库所有者申请）。

### 最快路径：安装也交给 AI（推荐）

给 AI 服务的工具，安装配置也交给 AI——**你不需要打开命令行**。把 `<...>` 替换成实际值后，把这段话发给你的 AI 助手：

```text
请帮我安装并配置 upipe——一个让你能安全指挥 Unity Editor 的工具
（私有仓库 github.com/Kayungko/unity-pipe，我已有访问权限）。步骤：
1. 把仓库 clone 到本地，让 upipe 命令可用：优先从 Releases 下载 upipe.exe
   放进 PATH 里的目录；没有合适目录就新建一个（如 C:\Tools）并加入我的 PATH；
2. 从 Releases 下载适配包 tgz，给我要装的 Unity 工程装适配包：
   先用 --dry-run 给我看安装计划，我确认后再正式安装；
   工程路径：<我的 Unity 工程路径>
3. 安装你的 upipe 技能和全局安全规则，一条命令：
   upipe plugin install --apply-rules
   （它会自动装技能，并把规则追加进你的全局配置——AI Mana/Claude Code 的
   CLAUDE.md 与 Codex 的 AGENTS.md，改前自动备份，重复执行不会重复写入；
   如果我有需要双信道的工程，把路径 glob 告诉 AI，例如：
   --co-use-whitelist "E:\P4\SGAME_*" --co-use-depot //sgproj/trunk，
   没有就省略，AI 按最保守方式工作）；
4. 全部完成后告诉我怎么验证。
```

过程中你只需要做两件事：**看一眼第 2 步的安装计划并确认**，以及最后**打开 Unity Editor**。下载、配 PATH、装技能、贴规则、替换占位符——全部由 AI 完成。

> 💡 第 3 步的 `--apply-rules` 别让 AI 省掉：技能是「说明书」，规则是「安全守则」。只装技能不贴规则，AI 不知道「场景 / Prefab / 材质等文件禁止直接改」这条红线，可能绕过保护改坏工程文件。

### 验证

打开工程让 Unity Editor 运行，然后把这句发给 AI：

`帮我探活一下 Unity 编辑器`

它调用 `upipe status` 并报告 ready，即全链路打通 ✅

- 若 AI 说「不认识 upipe」：Codex 必须重开一个新对话才生效（AI Mana 当前窗口即可）；
- 想自己确认：`C:\Users\<你>\.claude\skills`（或 `.codex\skills`）里应有 `unity-pipeline` / `unity-router` / `unity-mcp-bridge` 三个文件夹。

### 想自己动手？（可选）

<details>
<summary><b>手动安装三步</b>——排查问题时用得上</summary>

**① 装 upipe**：到 [Releases](https://github.com/Kayungko/unity-pipe/releases) 下载 `upipe.exe` 放进固定文件夹（例 `C:\Tools`），`Win + I` 搜「编辑账户的环境变量」→ 环境变量 → 用户变量 `Path` → 新建 → 填文件夹路径；重开 PowerShell 输 `upipe status` 验证。开发者可 clone 仓库后 `go build -o dist/upipe.exe ./cmd/upipe`（需 Go 1.23+）。

**② 装适配包**（每个工程一次）：从 Releases 下载 `com.unity.pipeline-*.tgz`，然后：

```powershell
upipe pkg-install --project-path <project> --source <tgz路径> --dry-run   # 预览，无误后去掉 --dry-run
```

装好后打开该工程的 Unity Editor 即可（出现「P4 / p4 edit」提示是团队版本控制相关，个人项目忽略）。

**③ 装技能 + 贴规则**：仓库目录内执行 `upipe plugin install --apply-rules`——一条命令装技能并把规则追加进全局配置（改前自动备份；重复执行不会重复写入）。有 co-use 白名单工程就加 `--co-use-whitelist "E:\P4\SGAME_*" --co-use-depot //sgproj/trunk`，省略则按最保守的 pipeline-only 模式写入。

装完之后的变更——早期版本补规则、白名单事后调整、规则模板升级跟进——见 [harness/rules/README.md](harness/rules/README.md) 的「已部署机器的后续操作」；手动贴模板的老办法也在该文档。

</details>

### 升级 upipe（装过的人）

upipe 升级只需要替换 exe——不用 clone 仓库，一条 `gh release download` 搞定（前提：目标机器 `gh auth login` 过，仓库授权同一凭据即可，无需单独配 git clone 权限）：

```powershell
gh release download v0.1.12 --repo Kayungko/unity-pipe --pattern upipe.exe --pattern upipe.exe.sha256 --output <你放 upipe.exe 的目录> --clobber
upipe version   # 确认版本号已更新
```

- 回滚同理：把 `v0.1.12` 换成任意历史 tag 重新下载即可（Release 保留全部历史版本）；
- 下载后可校验：`Get-FileHash upipe.exe -Algorithm SHA256` 与 `upipe.exe.sha256` 内容比对；
- 适配包 tgz 同一个 Release 页面（`--pattern com.unity.pipeline-*.tgz`），装法见上面的 ②；
- 规则模板有更新的机器，顺手跑 `upipe plugin install --upgrade-rules`（见 harness/rules/README.md）。

### AI 提示词大全（日常这样用）

装完之后的使用方式就一种：**打开 AI 对话框，用大白话提需求**。下面加粗的句子都能直接复制发给 AI（把工程名、命名空间换成你自己的）：

**例 1 · 编译测试**

> **你**：**帮我编译这个工程，然后只跑 MyNamespace 下的编辑器测试。**
>
> **AI**：好的，我先确认编辑器在线，打开后台工作开关，然后触发编译、等它完成，再跑测试。⏳ Unity 窗口可能短暂卡一下（脚本重载，正常现象）。
> **AI**：编译通过，12 个测试全部绿色 ✅（若有失败会贴出报错并主动分析原因）

**例 2 · 改场景资产**

> **你**：**把 Main 场景里的 Canvas 改名为 BattleCanvas。**
>
> **AI**：（自动走正规流程打开场景、重命名、保存，不会绕过保护直接改文件）已完成 ✅

AI 会自动做的事（都写在技能里，你无需操心）：开工前探活、打开后台工作开关防止 Unity 失焦卡死、新建脚本后触发导入、编译/测试的等待与轮询、失败后读错误详情决定重试还是报错。

<details>
<summary><b>手动模式（可选）</b>——不想用 AI 时自己敲，也是排查问题的方法</summary>

```powershell
upipe status                                        # 编辑器在线？
upipe exec set_autotick --set enable=true           # 后台工作前必开（防失焦卡死）
#   ... 编辑 .cs 文件 ...
upipe recompile                                     # 触发编译并等待完成
upipe test --mode editor --filter MyNamespace       # 跑测试
upipe eval "return 2 + 2;"                          # 快速求值
upipe commands                                      # 浏览当前编辑器支持的全部命令
```

自动化验收可加 `--require-match`，让测试筛选零命中时退出 `20`。测试启动前会检查所有打开的场景，有未保存的场景时返回其名称或路径。

`upipe test` 默认返回完整结果。`--detail failures` 只保留失败用例及其诊断，`--detail summary` 只保留汇总和阶段计数。两种精简模式都需要等待测试完成，成功或失败仍由测试结果决定。

`exec run_script` 执行失败、`exec batch` 事务回滚时退出 `20`，原始诊断和逐步结果在 `error.data`。后台任务的最终退出码由 `upipe job <id>` 返回。

自定义命令可用 `PipelineCommandResult` 返回结构化失败和分级日志。`upipe exec` 和 `upipe job` 接收 `--log-level` 与 `--log-format json`，命令日志在结果就绪时写到 stderr。连接本地开发 Player 时，提交、等待和取消任务均使用同一个 `--player-path`。用法见 [自定义命令文档](package/com.unity.pipeline/Documentation~/creating-commands.md#structured-outcomes-and-logs)。

`upipe recompile` 等待编译结果，失败时退出 `20`，编译诊断见 `error.data.errors`。`--no-wait` 只提交请求，使用 `recompile_status` 查看编译结果。

`upipe status` 的 `server.phase` 报告可识别的 Editor 阶段，`server.editorState` 包含 Auto Refresh 及会话内代码加载次数。写命令发出后连接丢失时，退出码为 `11` 且 `error.data.outcome` 为 `unknown`。先检查 Editor 状态或实际写入结果，再决定是否重新执行。

### 比较 Git 或 P4 中的 Unity 资产

这些命令均可在 Editor 关闭时使用。Git 默认从当前目录读取，也可用 `--project-path` 指定 Git 仓库内的 Unity 工程目录。

```powershell
upipe asset-guid --at REV GUID --json
upipe asset-refs --at REV --under Assets/UI GUID --json
upipe asset-check --at REV --under Assets/UI --json
upipe asset-impact --from OLD --to NEW --json
upipe asset-impact --at REV --json
upipe asset-diff --from OLD --to NEW Assets/UI/Screen.prefab --json
upipe asset-diff --include-inherited --from OLD --to NEW Assets/UI/Screen.prefab --json
```

P4 用已提交的数字 changelist 作为 `--at`、`--from` 或 `--to`。这些命令都支持以下读取参数。P4 Suite 负责认证和路径编码。

```powershell
upipe asset-guid --at CL GUID --p4-root //depot/Project --p4-ops PATH_TO_P4_OPS_JS --p4-port SERVER --p4-user USER --p4-client CLIENT --json
```

GUID 解析范围为选定版本中纳管的 `Assets/` 和 `Packages/`。报告区分已解析、未解析、重复 GUID、只有元数据的资源以及 Unity 内建引用。未纳管的注册表包引用可能无法解析。

`asset-refs` 查找该版本中每个文本 Prefab 和 Scene 直接保存的对象引用，返回资产路径、节点、组件、字段和引用 File ID。嵌套 Prefab 的源引用和已保存的对象引用覆盖分别出现在对应字段中。覆盖还提供目标 GUID、File ID 和属性路径。被删资源的 GUID 仍可用于定位残留引用。用 `--under` 指定目录或单个资产可以缩小搜索范围。未读取的二进制资源或 Git LFS 指针列在 `skippedFiles` 中，此时 `complete` 为 `false`。零匹配使用正常成功响应。

`asset-check` 报告 `Assets/` 中文件和目录缺失的 `.meta`，以及纳管的 `Assets/`、`Packages/` 中的无效元数据、孤立元数据和重复 GUID。它还列出文本 Prefab、Scene 中无法按快照解析的 GUID 引用及字段位置。来自注册表包等未纳管依赖的 GUID 也可能无法解析。

`asset-impact` 将资产和元数据的版本变更放在 `changes`，通过已保存的 GUID 引用找出直接和间接受影响的 Prefab、Scene。引用此前已失效且位置不变时不会列入 `newlyInvalidReferences`。移动资产会显示原路径移除、新路径新增，并用同一 GUID 关联引用。单版本模式 `--at` 对比 Git 第一父提交，或 P4 上一个可见的已提交 Changelist。显式 `--from` 和 `--to` 比较两个快照。P4 按文件修订比较，提交了相同内容也可能列为变更。可用 `--under` 限定所扫描的 Prefab、Scene，变更清单和 GUID 仍取自整个项目快照。影响链如果经过限定范围之外的资产，使用全项目扫描可以看到完整链路。

这两条命令以单个 `--json` 信封返回。完成扫描时退出 `0`，发现问题放在报告中。参数错误、读取失败和超时分别退出 `2`、`20`、`21`。二进制资源和 Git LFS 指针计入跳过清单。引用诊断验证资源 GUID 的解析状态，适用于版本库中可见的文本 Prefab 和 Scene。

差异支持 Unity 文本序列化的 `.prefab` 和 `.unity`，以类型和本地 File ID 配对节点及组件。同名兄弟节点按层级顺序标记，字段使用 JSON Pointer。嵌套 Prefab 实例以源资产名标识，实例的序列化覆盖和引用在实例下显示。资源移动会展示两边不同的路径，引用无法解析时保留原 GUID 和 File ID。资产本身改名可用 `--to-path` 指定新路径。

`--include-inherited` 递归读取两侧版本各自的源 Prefab，先应用内层覆盖，再应用外层覆盖。在额外的 `data.inherited.sources` 中，每条记录列出实例 File ID 链、源资产在两侧的状态和生效后的节点、组件、字段差异。材质槽和文本序列中的对象引用数组支持扩容、缩容、清空和按索引覆盖。已解析的本地对象引用返回源 File ID 和对应的 `instanceIds`。直接比较 Variant 时，`data.inherited.localNodes` 显示 Variant 添加到继承层级中的节点及其实际路径。源缺失时保留 GUID。无法可靠还原的覆盖（例如没有元素类型的紧凑编码数值数组）会列入 `issues`。

输入错误返回 `2`，版本读取或格式失败返回 `20`，读取超时返回 `21`。差异和未解析 GUID 是正常的查询结果，退出码为 `0`。

> 新建的 `.cs` 文件首次编译前需 `upipe eval "UnityEditor.AssetDatabase.Refresh();"` 触发导入——AI 技能已内置此处理，手动模式才需要你自己记。
</details>

### 升级（有新版本时）

```powershell
upipe pkg-status --project-path <project>           # 对比工程内版本 vs 仓库版本，再走 ② 安装
```

## 术语小词典

| 词 | 白话解释 |
|---|---|
| **upipe** | 本仓库出品的命令行小工具，AI 和你都用它指挥 Unity |
| **AI 助手 / Agent** | 会自己动手干活的 AI 软件，这里特指 AI Mana（Claude Code）和 Codex |
| **harness** | 上面那类 AI 软件的统称——给 AI 套上就能干活的一整套外壳 |
| **技能 skill** | 装进 AI 脑子的「upipe 使用说明书」，装完 AI 自己知道该敲哪些命令 |
| **规则模板** | 你要亲手贴进 AI 全局配置的「安全守则」：哪些工程能用哪些能力、哪些文件禁止直接改 |
| **适配包** | 让 Unity 2022 老工程听懂指令的插件（官方 com.unity.pipeline 的 2022 适配版） |
| **域重载** | Unity 改完代码自己重启一次脚本环境，窗口卡几秒属正常现象 |
| **P4 / Perforce** | 部分团队使用的版本控制工具；个人项目可忽略相关提示 |

---

## 给开发者

架构设计（为什么自维护、分层图、域重载、退出码契约）已拆至 **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**。本节只留速查。

### 命令面

| 命令 | 说明 |
|---|---|
| `status` | 探活：端口文件实例 + 服务健康 + 编辑器状态 |
| `commands` / `commands --full` | 服务端命令索引 / 含参数 schema |
| `exec <name> [--set k=v] [--params JSON] [--job]` | 主通道，任意命令直通；`--set` 值自动做 JSON 标量类型推断 |
| `help <name>` | 精确显示当前 Editor 或开发 Player 中一条命令的参数、默认值以及副作用。支持 `--json` |
| `commands --group-by tag` / `help --group <tag> [name]` | 按 tag 浏览或在指定 tag 子树中查找精确命令。也可用 `--group-by package` 按程序集分组 |
| `build [--dry-run|--confirm] [--no-wait]` | 校验目标、场景和输出路径，或构建 Player 并等待对应的 BuildReport |
| `dialog` | 从 Editor 读取已识别的模态对话框，主线程被阻塞时也可查询 |
| `eval "<code>"` / `eval-file <path>` | C# 求值，支持 `await` 和 `--dry-run`。用 `--job` 提交可取消的后台任务 |
| `recompile [--no-wait]` | 触发编译并轮询 `recompile_status` 至终态 |
| `test [--mode] [--filter] [--no-wait]` | `run_tests` 异步 + `test_status` 轮询 |
| `audit [--no-wait]` | Project Auditor 扫描 + 轮询 |
| `job <id>` / `job-cancel <id>` / `progress` | 分离式 job 模式与进度 |
| `pkg-status` / `pkg-install [--prune]` | 适配包版本查询 / diff 计划安装（P4 只读目标拒绝；`removed`=不在 tgz 里的残留文件，默认只报告不删，`--prune` 才实际删除） |
| `plugin install` | 安装 harness 技能到双 harness |
| `plugin-uninstall` | 对称卸载：技能目录移入 backup（不硬删）；`--remove-rules` 额外剥离 CLAUDE.md/AGENTS.md 里的管理块 |
| `instances [--all] [--prune]` | 列出本机已知的运行实例；`--prune` 清理登记表里已失效的工程路径 |
| `version` | CLI 版本 + git commit + 构建时间 + vendored 适配包版本 |

未封装的服务端命令一律走 `exec` 直通——这正是薄客户端契约：**子命令只封装等待策略，其余零改动跟进**（完整退出码表与信封格式见 [架构设计](docs/ARCHITECTURE.md)）。

### 异步脚本求值

```bash
upipe eval 'await Task.Delay(100, cancellationToken); return Application.unityVersion;' --json
upipe eval-file Scratch.cs --dry-run --json
```

`--dry-run` 返回编译诊断，且不会加载程序集或执行脚本。诊断的行和列从 0 开始，对应输入代码或文件。`await` 后默认回到 Unity 主线程。异步代码可将 `cancellationToken` 传给它等待的任务，响应求值超时或 `job-cancel`。代码需要主动配合取消，已经执行的副作用不会回滚。原同步调用保留既有回包。只编译和异步求值失败返回 `20`，到达求值时限返回 `21`，已取消的后台任务返回 `22`。

### 构建与卡点排查

```bash
upipe build --dry-run --target StandaloneWindows64 --scene Assets/Main.unity --output-path Builds/Game.exe --json
upipe build --confirm --target StandaloneWindows64 --scene Assets/Main.unity --output-path Builds/Game.exe --option Development --json
upipe dialog --json
```

`build` 默认等待同一 build ID 的报告。用 `--no-wait` 获取排队 ID，再查询 `exec build_status`。CLI 等待超时后，Editor 中的构建可能仍在运行。`--profile` 需要 Unity 6；使用时由配置决定构建目标和场景。Unity 2022 会明确拒绝该参数。

`dialog` 返回当前可识别的对话框。可检测性依赖 Editor 版本和平台，`available=false` 表示当前实例没有检测能力。Unity 2022 Windows 可报告已识别的原生模态标题。

### Profiler 排查

在运行同版本 Editor 的工程根目录录制或读取 `Library/Pipeline/Profiles/` 下的 `.data`。用 `profiler_targets` 找到开发 Player 的 `player_id`，省略该参数时录制 Editor。也可以在 Unity Profiler 中先直接连接，再选用 `profiler_targets` 标记为 `current` 的 Player。

```bash
upipe exec profiler_record --set file=perf.data --set player_id=PLAYER_ID --set gc_callstacks=true --set confirm=true
upipe exec profiler_record_status --json
# 如需提前保存，在录制期间调用
upipe exec profiler_record_stop --json
upipe exec profiler_session --set file=perf.data --set confirm=true --json
upipe exec profiler_summary --set session_id=SESSION_ID --set frame_budget_ms=16.67 --json
upipe exec profiler_rank_frames --set session_id=SESSION_ID --set top=10 --json
upipe exec profiler_frame_paths --set session_id=SESSION_ID --set frame=FRAME --json
upipe exec profiler_gc_callstacks --set session_id=SESSION_ID --set frame=FRAME --json
```

已有帧时，`profiler_record_stop` 保存到开始录制时指定的文件。重复调用返回同一份保存结果。首帧还未到达时，录制继续运行，可稍后再停止。`profiler_cancel` 结束录制并放弃保存。域重载会结束正在进行的录制。

用 `profiler_session` 返回的 `sessionId` 替换示例中的 `SESSION_ID`，用 `slowFrames` 或排序结果中的 `frame` 查看热点。会话复用当前已加载的历史。切换捕获或外部替换历史后，覆盖已有历史需显式传入 `confirm=true`。域重载或重启后要重新打开会话。`profiler_close --set session_id=SESSION_ID` 可注销会话，Profiler 窗口保留当前历史。排序、热点和 GC 栈仍支持原有的 `file` 参数。

`profiler_summary` 默认统计整份捕获。可用 `first_frame` 和 `last_frame` 指定含首尾的区间。帧时间超过预算才计入超预算帧，默认预算为 60 FPS 对应的帧时间。中位数和 P95 取升序数据中下标为 `floor((样本数 - 1) × 分位数)` 的值，超预算比例以有效帧为分母。GC 统计用有分配数据的帧计数，整个区间缺少 GC 数据时返回 `null`。

用 `metric=gc_bytes` 按录到的分配字节排序。帧内标记链来自 Profiler 样本树，GC 调用栈来自录制时保存的堆栈，未解析的符号仍带有地址。GC 调用栈需在录制时开启，会增加采样开销。

用 `profiler_threads` 查看某帧的线程 ID 和线程索引。用 `profiler_rank_markers` 汇总含首尾的 1–300 帧，按累计自身耗时排序，同名 marker 按线程分别统计。用 `thread_id` 限定线程，用 `--params '{"exclude_markers":["WaitForJobGroupID"]}'` 排除精确名称。将结果 `peak` 中的 `frame`、`threadIndex` 和 `sampleIndex` 分别传给 `profiler_frame_paths` 的 `frame`、`thread` 和 `sample_index`，即可读回原始标记链。

用 `profiler_counters` 枚举捕获里记录的非 GPU 计数器。传入 `counters` 可按名称或 `类别/名称` 查询统计，`per_frame=true` 同时返回逐帧值。值和统计使用 Unity 的原始单位，例如 `TimeNanoseconds` 或 `Bytes`。`available=false` 和 `value=null` 表示该帧没有可用的数值。用 `profiler_marker_info` 查询某帧的 marker 定义，包括类别、原生标志，以及 Unity 记录的元数据字段、类型和单位。

`totalMs` 是所有匹配样本的含子样本时长之和，嵌套样本可能重复计时。`coveredMs` 取同一线程、同一帧内时间区间的并集，再跨帧求和。不同线程可能并发，线程之间的时长不能相加作为经过时间。

本机内存快照要求构建 Development Player 时启用 Pipeline Runtime。启动后，`--player-path` 指向 Player 的工作目录。

```bash
upipe --player-path PLAYER_WORKING_DIR exec player_memory_snapshot --set file=player.snap --set confirm=true --json
upipe --player-path PLAYER_WORKING_DIR exec player_memory_snapshot_status --json
```

快照写入 Player 持久数据目录下的 `Pipeline/Profiles/`，完成回包包含完整路径及构建 GUID。

### 适配包维护（package/）

- **`0.6.0-2022.16`** 基于官方 `0.6.0-exp.1`（revision `27af91a9`），面向 Unity 2022.3。包括部分官方 1.0.1 的 Editor 生命周期、异步等待、Console、截图、Prefab 寻址和 Profiler 能力。详见 [package/ADAPTATION-2022.md](package/ADAPTATION-2022.md)。
- `pkg-install` 将适配包作为 embedded 包安装到工程，由 Unity Package Manager 从本地包加载。
- `upstream/` 原始基线（只读） · `com.unity.pipeline/` vendored 唯一真相源 · `patches/` 自动生成的适配补丁快照
- 构建：`go run ./package/build` → `dist/*.tgz` + SHA256（剔除 Tests/ 与失效签名）

### Harness 插件包（harness/）

三个技能（`unity-pipeline` / `unity-router` / `unity-mcp-bridge`）随仓库维护，一条命令安装到 AI Mana（`~/.claude/skills`）与 Codex（`~/.codex/skills`）双侧；AI Mana 当前会话即热加载，Codex 需新会话。安装后提示会引导贴规则模板（见「快速开始 ③」）。

- `unity-pipeline`：主力手册——编译、测试、改资产都靠它
- `unity-router`：前台调度——判断当前任务走哪条信道
- `unity-mcp-bridge`：外协守则——仅白名单工程适用的 MCP 信道使用规范

co-use 白名单机制、序列化资产红线、MCP 8080 契约均维持既有全局治理不变（规则模板见 [harness/rules/](harness/rules/README.md)，两侧须手动镜像）。

### 开发与测试

```powershell
go build ./...      # 构建
go vet ./...        # 静态检查
gofmt -l .           # 格式检查（CI 门禁；非空输出即 fail）
go test ./...       # 协议单元 + httptest mock 端到端 + 分层 lint
```

带版本信息的发版构建（`upipe version` 会回显）：

```powershell
go build -ldflags "-X github.com/Kayungko/unity-pipe/internal/commands.Version=v0.1.12 -X github.com/Kayungko/unity-pipe/internal/commands.Commit=$(git rev-parse --short HEAD) -X github.com/Kayungko/unity-pipe/internal/commands.BuildDate=$(Get-Date -AsUTC -Format o)" -o dist/upipe.exe ./cmd/upipe
```

**正式发版由 `Release` 工作流发布 GitHub 草稿。** 在 `main` 的发布提交上构建并验收 Windows CLI 与 UPM，保留验收原件。准备 Linux amd64、macOS amd64 和 macOS arm64 二进制，将五个产物及其 `.sha256` 和 `SHA256SUMS` 一起上传到以完整提交 SHA 为 target 的 `vX.Y.Z` 草稿。保留标签创建给发布步骤，在 Actions 中选择 `main` 运行 `Release`，填入版本及验收记录里的 Windows CLI 和 UPM SHA256。工作流核对提交的 CI、草稿和附件校验值，发布后回下载逐一比对。

Go 工具链 ≥ 1.23；无 CGO、无第三方依赖、无生成代码。

## 目录

```
unity-pipe/
├── cmd/upipe/          入口（只做分发）
├── internal/
│   ├── cli/            参数分发 · 信封渲染 · 退出码
│   ├── commands/       子命令等待策略
│   ├── protocol/       纯逻辑协议层（零 IO，移植边界）
│   └── transport/      net/http（唯一 IO 点）
├── package/            适配包：upstream 基线 + vendored + 补丁 + 构建器
├── harness/
│   ├── skills/         技能源（三技能）
│   └── rules/          全局规则模板（claude.md / agents.md + 变量表）
├── assets/             logo 与配图
├── docs/               ARCHITECTURE.md · PROTOCOL.md · MIGRATION.md · RECIPES.md · BACKLOG.md
└── layering_test.go    分层依赖 lint（强制执行）
```

## 文档

- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** — 架构设计：为什么自维护、客户端分层、域重载跟随、退出码契约
- **[docs/PROTOCOL.md](docs/PROTOCOL.md)** — Pipeline HTTP 协议实测参考（端点、端口文件、域重载行为、job 模式、双重编码陷阱）
- **[docs/MIGRATION.md](docs/MIGRATION.md)** — 从 rocwood / 官方 CLI 迁移；后续编辑器管理能力迁移路线
- **[docs/RECIPES.md](docs/RECIPES.md)** — Agent 实测配方：回包判据、三种守门模式、eval 禁忌、大规模普查策略（只收实测判据，未验证项单列）
- **[docs/BACKLOG.md](docs/BACKLOG.md)** — 深度评审沉淀的非阻塞项清单，完成一项划掉一项
- **[package/ADAPTATION-2022.md](package/ADAPTATION-2022.md)** — Unity 2022 适配全记录
- **[harness/rules/README.md](harness/rules/README.md)** — 规则模板使用说明与变量对照表

## 许可 ⚠️

- **upipe 代码**（`cmd/` `internal/` `harness/` `docs/` `scripts/`）：[MIT](LICENSE)
- **`package/` 内的 com.unity.pipeline**：版权归 Unity Technologies，**不适用 MIT**

> 🔴 **本仓库必须保持 PRIVATE**——含 Unity 包再分发内容，禁止转为 public；`dist/*.tgz` 仅经私有 Release 分发。公开分发 upipe 时需剥离 `package/`。详见 [LICENSE-NOTES.md](LICENSE-NOTES.md)。
