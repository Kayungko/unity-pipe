# Editor lifecycle & observability commands

Commands to control Editor play mode, focus, and menus, and to observe the Editor's state, console, and performance.

### `editor_play`
Enter Unity Editor play mode. Returns a command failure when scripts have compiler errors or are still compiling.

| Parameter | Default | Description |
|---|---|---|
| `run_in_background` | `true` | Keep the game running while the Editor is unfocused for this Play Mode session. The prior value is restored on exit. |

**Returns:** `string`

### `editor_stop`
Exit Unity Editor play mode.

No parameters.

**Returns:** `string`

### `editor_pause`
Toggle pause state of Unity Editor play mode (calling it again while paused resumes play mode).

No parameters.

**Returns:** `string`

### `editor_status`
Get detailed Unity Editor status and state information.

No parameters.

**Returns:** `StatusResponse`

### `wait`
Wait for an Editor condition without blocking the Unity main thread. The command holds the execution gate while it waits, so use it for changes driven by the Editor, the user or existing callbacks.

| Parameter | Default | Description |
|---|---|---|
| `condition` | required | `editor_ready`, `playing`, `stopped`, `not_compiling`, `asset_exists`, or `object_active` |
| `path` | – | Asset path for `asset_exists`. |
| `target` | – | Scene object reference for `object_active`. |
| `timeout_ms` | `10000` | Condition budget from 1 to 600000 milliseconds. Set the CLI `--timeout` higher than this value. |

A match returns `status=matched`. A condition timeout exits 21. Use `--job` to submit a cancellable wait and `upipe job-cancel` to stop it. Detached jobs belong to the current Editor domain.

### `wait_start`, `wait_status`, and `wait_cancel`

`wait_start` returns a `waitId` immediately and releases the command queue. Pass an object as `target` and optionally select a component with `component`. To wait for a future hierarchy path or a missing intermediate object, pass `tolerate_missing=true`. Alternatively, pass `find_type` with a fully qualified Unity Component type. It waits for a live scene component and reports an error if there are multiple candidates at binding time.

`field` accepts a dotted path such as `State.Score` through public instance fields and public getters without public setters. The path must end in a scalar. Methods and indexers are rejected. Use `op` and `timeout_ms` to set the comparison and budget. For `changed`, omit `value`. The first available value becomes `initialValue`, and a later different value matches. Boolean, text and enum members support `eq`, `ne` and `changed`. Use an enum name as the comparison value.

Pass `capture_path` ending in `.png` to save Game View pixels during the same Editor update as the match. The path resolves under the authoring root and can overwrite an existing PNG. `capture_source=camera` renders a camera. `capture_source=screen` requires Play Mode and a verified fresh Game View frame. Optional `capture_width` and `capture_height` control the output dimensions. The result includes `matchedFrame`, `captureFrame`, `value` and the capture metadata.

Use `pause=true` to pause Play Mode on the matching frame. When both capture and pause are requested, the Editor captures first and then pauses in the same update. The result includes `paused` and `pausedFrame`.

Poll with `wait_status` and pass the ID as `wait_id`. The CLI returns the terminal result under `error.data` for `timed_out` (21), `cancelled` (22), `failed` or `interrupted` (20). Use `wait_cancel` to stop a pending wait. A domain reload interrupts pending waits and retains their terminal results in the current Editor session. The limit is 32 pending waits and 128 retained results.

The command catalog advertises `effect` as `read_only`, `stateful`, `destructive`, or `unknown`. Unknown is the default for commands without an explicit declaration.

### `editor_quit`

Close the Editor after returning a structured reply. The default refusal lists dirty loaded scenes and the dirty open Prefab stage. Use `save=true` to save them to their existing paths, or `discard=true` to close without saving. A dirty untitled scene must be given a path before using save. In Play Mode, stop play before saving and quitting. A combined save and discard request is rejected.

On success the result includes `quitting`, `exitCode`, `saved` and `discarded`. The process exits shortly afterward. With no explicit discard, newly dirty contents or a Play Mode transition cancel the pending exit. This command is unavailable inside batches.

