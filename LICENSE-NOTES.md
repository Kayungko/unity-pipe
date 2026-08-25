# 许可说明 / License Notes

## upipe 代码（`cmd/`、`internal/`、`harness/`、`docs/`、`scripts/`、构建脚本）

MIT License（见 LICENSE），Copyright (c) 2026 Kayungko。

## `package/` 目录 — com.unity.pipeline

`package/upstream/` 与 `package/com.unity.pipeline/` 的内容来自 **Unity Technologies** 的
`com.unity.pipeline` 0.5.0-exp.1（UPM 包，内部 git revision `17df0ac8d6533a8c22830c89dc85a24c01f30ade`），
其版权与许可归 Unity Technologies 所有，**不适用本仓库 MIT 许可**。

本仓库对其的修改（Unity 2022 适配，见 package/ADAPTATION-2022.md）仅限私有内部使用：

- **本仓库必须保持 private，禁止转为 public**（再分发受 Unity 许可条款约束）。
- `dist/*.tgz` 仅通过 GitHub 私有 Release 分发给有权限的成员。
- 若需要公开分发 upipe 本体，剥离 `package/` 目录后单独发布。

## harness/skills

skill 文档由本仓库撰写（部分内容衍生自 com.unity.pipeline 包内附带的
`.claude/skills/unity-pipeline/SKILL.md` 与既有个人全局技能），随 upipe 一并按 MIT 提供。
