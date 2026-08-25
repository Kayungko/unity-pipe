<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
  <img src="assets/logo.svg" width="400" alt="upipe · Unity Editor Pipeline">
</picture>

**自维护的 Unity Pipeline 薄客户端 · Unity 2022 适配包分发 · AI Harness 插件包**

[![Go 1.23+](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white)](go.mod)
[![Unity 2022.3](https://img.shields.io/badge/Unity-2022.3-111827?style=for-the-badge&logo=unity&logoColor=white)](package/ADAPTATION-2022.md)
[![Zero dependencies](https://img.shields.io/badge/dependencies-zero-16A34A?style=for-the-badge)](#开发)
[![Agent ready](https://img.shields.io/badge/agent-ready-7C3AED?style=for-the-badge)](#直接跟-ai-说话)

[快速开始](#快速开始) · [跟 AI 说人话](#直接跟-ai-说话) · [术语表](#术语小词典) · [架构设计](docs/ARCHITECTURE.md) · [协议参考](docs/PROTOCOL.md) · [更新日志](CHANGELOG.md)

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

从零到「AI 帮我干活」共三步安装（都只需做一次）+ 日常使用。以 Windows + PowerShell 为例（按 `Win` 键输入 `powershell` 回车即可打开蓝色命令行窗口），目录按需替换。

### ① 安装 upipe（一次性）

**普通用户（推荐）**：直接下载现成的 exe，不需要装任何编程工具。

1. 到 [Releases](https://github.com/Kayungko/unity-pipe/releases) 页下载 `upipe.exe`，放进一个固定文件夹（例：`C:\Tools`）；
2. 把该文件夹加入 PATH（PATH 是 Windows 的「找程序名单」，加入后任何地方输 `upipe` 都能找到）：
   - `Win + I` → 搜索「**编辑账户的环境变量**」→ 环境变量 → 用户变量 `Path` → 新建 → 填 `C:\Tools` → 确定；
3. **重新打开**一个 PowerShell 窗口，输入 `upipe status` 验证——有输出即成功（提示找不到工程是正常的，说明 upipe 已可用）。

<details>
<summary><b>开发者：从源码构建</b>（需要 Git + Go 1.23+）</summary>

```powershell
git clone https://github.com/Kayungko/unity-pipe.git <你的目录>\unity-pipe
cd <你的目录>\unity-pipe
go build -o dist/upipe.exe ./cmd/upipe     # 产物同样加入 PATH
```
</details>

### ② 给 Unity 工程装适配包（每个工程一次）

适配包是让 **Unity 2022** 老工程也能听懂指令的插件（官方只支持 Unity 6+，本仓库自制了 2022 版）。

1. 从 [Releases](https://github.com/Kayungko/unity-pipe/releases) 下载 `com.unity.pipeline-*.tgz`（与 upipe 同页）；
2. 预览安装计划（把 `<project>` 换成你的 Unity 工程文件夹路径）：

```powershell
upipe pkg-install --project-path <project> --source <下载的tgz路径> --dry-run
```

3. 预览无误后去掉 `--dry-run` 正式安装，然后**打开该工程的 Unity Editor**（工程文件夹的 `Library/Pipeline/` 下会自动生成连接文件，无需手动操作）。

> 💡 提示中出现「P4 / p4 edit」是团队版本控制（Perforce）相关，个人项目直接忽略。

### ③ 教会你的 AI（一次性）

你的 AI 助手（**AI Mana / Claude Code** 或 **Codex**，下称 harness）出厂时并不认识 upipe。安装「技能」就是给它塞一本使用说明书——**装完你就不需要再敲任何命令**。

```powershell
# 在 unity-pipe 仓库目录内执行（或从 Release 下载的仓库副本内）
upipe plugin install
```

这条命令会把三个技能装进 AI Mana（`~/.claude/skills`）和 Codex（`~/.codex/skills`）双侧。如果你以前装过旧版，会自动备份到旁边的 `skills-backup/` 文件夹（带完整性校验），随时可找回，不怕覆盖丢失。

**技能 ≠ 全部——还差一步贴「安全守则」**：

技能是「说明书」，治理规则（哪些工程允许用哪些能力、哪些文件禁止 AI 直接改）住在 AI 的全局配置文件里，安装命令不能替你改它。**只装技能不贴规则，AI 就像拿了工具但没拿到安全守则**——不知道「场景 / Prefab / 材质等文件禁止直接改、必须走正规流程」这条红线，可能绕过保护直接动手改工程文件。

1. 打开 [harness/rules/](harness/rules/README.md)，AI Mana 用户取 `claude.md` 模板，Codex 用户取 `agents.md`；
2. **把模板里所有 `{{...}}` 占位符替换成你机器的真实值**（对照变量表：仓库路径、upipe.exe 路径、白名单工程路径；没有白名单工程就整段删掉 co-use 条目，AI 会自动按最保守方式工作）——**直接照抄 `{{}}` 会让规则失效**；
3. 用记事本打开对应全局文档（AI Mana：`C:\Users\<你>\.claude\CLAUDE.md`；Codex：`C:\Users\<你>\.codex\AGENTS.md`），把替换好的内容贴到文末（或对应章节）。

**装后验证清单**：

- [ ] `C:\Users\<你>\.claude\skills`（或 `.codex\skills`）里各多出 `unity-pipeline` / `unity-router` / `unity-mcp-bridge` 三个文件夹
- [ ] 重开一个新对话（**Codex 必须重开才生效**；AI Mana 当前窗口即可）
- [ ] 冒烟测试：把这句发给 AI——`帮我探活一下 Unity 编辑器`——它调用 `upipe status` 并报告 ready，即全链路打通 ✅

### 直接跟 AI 说话

装完之后的使用方式就一种：**打开 AI 对话框，用大白话提需求**。下面加粗的两句现在就能直接复制发给 AI（把工程名、命名空间换成你自己的）：

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
| `eval "<code>"` / `eval-file <path>` | C# 求值（`eval_file` 参数名映射为 `file`） |
| `recompile [--no-wait]` | 触发编译并轮询 `recompile_status` 至终态 |
| `test [--mode] [--filter] [--no-wait]` | `run_tests` 异步 + `test_status` 轮询 |
| `audit [--no-wait]` | Project Auditor 扫描 + 轮询 |
| `job <id>` / `job-cancel <id>` / `progress` | 分离式 job 模式与进度 |
| `pkg-status` / `pkg-install` | 适配包版本查询 / diff 计划安装（P4 只读目标拒绝） |
| `plugin install` | 安装 harness 技能到双 harness |

未封装的服务端命令一律走 `exec` 直通——这正是薄客户端契约：**子命令只封装等待策略，其余零改动跟进**（完整退出码表与信封格式见 [架构设计](docs/ARCHITECTURE.md)）。

### 适配包维护（package/）

- **`0.5.0-2022.1`**：基于官方 `0.5.0-exp.1`（upstream revision `17df0ac8`），9 文件适配 Unity 2022.3（详见 [package/ADAPTATION-2022.md](package/ADAPTATION-2022.md)）
- 版本命名策略：semver 预发布段 `"2022.1" > "exp.1"`，UPM 视 vendored 版本更高，**不会被 registry 误"升级"覆盖**
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
go test ./...       # 协议单元 + httptest mock 端到端 + 分层 lint
```

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
├── docs/               ARCHITECTURE.md · PROTOCOL.md · MIGRATION.md
└── layering_test.go    分层依赖 lint（强制执行）
```

## 文档

- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** — 架构设计：为什么自维护、客户端分层、域重载跟随、退出码契约
- **[docs/PROTOCOL.md](docs/PROTOCOL.md)** — Pipeline HTTP 协议实测参考（端点、端口文件、域重载行为、job 模式、双重编码陷阱）
- **[docs/MIGRATION.md](docs/MIGRATION.md)** — 从 rocwood / 官方 CLI 迁移；后续编辑器管理能力迁移路线
- **[package/ADAPTATION-2022.md](package/ADAPTATION-2022.md)** — Unity 2022 适配全记录
- **[harness/rules/README.md](harness/rules/README.md)** — 规则模板使用说明与变量对照表

## 许可 ⚠️

- **upipe 代码**（`cmd/` `internal/` `harness/` `docs/` `scripts/`）：[MIT](LICENSE)
- **`package/` 内的 com.unity.pipeline**：版权归 Unity Technologies，**不适用 MIT**

> 🔴 **本仓库必须保持 PRIVATE**——含 Unity 包再分发内容，禁止转为 public；`dist/*.tgz` 仅经私有 Release 分发。公开分发 upipe 时需剥离 `package/`。详见 [LICENSE-NOTES.md](LICENSE-NOTES.md)。
