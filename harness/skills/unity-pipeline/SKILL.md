---
name: unity-pipeline
description: Drive a running Unity Editor or development Player from the command line via the unity-pipeline package — install the package, keep the editor ticking while unfocused, run the edit→recompile→run_tests loop, evaluate C#, and hot-reload files at runtime. Use when an agent needs to control a live Unity instance, automate Unity tests, recompile scripts headlessly, or apply runtime hot reloads. Uses the self-maintained `upipe` CLI (github.com/Kayungko/unity-pipe, direct HTTP to the pipeline server); does NOT require Unity's closed-source unity CLI binary.
---

# Unity Pipeline (agent control)

Invoke commands with `upipe <subcommand>` (thin client over the pipeline
HTTP API inside the editor; zero third-party deps, single static binary).
Run `upipe commands` to list what an instance exposes. Two servers exist:
**Editor** (`7800-7849`, auto-starts with the editor) and **Runtime**
(`7900-7949`, only in a dev Player build). See the package `README.md`
"Commands" reference for the full list and parameters.

## 0. Exit codes (branch on these, never parse stderr)

| code | meaning |
|---|---|
| 0 | success |
| 2 | usage error |
| 10 | no running instance (port file missing) |
| 11 | unreachable after domain-reload retry ladder |
| 12 | auth failure — editor restart refreshes the token |
| 13 | busy/settling/queue-full — retryable |
| 20 | command failed (compile errors, failing tests) — details in `error.data` |
| 21 | timeout |
| 30 | protocol mismatch (package too old/new) |

With `--json`, stdout is always exactly one envelope:
`{"ok": bool, "error": {code,kind,message,retryable,data}?, "data": ...}`.

## 0.5 Response-value field map & write-gates (field-tested)

Exit code 0 does NOT mean the business operation happened — write commands
gate behind `confirm`/`dry_run` and report refusal in **three different
places**. Success = outer `ok=true` AND (`result.applied==true` or
`result.status != "error"`):

| gate style | commands | refusal looks like |
|---|---|---|
| inner `result.status=="error"` | `build`, `bake_*` | outer ok=true, exit 0 — check result.status! |
| outer `ok=false`, `error.code=20` | `clear_*`, `delete_asset`, `switch_build_target` | error.message contains "confirm" |
| inner `result.applied==false` | `set_*_settings` | message contains "Refused" |

Read eval return values from `data.result.result` (the `output` field is
always null); scene-hierarchy nodes live under `result.roots`. Asset search:
prefer `eval` + `AssetDatabase.FindAssets` over `exec search` (full-catalog
queries can block >2min). Full field-tested recipes — eval pitfalls (no
`System.Tuple`, no `\n` escapes), missing-script locator, prefab census
strategy, probe-object lifecycle — see `docs/RECIPES.md` in the repo.

## 1. Install & verify

```powershell
upipe pkg-install --source <com.unity.pipeline-0.5.0-2022.1.tgz> --dry-run   # review, then drop --dry-run
upipe status                # port file + server health + editor_status (auto-detects project from cwd)
upipe commands              # dynamic command surface of the connected instance
```

`upipe pkg-install` refuses read-only (P4-managed) targets — run p4
edit/reconcile first; upipe never runs p4 itself.

## 2. Autonomous edit loop (Editor)

This is the core agent workflow: keep the editor alive, change code, recompile, test.

```powershell
# 1. Keep the editor ticking even when unfocused/minimized. REQUIRED before headless work —
#    Unity otherwise throttles or stalls update/compile when it isn't the active app.
upipe exec set_autotick --set enable=true

# 2. Edit C# source files on disk normally.

# 3. Recompile and WAIT: upipe recompile polls recompile_status through the
#    domain reload until completed/up_to_date. Compile errors → exit 20 with
#    the error list in error.data.
upipe recompile

# 4. (Optional) List available tests without running them.
upipe exec list_tests --set mode=editor        # mode: all | editor | playmode

# 5. Run tests (filter to keep it fast). Failing tests → exit 20, summary in error.data.
upipe test --mode editor --filter MyFixture.MyTest
```

`upipe test` submits `run_tests --async_tests=true` and polls `test_status`.
A filter matching zero tests is reported as `{"status":"completed","message":"filter matched no tests"}`, not a failure.

