# 全局路由规则模板（harness/rules/）

`upipe plugin install` 只安装技能本体；**治理规则**（主信道声明、co-use 白名单、序列化资产红线）住在各 harness 的**全局文档**里，技能通过名称引用它们：

| Harness | 全局文档 | 段落 | 模板 |
|---|---|---|---|
| AI Mana / Claude Code | `~/.claude/CLAUDE.md` | §13 Unity CLI / Pipeline 操作规范 | [`claude.md`](claude.md) |
| Codex | `~/.codex/AGENTS.md` | 6.4 Unity CLI / Pipeline 操作规范 | [`agents.md`](agents.md) |

## 一键应用（推荐）

```bash
upipe plugin install --apply-rules [--co-use-whitelist "E:\P4\SGAME_*"] [--co-use-depot //sgproj/trunk]
```

装技能的同时把两份模板渲染进全局文档：本机路径类占位符（仓库、upipe.exe、UNITY-GUIDE）自动填充，co-use 白名单来自 `--co-use-*` 参数（缺省写入 pipeline-only 安全默认）。行为契约：

- 追加内容用 `<!-- unity-pipe:rules ... -->` 标记包裹，**重复执行自动跳过**（幂等）；
- 已有全局文档**改前自动备份**（同目录 `<文件名>.upipe-bak-<时间戳>`）；
- 全局文档此前已手动贴过规则（无标记）时同样按追加处理——旧内容保留在标记块之前，请自行取舍；
- 只想预览：加 `--dry-run`。

手动贴模板（老办法，适合精细控制章节位置）仍按下方变量表操作。

## 已部署机器的后续操作

### 场景 1：早期版本只装了技能，现在补规则（v0.1.2 及之前）

直接重跑同一条命令即可——技能重装是安全的（旧版自动备份到 `skills-backup/`），规则追加是幂等的：

```bash
upipe plugin install --apply-rules [--co-use-whitelist "E:\P4\SGAME_*" --co-use-depot //sgproj/trunk]
```

若全局文档里此前**手动贴过**规则（无标记块）：命令会追加一个新的标记块，旧内容保留在前面——确认新块无误后请手动删除旧段落（`.upipe-bak-*` 备份里有原始版本）。

### 场景 2：白名单事后变更（装时没传 / 换工程了）

命令不会改写已写入的规则块。直接编辑全局文档中 `<!-- unity-pipe:rules ... -->` 标记内的这一行：

```
当前白名单：`E:\P4\SGAME_*`（depot `//sgproj/trunk`）
```

改为新的 glob 即启用/收窄 co-use；想全部回到 pipeline-only，把该行改回「未配置」表述或整段删除 co-use 条目。**两侧全局文档（CLAUDE.md 与 AGENTS.md）都要改**——见下方镜像要求。

### 场景 3：仓库规则模板升级了，已部署机器要跟进

命令**不会**更新已写入的规则块（幂等跳过就是为保护你的手动修改）。升级步骤：

1. 重跑命令确认 `already-managed`（无副作用，仅确认现状）；
2. 手动删除两份全局文档中的旧标记块（从 `begin` 到 `end` 整块）；
3. 重跑 `upipe plugin install --apply-rules ...`——按最新模板重新写入。

> 或者不删旧块：对比 [claude.md](claude.md) 模板与标记块内容，手动把变化合并进去（适合已在块内做过自定义的机器）。

## 模板变量

模板中的 `{{...}}` 占位符按机器替换：

| 占位符 | 含义 | 示例值 |
|---|---|---|
| `{{UNITY_PIPE_REPO}}` | 本仓库克隆路径 | `E:\unity-pipe` |
| `{{UPIPE_BIN}}` | upipe 可执行文件路径 | `C:\Users\admin\bin\upipe.exe` |
| `{{CO_USE_WHITELIST}}` | 启用双信道（co-use）的工程路径 glob | `E:\P4\SGAME_*` |
| `{{CO_USE_DEPOT}}` | 白名单工程的 P4 depot | `//sgproj/trunk` |
| `{{UNITY_GUIDE}}` | 全局 Unity 操作规范路径 | `~/.codex/UNITY-GUIDE.md`（Codex） / `C:\Users\admin\.codex\UNITY-GUIDE.md`（Claude） |

> 白名单留空或整段删除 co-use 条目 = 全部工程 pipeline-only（安全默认；技能的 §0 兜底逻辑会自动落到这个行为）。

## 两侧必须手动镜像

两份模板语义逐条对应（同一规则的两个 harness 表述）。修改任一侧后**必须手动同步另一侧**——两套全局文档不会自动同步。

## 规则与技能的边界

- **技能**（`harness/skills/`）：怎么用——命令语法、退出码分支、轮询策略。自包含，不依赖本目录。
- **规则**（本目录）：什么时候用、禁止什么——主信道默认、co-use 白名单触发条件、序列化资产红线、MCP 写工具 dry_run。unity-router / unity-mcp-bridge 两个技能引用这些规则做信道判定。
