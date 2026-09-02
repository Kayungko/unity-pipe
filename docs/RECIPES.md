# Agent 实测配方（SGAME 2022 工程验证版）

> 2026-08-25 在真实生产工程（Unity 2022, P4 工作区, 820 UI prefab, 142 命令面）实测沉淀。
> 配套验证记录：AI Mana cowork `2026-08-25/20260825-recipes-validation`（batch1-readonly.md / batch2-write.md / ui-prefab-analysis.md）。
> 本文只收录**实测已验证**的判据；推断/未验证项在文末单独列出。

## 1. 回包判据（最容易踩的坑）

`--json` 输出双层信封。各命令的**业务返回值位置不统一**：

| 命令类 | 业务值位置 | 示例 |
|---|---|---|
| `eval` | `data.result.result`（二层嵌套） | `d["data"]["result"]["result"]` == 返回值 |
| 其他 exec 命令 | `data.result`（可能是 dict 或 JSON 字符串，需二次 `json.loads`） | `get_scene_hierarchy` |
| 写命令守门拒绝 | `result.status` / `result.applied` / 外层 `error`，见 §2 | |

注意（数组字段名逐命令核对，别猜）：
- `eval` 回包里的 `output` 字段**恒为 null**（留给 stdout），别读它。
- `get_scene_hierarchy` 的节点数组字段是 **`roots`**（不是 `nodes`）。
- `find_gameobjects` 的结果数组字段是 **`gameObjects`**（+`count`）——实测正常工作（此前误报"name 过滤不命中"是调用方读错了字段名）。

## 2. 三种守门模式（写命令拒绝的判据必须三查）

| 模式 | 命令类 | 表现 | 判据 |
|---|---|---|---|
| A 内层 status | `build` / `bake_*` | 外层 `ok=true`, exit 0 | `result.status=="error"` + message 含 "Refused" |
| B 外层信封 | `clear_*` / `delete_asset` / `switch_build_target` / `create_prefab`·`create_prefab_variant`（覆盖已有资产）/ `open_scene`·`create_scene`（非 additive 脏场景或覆盖）/ `remove_scene_from_build` | 外层 `ok=false`, `error.code=20`, `kind=CommandFailed` | `error.message` 含 "confirm" |
| C 内层 applied | `set_*_settings` | 外层 `ok=true` | `result.applied==false` + message 含 "Refused" |

**统一规则：写命令成功 = 外层 `ok=true` 且 `result.applied==true` 或 `result.status` 非 `"error"`，双条件缺一不可。**

补充：
- `set_*` 有内建 diff 检测：值不变时 `applied=false, message="No changes specified"`——幂等，不是失败。
- `set_*` 支持 `dry_run=true` 预览（实测 `applied=false, dryRun=true`，磁盘未变）。
- **任何写命令先 dry_run**；不支持的（delete_asset 等）靠守门反证兜底。

### 2.1 0.5.2+ 守门拒绝消息签名与恢复动作（源码确证）

| 拒绝消息特征 | 守门 | 恢复动作 |
|---|---|---|
| `Refusing to overwrite existing prefab/scene '...'` | 覆盖守门（create_prefab / create_prefab_variant / create_scene） | 从消息确认目标路径无误 → 重发带 `confirm=true` |
| `'...' would replace open scene(s) with unsaved changes: {场景名清单}` | 脏场景守门（open_scene / create_scene 非 additive） | `save_scene`/`save_all` 保存后重发；或显式 `confirm=true` 丢弃（不可恢复） |
| `Refusing to remove a scene from the build list` | build 列表守门（remove_scene_from_build） | 确认后重发带 `confirm=true` |
| `cannot run while the editor is in (or entering) play mode` | play 模式守门（全部场景写命令） | `editor_stop` 后重试 |

