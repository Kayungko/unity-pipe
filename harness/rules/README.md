# 全局路由规则模板（harness/rules/）

`upipe plugin install` 只安装技能本体；**治理规则**（主信道声明、co-use 白名单、序列化资产红线）住在各 harness 的**全局文档**里，技能通过名称引用它们：

| Harness | 全局文档 | 段落 | 模板 |
|---|---|---|---|
| AI Mana / Claude Code | `~/.claude/CLAUDE.md` | §13 Unity CLI / Pipeline 操作规范 | [`claude.md`](claude.md) |
| Codex | `~/.codex/AGENTS.md` | 6.4 Unity CLI / Pipeline 操作规范 | [`agents.md`](agents.md) |

新机器部署或分享给同事时，装完技能后把对应模板贴入全局文档的对应章节即可完成整套部署。

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
