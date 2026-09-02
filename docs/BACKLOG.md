# BACKLOG

深度评审（2026-08-26，工作流 wf_e6f1a7aa-795，29 项发现）沉淀的非阻塞项。评审文档见会话存档 `deep-review-20260826-0250.md`。完成一项划掉一项（改为 ~~删除线~~ 并注明落地版本）。

## P2 — 应改进

- [ ] **create_prefab 守门不查未导入磁盘文件**：守门用 `AssetDatabase.LoadAssetAtPath`，磁盘上存在但未导入的 `.prefab` 会被无 confirm 覆盖（`Editor/Commands/Prefabs/PrefabCommands.cs`）。改用 `File.Exists` 补查。
- [ ] ~~**apply_prefab_overrides 的 confirm 描述与事实相反**~~：已随场景族守门批次修复（描述改为如实说明资产写回语义）——见 2026-08-26 场景守门提交。
- [ ] **tgz 解压无资源上限**：`pkg.go` 的 `io.ReadAll(tr)` 无每条目/总大小上限，恶意 gzip bomb 可耗尽内存/磁盘（对比 transport 层有 `LimitReader`）。dry-run 也全量读入。
- [ ] **evalToken 端口文件 ACL fail-open**：权限收紧失败只打 warning 继续；且先写文件再收紧有 TOCTOU 窗口（`Runtime/Models/InstanceDescriptor.cs`）。
- [ ] **applyPlan 无视 Unchanged 分类全量重写**：每次安装把字节相同的文件原样重写（`internal/commands/pkg.go`）。
- [ ] **pkg-install 只增改不删除**：tgz 中已不存在的文件永久残留（未来上游删 .cs 后旧脚本残留可致编译冲突）。
- [ ] ~~**SKILL.md §0.5 / RECIPES.md §2 守门表未纳入 prefab confirm 语义**~~：0.1.10 / 0.5.3 已补；0.6.0-2022.1 文档继续保留。
- [ ] ~~**包内 Documentation~/commands/prefabs.md 对 7 命令 confirm 零记载**~~：0.6.0-2022.1 已把 create/variant 覆盖守门写入 prefabs.md（其余 5 个仍是 parity 保留，描述在命令参数上）。
- [ ] **InstancesCommand 零测试**：TCP 探活、心跳龄、--all 残留检测（`internal/commands/instances.go`）。
- [ ] **set_serialized_field.value 的 ArbitraryKeys 豁免未测**：JToken 转换分支与已测的 JObject 不同（`Tests/Editor/Scripts` 缺用例）。

## P3 — 低优先

- [ ] FindProjectRoot 盘符根边界：起点 `C:\` 直接 not-found；从 `C:\a` 上溯产生 `C:`（盘符相对路径）而非 `C:\`（`internal/protocol/discovery.go`）。
- [ ] shader 属性恰叫 `token` 时被接缝剥离且不进 unknown[]（set_material_properties 未标 ArbitraryKeys——语义边界待定：shader 属性名空间 vs 用户命名空间）。
- [ ] 监听为通配绑定 + 应用层 loopback 检查（非 socket 级 loopback-only）。
- [ ] Origin 拒绝测试被 `[Ignore("6.0 Issues")]` 长期禁用——安全控制无回归覆盖。
- [ ] cli 与 commands 两层各有一份 flag 重排近似实现（reorderForFlags vs parseArgs）——双份维护漂移风险。
- [ ] README 多处仍引用 0.5.0-2022.1 与"9 文件适配"。
- [ ] CHANGELOG 底部 compare 链接停在 v0.1.5（缺 0.1.6/0.1.7）。
- [ ] Documentation~ 全目录零覆盖 ArbitraryKeys 与 token 接缝剥离语义。
- [ ] pkg-install 剔除集合与 package/build 不一致（Tests.meta/.signature/.attestation 会写入非本仓构建的 tgz）。
- [ ] upipe.exe 与包版本间无协议兼容协商（PROTOCOL.md 无 enforced 契约）。

## 评审推翻项（记录备考，无需行动）

- ~~isUnsafeEntryName 漏防反斜杠穿越~~——调用点 `ToSlash` 已归一化，验证员实测全部恶意形态被拒。
- ~~exec 主通道零测试~~——`TestExecEndToEnd` 覆盖了 coerceScalar 契约（跨包 e2e）。

## 批判员盲区项（规划期新增）

- [ ] pkg-uninstall / 安装回滚面缺失（对生产工程注入的 manifest/Samples 变更无撤销手段）。
- [ ] 用户侧升级路径端到端演练（旧 CLI 撞新包的降级行为未定义）。
- [ ] token 泄漏面审计：CLI 错误输出/日志是否会把 token 或工程路径打进可转发文本。
- [ ] job 列举端点（GET /api/jobs）+ SKILL/RECIPES 暴露 --job 用法。
- [ ] eval --file/--stdin 通道 + stdout 捕获接通（RECIPES §3 实测痛点）。