- **脏场景恢复流配方**：`list_open_scenes` 查 `isDirty` → 有脏场景先 `save_scene <path>` 或 `save_all` 再原命令重试。注意 `save_all` 对从未保存过的无名脏场景会拒绝（先 `create_scene <path>` 落盘）；additive 打开不替换场景、不触发守门。
- **安全覆盖资产三步配方**：写资产前先查存在性（`find_assets` 或 eval `File.Exists`）→ 首调无 confirm 预期被拒（守门不是故障）→ 从拒绝消息核对路径 → 重发带 `confirm=true`。
- **批量 confirm 三档语义**：真守门（create_prefab/create_prefab_variant 覆盖、场景族）必须按需 true；parity 保留（instantiate/apply/revert/unpack/save_prefab_contents，描述写 "not required"）无需传；**禁止无差别给所有写命令塞 confirm=true**——会静默解锁覆盖守门。批量循环把"被拒"当正常分支处理而非中止整个批。

## 3. eval 配方（Roslyn 动态编译）

### 3.1 禁忌（实测踩坑）

1. **禁用 `System.Tuple`**：工程程序集（如 ExCSS.Unity）与 mscorlib 的 Tuple 冲突 → CS0433。深度优先遍历用 `Dictionary<Transform,int>` 存深度。
2. **禁用 `\n` 转义**：bash→python→C# 转义链断裂 → CS1010 "Newline in constant"。输出分隔符用 `|`。
3. `GameObjectUtility.GetMonoBehavioursWithMissingScriptGUID` 在 2022 **不存在**（CS0117）；missing script 用 `GetComponents<Component>()` null 计数。
4. 多行代码可用：递归 lambda（`System.Action<T,string> scan=null; scan=(t,p)=>{...}; scan(root,"(root)")`）实测可用。

### 3.2 只读 prefab 拆解（配方模板）

```csharp
var root = UnityEditor.PrefabUtility.LoadPrefabContents(@"<绝对路径>");
if (root == null) return "LOAD_NULL";
try {
    // 遍历统计：GO 数/深度/组件数/missing（null 组件）
    return "GOs=" + count + ";depth=" + maxDepth + ";missing=" + missing;
} finally { UnityEngine.Object.DestroyImmediate(root); }   // 必须 finally 释放
```

不调 `SaveAsPrefabAsset` 即为纯只读（内存副本）。

### 3.3 missing script 精确定位（实测输出示例）

```
(root)/Node_SafeArea/m_ScrollRect_AlbumTest/RectMask2D_Viewport/Content [comp#2]
```

hierarchy path + 组件索引，直接对应 Lua 绑定命名。239ms / 215 节点 prefab。

## 4. 资产/场景写面规则

### 4.1 场景对象命令参数表（源码实测：`GameObjectCommands.cs`）

| 命令 | 参数 |
|---|---|
| `rename_gameobject` | `target`（Required, ObjectRef）+ `name`（Required, string） |
| `delete_gameobject` | `target`（Required, ObjectRef） |
| `set_transform` | `target`（Required）+ `position`/`rotation`/`scale` 各为 **float 数组 `[x,y,z]`**（不是 `{"x":..}` 对象！） |
| `set_parent` | `target` + `parent`（可省=移到场景根）+ `world_position_stays`（默认 true） |
| `create_gameobject` | `name` + `primitive`（可省）+ `parent`（ObjectRef，可省） |

`target` 是 ObjectRef，裸字符串按优先级解析：`GlobalObjectId_V1…` → `Assets/…` 资产路径 → `guid:…`/32位hex → `instanceId:…`/纯整数（含负数）→ **其余（含 `/Name`）= hierarchy path**。`create` 返回的 `instanceId` 与 `/Name` 形式均有效。

### 4.2 ⚠️ Git Bash 路径改写坑（MSYS path mangling）——实测根因

在 Git Bash 下传 `--set target=/Name`，MSYS 会把开头 `/` 当 POSIX 路径**自动改写**成 `C:/Program Files/Git/Name`，包端收到的是废路径（报 "No GameObject at hierarchy path 'C:/Program Files/Git/…'"）。**这不是包的 bug**。修复任选其一：

