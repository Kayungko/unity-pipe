# com.unity.pipeline Unity 2022 适配记录

上游基线：`com.unity.pipeline` **0.5.0-exp.1**（Unity Registry tgz，upstream revision `17df0ac8d6533a8c22830c89dc85a24c01f30ade`），官方要求 Unity 6.0+。

本仓库 vendored 版本：**0.5.1-2022.1**（适配 Unity 2022.3.23f1，已在真实工程端到端验证）。

版本命名依据：semver 预发布段字典序 `"2022.1" > "exp.1"`，UPM 视 vendored 版本高于 registry 版本，不会被"升级"覆盖。

## 完整改动清单（upstream → vendored，git diff 快照见 patches/0001-adapt-unity-2022.patch）

共 9 个文件：

| # | 文件 | 改动 |
|---|---|---|
| 1 | `package.json` | `unity: 6000.0 → 2022.3`；`version: 0.5.0-exp.1 → 0.5.0-2022.1` |
| 2 | `Runtime/Unity.Pipeline.asmdef` | 移除 `Unity.InputSystem` 引用（代码本身有 `ENABLE_INPUT_SYSTEM` 守卫，相关命令在 2022 降级为 unavailable） |
| 3 | `Editor/Commands/Assets/AssetCommands.cs` | `#if !UNITY_6000_0_OR_NEWER` 守卫，回退 `PhysicsMaterial = PhysicMaterial` 别名 |
| 4 | `Editor/Commands/Materials/MaterialCommands.cs` | `rawRenderQueue` 守卫回退 `renderQueue` |
| 5 | `Editor/Commands/ProjectSettings/GraphicsSettingsCommands.cs` | `defaultRenderPipeline` 守卫回退 `renderPipelineAsset`（上游 README 未提及，是额外发现的 U6 API） |
| 6-9 | `Runtime/Plugins/CodeAnalysis/*.dll.meta` ×4 | PluginImporter v3 列表格式 → v2（GUID 保留） |

## 已知取舍

- **Tests/ 未部署**：上游测试引用 `PhysicsMaterial`/`rawRenderQueue`/`InputSystem.TestFramework`，移植成本高。vendored 树保留 Tests 供未来适配参考；`build/` 打 tgz 时剔除，`upipe pkg install` 同样跳过。
- **.cs.meta 的 MonoImporter 段**：Unity 2022 导入时自动在 `.cs.meta` 追加 `MonoImporter:` 段（upstream 的裸 v2 + guid 是合法的），属导入副作用，非适配改动。
- `.signature` / `.attestation.p7m`：上游签名文件对本地修改后的包已失效，分发包中剔除（vendored 树保留作溯源）。

## 验证记录

- 2026-08-20：SGAME（Unity 2022.3.23f1，E:\P4\SGAME_o_jiarongke_6099）编译零错误 + `editor_status` ready + 7800-7849 端口可达。
- 2026-08-25：`upipe`（本仓库自研 CLI）对该实例全命令面实测通过（status/commands/exec/eval/recompile/test/progress）。

## 上游升级流程

1. 下载新版本 tgz → 覆盖 `package/upstream/`（一次性 commit）。
2. 将 `patches/` 中的适配重新应用到新树（冲突处人工审读 API 差异）。
3. 版本号递增（如 `0.6.0-2022.1`），更新本文件与 `patches/` 快照。
4. `go run ./package/build` 重打 tgz，跑冒烟回归。