> **Known caveat:** when any selected test *fails*, the server may surface an
> opaque summary. Re-run a narrower `--filter`, or inspect the editor's Test
> Runner / logs for per-test detail.

## 3. Runtime hot reload

Change gameplay code in a **running** game with no domain reload. The game
must be live: enter Editor Play Mode (`upipe exec editor_play --set enable=true`)
or run a dev Player. A `RuntimePipelineManager` in the scene auto-discovers
tagged methods on `Awake` (no manual registration). Mono only — Editor Play
Mode and Mono desktop dev builds, not IL2CPP.

### Prefer: in-place — `reload_file`

1. Tag the method `[HotReload]` on the MonoBehaviour.
2. Edit its body on disk.
3. Apply (re-run to iterate): `upipe exec reload_file --set filename=Assets/Spinner.cs`
   (add `--set pdb=true` for debuggable reloads with a source-mapped PDB).

Constraints: `void` instance methods only; **public** members only; debugging
requires `pdb=true` (the default emits no symbols).

### Alternative: with helper — `reload_file_override`

1. Tag the method `[HotReloadWithOverrides]` and route it via
   `HotReloadHelper.ExecuteWithHotReload(this, "Update", OriginalUpdate)`.
2. In a **separate file that does not redeclare the target type**, add a
   `public static` method tagged `[HotReloadOverrideMethod("BossController.Update")]`
   taking the instance first.
3. Apply: `upipe exec reload_file_override --set filename=Assets/HotReload/BossOverrides.cs`

`upipe exec hotreload_status` shows active overrides.

## 4. Quick C# eval (Runtime)

```powershell
upipe eval "return 2 + 2;"
upipe eval-file Assets/Scratch.cs
```

## 5. Project audit (Editor)

```powershell
upipe audit                 # triggers audit, polls audit_status until terminal
```

Terminal statuses: `completed` (csvPath + issueCount), `failed`,
`unavailable` (no Project Auditor / rules — see message), `interrupted`
(a domain reload killed the scan — just re-run). Requires Project Auditor
plus its rules (`upipe exec package_add --set identifier=com.unity.project-auditor-rules --set confirm=true`).

## 6. Fallback when upipe is not installed

The CLI is a thin HTTP client; the protocol is directly reachable:

```powershell
# Read <project>/Library/Pipeline/.unity-pipeline-port → { "port": …, "evalToken": … }
$pf = Get-Content "<project>/Library/Pipeline/.unity-pipeline-port" | ConvertFrom-Json
$h = @{ Authorization = "Bearer $($pf.evalToken)" }
Invoke-RestMethod "http://127.0.0.1:$($pf.port)/api/editor_status" -Headers $h          # GET
Invoke-RestMethod "http://127.0.0.1:$($pf.port)/api/exec" -Method Post -Headers $h `
  -ContentType "application/json" `
  -Body (@{ command = "eval"; parameters = @{ code = "return 2 + 2;" } } | ConvertTo-Json)
```

The port file is rewritten after every domain reload — always re-read it.

## Gotchas

- **set_autotick first.** Without it, recompile and tests can hang while the editor
  is unfocused.
- **Hot reload needs the game running.** Enter Play Mode or run a dev Player first.
- **Player-only commands need a dev Player.** `log`, `set_timescale`,
  `runtime_status`, etc. hit the Runtime server, absent in the Editor.
- **Domain reloads drop connections.** upipe retries internally (re-reading the
  port file); during a reload expect exit 13/11 transiently — retry, don't assume failure.
- **Target a specific project** with `--project-path <root>` when cwd is elsewhere.
- **Any command not wrapped as a subcommand** goes through `upipe exec <name> --set k=v`
  (values are JSON-scalar coerced: "true"→bool, "60"→number).
- **Git Bash mangles leading-slash args.** `--set target=/Name` becomes
  `C:/Program Files/Git/Name` (MSYS path conversion) and the command fails with a
  bogus hierarchy-path error. Prefix the call with `MSYS_NO_PATHCONV=1` (or use
  `//Name`); instanceId targets are immune. PowerShell/cmd unaffected.