### `editor_focus`
Bring the Unity Editor window to the foreground.

No parameters.

**Returns:** `string`
**Notes:** `MainThreadRequired = true`.

### `menu`
Execute an Editor menu item by path, or list available items when no path is given.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | no | `–` | Menu item path to execute, e.g. "Assets/Reimport All". Omit to list available menu items. |

**Returns:** `MenuResponse`
**Notes:** `MainThreadRequired = true`.

### `screenshot`
Capture the Scene or Game view as a PNG and return its file path.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `view` | no | `game` | Which view to capture: 'game' (default) or 'scene' |
| `output` | no | `–` | Output PNG path (absolute, or relative to the project root). Defaults to a timestamped file under <project>/Temp/pipeline-screenshots/. |
| `width` | no | `0` | Output width in pixels. 0 (default) uses the view camera's current width. |
| `height` | no | `0` | Output height in pixels. 0 (default) uses the view camera's current height. |

**Returns:** `ScreenshotResponse`
**Notes:** `MainThreadRequired = true`.

### `set_autotick`
Keep the editor ticking while unfocused by forcing EditorApplication.SignalTick at a throttled rate.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `enable` | no | `true` | Enable (true) or disable (false) auto-tick mode |
| `interval_ms` | no | `16` | Minimum milliseconds between forced ticks. 0 = every update (max rate, pegs a CPU core). Default 16 (~60Hz). |
| `persist` | no | `true` | Persist this choice to SessionState so it survives a domain reload. Set `false` for a one-off/expensive setting (e.g. `interval_ms=0`) that should revert to the last persisted choice (or the default) after the next recompile instead of sticking for the rest of the session. |

**Returns:** `string`
**Notes:** `MainThreadRequired = true`. Persisted in SessionState by default: your enabled/interval choice survives a domain reload (dies with the editor process/session).

Use `persist=false` for a change you only want in effect until the next unrelated recompile — e.g. a short profiling run at `interval_ms=0` (pegs a CPU core) or a one-off test/CI script — so it can't outlive its purpose and silently burn CPU for the rest of the session. Leave `persist` at its default `true` for a setting you want to remain in effect across recompiles, such as the normal always-on 16ms tick.

### `console` and `console_status`
Use `console` for cursor-based reads across Editor reloads and in a Development Player. Pass the returned `cursor` as `since` and `session` as `since-session`. If either refers to an expired cursor, the response includes a fresh tail with `reset=true`.

Use `console_status` to read the cursor and retained severity counts without fetching logs. In the Editor, `groundTruth` includes native Console counts, the compiler state and the age of the last main-thread sample. Entries recovered from the native Console store have `seeded=true`.

### `get_console_logs`
Read recently captured Editor console logs (structured).

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `severity` | no | `all` | Filter: all | log | warning | error. 'all' = every entry; 'log' = Log only; 'warning' = Warning only; 'error' = Error/Exception/Assert only. |
| `limit` | no | `100` | Max entries to return (most-recent first), capped at 1000. |

**Returns:** `object`

### `clear_console`
Clear the captured log buffer and the Unity Editor console.

No parameters.

**Returns:** `object`

### `get_performance_stats`
Read render, memory, and frame-timing stats (structured, read-only).

No parameters.

**Returns:** `PerformanceStats`

### Profiler recordings and memory snapshots

- `profiler_targets` lists the local Editor and discoverable Development Players. Pass a Player's ID as `player_id` to `profiler_record`, or omit it to record the Editor. Set `file` to a `.data` filename and poll `profiler_record_status`. Use `profiler_cancel` to stop an in-progress recording.
- `profiler_analyze` loads a saved `.data` file and returns frame times, GC allocation and marker hot spots for the selected thread. Loading or recording over existing Profiler history requires `confirm=true`. Recording restores the previous target and Profiler settings.
- `memory_snapshot` takes an Editor memory snapshot. Pass a `.snap` filename and poll `memory_snapshot_status`.

The files are saved under `Library/Pipeline/Profiles/`. Copy captures out of `Library` before cleaning the project cache.

