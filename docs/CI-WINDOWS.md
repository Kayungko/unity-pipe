# Windows Runner CI

将 [模板](../examples/ci/unity-windows.yml) 复制到 Unity 项目仓库的 `.github/workflows/unity-windows.yml`，将 [同一运行脚本](../scripts/run-unity-ci.ps1) 复制到 `scripts/run-unity-ci.ps1`。

模板通过 `workflow_dispatch` 手动运行，在带 `unity-2022` 标签的 Windows x64 自托管 Runner 上使用 PowerShell 7.4 或更高版本。Runner 需装好 Unity 2022.3.23f1、Windows 构建模块和 upipe，并能以 Runner 账户启动已授权的 Editor。Unity 项目需装好 Pipeline 包。请在 Runner 上构建你信任的仓库和引用。

在 GitHub 仓库变量中配置：

- `UNITY_EXE` 指向 Runner 上的 `Unity.exe` 绝对路径。
- `UPIPE_EXE` 指向 Runner 上的 `upipe.exe` 绝对路径。
- `UNITY_PROJECT_PATH` 指向仓库内的 Unity 项目目录，默认为仓库根目录。

默认运行全部 EditMode 和 PlayMode 测试。提供 `affected_from` 和 `affected_to` 可按 Git 版本区间选测。启用 `require_match` 后零匹配返回 `20`。启用 `build` 后，测试通过才会构建 Windows Player，默认用项目 Build Settings 中启用的场景。

也可在 Runner 上直接调用同一脚本：

```powershell
pwsh -File ./scripts/run-unity-ci.ps1 -Upipe 'C:/Tools/upipe.exe' `
  -Unity 'C:/Unity/Editor/Unity.exe' -ProjectPath './Game' `
  -ArtifactDirectory 'C:/CI/Unity/job-123' -Build -Scene 'Assets/Main.unity' `
  -TestTimeout 15m -BuildTimeout 30m
```

每次使用新的 `ArtifactDirectory`。脚本在 `reports/` 内保留 `run.json` 阶段状态与退出码、`test.json`、可用的 `nunit.xml`、`build.json`（含 BuildReport 或失败详情）及各阶段的 Editor 和 stderr 日志。编译或启动阶段出错、超时等情况下，尚未生成的 NUnit XML 或 BuildReport 不会补造。脚本返回首个失败阶段的原退出码。模板即使在该步骤失败时也会上传已生成的报告，保留 14 天。Player 文件位于同次执行目录的 `player/`，可按需增加单独的上传步骤。