```bash
MSYS_NO_PATHCONV=1 upipe exec delete_gameobject --set target=/Name ...   # 方式1：环境变量
upipe exec delete_gameobject --set target=//Name ...                     # 方式2：双斜杠（MSYS 转义）
```

PowerShell / cmd 无此问题。instanceId（纯数字）不受影响——这也是它在 Git Bash 下"看起来更可靠"的原因。

### 4.3 写面纪律

1. **探针模式**：create → 测试 → delete → hierarchy + 磁盘双重复核残留。删除资产后连 `.meta` 一起消失（实测干净）。
2. `get_authoring_root` 先查裸路径解析根（实测某工程为 `Assets`，未收紧）。
3. P4 工程：upipe 永不代跑 p4；探针资产用 `__upipe_probe_*` 命名，测试完立即 `delete_asset --set confirm=true`。

## 5. 大规模 prefab 普查：双轨策略（820 个实测）

- **本地 YAML 解析做全量普查**（秒级）：块拆分 `--- !u!1 &id`，统计 GO 数/深度/组件/missing（`m_Script` GUID 与全工程 `.cs.meta` GUID 对账——**映射范围必须含 Assets+Packages 全部**，只扫 Assets/Scripts 会把 810/820 误判 missing）。
- **eval LoadPrefabContents 抽查做精确验证**：含嵌套 prefab 时 YAML 计数偏低（嵌套子树不展开），eval 是权威（真实实例树全展开）。missing 判定两边一致。
- **废弃反推**：C# 源码 + Lua 字节码（bytes 二进制按 UTF-8 子串搜）+ LuaConfig 完整名三路搜索。Lua 视图层常在 `LuaBytes/GameScripts/UI/`；字节码只存片段常量（搜 `Activity_` 前缀命中、完整名不命中）→ **运行时拼接加载静态不可排除，整系列零引用也不能批量删**。

## 5.5 长任务 detached jobs（build/烘焙/audit 超时必用）

- 提交：`upipe exec <cmd> --job` 或 `upipe eval "…" --job`（立即返回 jobId）；等待：`upipe job <id> --timeout 45m`（长烤给足预算）；取消：`upipe job-cancel <id>`（HTTP body 字段是 **`id` 不是 `jobId`**）。
- 状态机：`queued | running | completed | failed | canceled`；退出码 **22 = job cancelled**。
- 限制：jobs 严格串行（同时上限 100，超出 429）、**不活过 domain reload**（重编译后 jobId 失效）。

## 5.6 冷启动 settling 与 test 轮询判据

- **settling**：冷导入后首命令遇 exit 13 / "still settling" 是编辑器在导入，不是命令失败——轮 `upipe status` 至 `ready` 再重发。
- **test 轮询**（0.5.2+ 修复后）：`run_tests` 启动即写 `status=running`（含 mode/filter）。健康判据 = 轮询回包的 filter/mode 与本次提交一致 + 状态迁移 `running→completed`；若始终无 running 态应怀疑 run 未启动（主线程派发超时），而不是误读上一轮结果（修复前换 filter 也返回同一份旧数据的历史缺陷已消除）。

## 6. 已知限制与未验证项

- `exec search`：全库查询（`t:Canvas`）实测 >2min 阻塞；`h:` 语法无效。**资产搜索首选 `eval + AssetDatabase.FindAssets`（可限定目录，秒回）**。
- 守门三模式是从代表性命令归纳，未逐一测完 142 个命令；0.5.2 新守门族（prefab/场景）的信封形态为同源代码路径推断（模式 B），逐命令实测待补。
- create_prefab 覆盖守门用 `AssetDatabase.LoadAssetAtPath` 判存在——磁盘存在但未导入的 .prefab 不受守门保护（源码确证，BACKLOG 已录）。