### `audit`
Run a Project Auditor static-analysis scan. Returns immediately; poll `audit_status` until status is `completed`, then read the CSV.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `categories` | no | `–` | Comma-separated issue categories to scan (e.g. `Code,ProjectSetting,Texture`). Default: all categories. An unknown name is rejected with an error listing the valid values. |
| `output` | no | `–` | CSV output path (absolute, or relative to the project root). Defaults to `<project>/Temp/pipeline-audit/<scanId>.csv`. |

**Returns:** `object` — `{ status, scanId, csvPath }` on start, or `{ status: "unavailable" | "error" | "busy", message }`.
**Notes:** Requires Project Auditor in the Editor. Only one scan runs at a time (a second call returns `busy`). Not cancellable: stop polling to abandon a scan.

### `audit_status`
Get the status of the last audit: `idle` | `scanning` | `completed` | `failed` | `interrupted` | `unavailable`.

No parameters.

**Returns:** `AuditStatus` (JSON) — `issueCount` and `csvPath` are populated when `completed`; `error` when `failed`; `message` when `unavailable`.
**Notes:** `MainThreadRequired = false`, so it answers while a scan holds the main thread (Code analysis compiles assemblies). `interrupted` means a domain reload killed an in-flight scan — re-run `audit`.

#### Project Auditor prerequisites

`audit` needs both Project Auditor **and** its rules:

- **Project Auditor absent** — `audit` returns `unavailable` immediately.
- **Rules absent** — when Project Auditor ships as a built-in editor module, its rules (descriptors,
  API/obsolete databases, Roslyn analyzers) come from the separate `com.unity.project-auditor-rules`
  package. Without it Project Auditor registers no analysis modules and cannot analyze anything, so
  `audit` returns `scanning` and the first `audit_status` poll reports `unavailable` with a message
  naming the package to install. It never reports an empty `completed` scan, which would read as a
  clean project.

The CSV columns are `Category, Severity, Areas, Description, RelativePath, Line, DescriptorId, Recommendation`;
only diagnostics (things to fix) are emitted, not raw inventory rows.

### `report_evals`
Aggregate the local eval-usage telemetry into a ranked report: API fingerprint frequency, one-liner percentage, error rate, and command-coverage suggestions (read-only). See [Eval usage telemetry](#eval-usage-telemetry) below.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `top` | no | `50` | Maximum entries in each ranked list: the fingerprint ranking and the uncovered patterns suggested as commands. Accepted range: 0 or greater; negative values fall back to the default. |

**Returns:** `EvalUsageReport`
**Notes:** `MainThreadRequired = false` (stays servable while the Editor is settling). Reads `<project>/Library/Pipeline/eval-usage.jsonl` merged with the rotated `eval-usage.old.jsonl` (oldest first), so the report covers the full retained history — bounded to roughly 2× the size cap. Returns a zeroed report if no evals have been recorded. The report's `distinctFingerprints` field carries the uncapped total of distinct fingerprints so truncation of the ranked list is always visible; coverage suggestions are computed from the full ranking, not the capped list.

## Eval usage telemetry

The `eval`/`eval_file` commands are the workhorse of autonomous agent sessions, but they are opaque: raw C# through the protocol, unauditable, and hard to displace with typed commands without knowing what agents actually run. This telemetry closes that loop by recording, **locally**, what each eval does — so the [eval-displacement epic (AUTHAPI-24)](https://jira.unity3d.com/browse/AUTHAPI-24) can be triaged from data instead of anecdotes.

### What is recorded, and where

Every `eval`/`eval_file` invocation appends one JSON object (one line, JSONL) to `<project>/Library/Pipeline/eval-usage.jsonl`:

| Field | Meaning |
|-------|---------|
| `time` | ISO-8601 UTC timestamp. |
| `command` | `eval` or `eval_file`. |
| `success` / `error` | Whether it succeeded; short error category on failure. |
| `payloadLength` / `lineCount` | Size/shape of the eval body. |
| `executionTimeMs` | How long it took. |
| `classification` | `single-expression` (a one-liner: one invoke / read / assignment) or `statements` (multi-statement bodies, e.g. polling loops, bulk setup). |
| `fingerprints` | The top-level member-access paths from the syntax tree — e.g. `AssetDatabase.Refresh`, `PlayerSettings.SetScriptingBackend`, `Object.FindFirstObjectByType<T>`, `MatchManager.Instance.State`. Rendering is a strict whitelist: the only source text that can appear in a fingerprint is an identifier, type name, or member name. Literals, interpolated strings, and any other expression form are replaced by the neutral `<expr>` placeholder (`"…".Length` records as `<expr>.Length`); object creations record as `new TypeName` (constructor arguments never appear); casts unwrap to the inner expression; generic type arguments normalise to `<T>`. A fingerprint that would be nothing but the placeholder is dropped. |
| `source` | The raw eval source — **absent by default**; present only when the opt-in below is enabled, and truncated at 64 K characters with an explicit `…[truncated]` marker (`payloadLength` still reports the full length). |

**Local-first, privacy-first, editor-only.** Nothing is transmitted off the machine, and nothing is recorded outside the Editor: player builds — including development players, where eval itself is available — never write telemetry (recording compiles to a no-op there). The fingerprint is derived from the C# syntax tree, so the log captures *which APIs* agents reach for without capturing *what data* they operate on. Conditional-access chains (`a?.B.C`) are fingerprinted with `?.` normalised to `.` so they rank with their unconditional form. The fingerprint deliberately undercounts a few forms (arguments nested inside a chain's receiver call, bare object creations) — see the `EvalUsageFingerprinter` class doc for the full blind-spot list before treating absence as proof of non-use.

**Zero eval latency.** Recording is fire-and-forget: when an eval completes, the source and outcome plus the current settings are snapshotted, and the parse → fingerprint → append runs on a background thread from that snapshot — the eval response is never delayed by telemetry. The JSONL is bounded by a size cap and rotates to `eval-usage.old.jsonl`; the rotated file is merged back into `report_evals`, so retained history is bounded to roughly 2× the cap.

### Settings

Configured on the `EditorPipelineManager` settings asset (`Window/Pipeline/Settings...`). Edits apply live from the inspector, and the stored values are re-applied on every server start:

- **Eval Telemetry Enabled** (default on) — master switch. When off, nothing is recorded (and, as always, nothing is transmitted).
- **Store Eval Source** (default off) — the explicit opt-in to also persist the raw eval `source` in each record, for local debugging only.

### The feedback loop: report → backlog triage

Run `report_evals` to aggregate the log into an `EvalUsageReport`:

- **Ranked fingerprints** with counts — what agents actually eval, most-used first.
- **One-liner percentage** and **error rate** — the shape of eval usage (the evidence base for AUTHAPI-24 is that ~80% of eval calls are one-liners).
- **Coverage suggestions** — each fingerprint is cross-referenced against the live command catalog:
  - `covered`: patterns already served by an existing command (e.g. `PlayerSettings.SetScriptingBackend` → `set_player_settings`) — these are habit, and should move off eval.
  - `topUncovered`: the highest-frequency patterns with no covering command (e.g. `AssetDatabase.Refresh` → proposed `refresh_assets`) — these are the next typed commands to build ([AUTHAPI-17](https://jira.unity3d.com/browse/AUTHAPI-17)).

This turns backlog triage into a lookup: "12× `AssetDatabase.Refresh`, no command → build it; 60× state-read loops → `wait_for`". Re-run across sessions to demonstrate eval's shrinking share as typed commands land.

### `get_authoring_root`
Get the base folder (under Assets/) that bare authoring paths resolve against.

No parameters.

**Returns:** `object`

### `set_authoring_root`
Set the base folder (under Assets/) that bare authoring paths resolve against and are confined to. Use 'Assets' for full project access.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `root` | yes | `–` | Project-relative folder under Assets/, e.g. Assets/AgentWork. Use 'Assets' to allow the whole project. |

**Returns:** `object`

See [Creating commands](../creating-commands.md) and [Connectivity](../connectivity.md).
