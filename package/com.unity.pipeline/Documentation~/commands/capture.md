# Capture commands

Commands that render a camera, the Scene View, or a UI Toolkit element to a PNG and return it base64-encoded so an agent can "see" the editor (or a running player) without a display.

### `capture_game_view`
Render the game view to a PNG. Returns it inline as base64, unless `save_path` is set — then the result is **path-only** (no base64) so agent tool results stay small; pass `include_inline_image=true` to get both.

`source` selects what is captured. `camera` (default) renders a camera and **does not include Screen Space - Overlay UI** (HUDs, menus, damage numbers on overlay canvases) — overlay canvases composite straight to the screen and never render through a camera. `screen` captures the composited game view backbuffer, so overlay UI is included; because overlay UI only exists at runtime, `source=screen` **requires Play Mode** (in Edit Mode it returns a clear error — use `source=camera`). The screen capture reads the game view at its true pixel resolution — independent of OS display scaling — and preserves the on-screen color tone, so the PNG matches what the Game window shows.

Parameter interactions: `include_inline_image` is only meaningful together with `save_path` (without one, the inline image is always returned). `max_resolution` only applies when an inline image is actually returned — i.e. no `save_path`, or `save_path` + `include_inline_image=true`; it has **no effect** when `save_path` is set without `include_inline_image=true`. `camera` selects a camera for direct camera output or an explicitly enabled fallback.

Screen captures report `frameStatus`. The value is `fresh` when the captured Game view rendered its display during the repaint, `cached` when it skipped rendering, or `unknown` when a new render could not be confirmed. Cached and unknown results carry a `warning`. If no Game view exists, the command opens one in Play Mode, allows up to 1.5 seconds for its first capture, and includes `gameViewOpened: true` on success. With `allow_camera_fallback=true`, a missing screen buffer can return camera output with `frameStatus: "unavailable"` and a warning.

For visual verification, set `require_fresh_frame=true`. A cached, unverified or unavailable frame then fails before returning an image or touching `save_path`, including when camera fallback was requested. A fresh render can still represent the same simulation step while Play Mode is paused. Wait's same-update capture uses an existing view.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `width` | no | `1280` | Output width in px (default 1280; capped 4096). |
| `height` | no | `720` | Output height in px (default 720; capped 4096). |
| `camera` | no | `–` | Optional camera name for direct or fallback camera output. Defaults to Camera.main, then the first enabled camera. |
| `save_path` | no | `–` | Optional project-relative path to write the PNG (e.g. Screenshots/foo.png). When set, the result omits the inline base64 image unless `include_inline_image=true`. |
| `include_inline_image` | no | `false` | Also return the image inline as base64 when `save_path` is set. |
| `max_resolution` | no | `0` | Cap on the inline image's longest edge (e.g. 512), preserving aspect ratio. No effect when `save_path` is set without `include_inline_image=true`. The `save_path` file keeps the requested resolution; a downscaled inline copy is reported via `inlineWidth`/`inlineHeight`. |
| `source` | no | `camera` | `camera` renders a camera (misses Screen Space - Overlay UI); `screen` captures the composited game view incl. overlay canvases (**Play Mode only**). |
| `require_fresh_frame` | no | `false` | Require a verified new render when `source=screen`. |
| `allow_camera_fallback` | no | `false` | Use the named or default camera if the screen buffer is unavailable and strict freshness is disabled. |

**Returns:** `CaptureResult`

### `capture_scene_view`
The default `source=camera` renders the scene camera. Use `source=screen` for the drawn Scene View with gizmos and toolbars. Screen captures use the window's dimensions.
Render the active Scene View to a PNG. Same result contract and parameter interactions as `capture_game_view`: inline base64, or path-only when `save_path` is set (opt back in with `include_inline_image=true`; `max_resolution` has no effect when `save_path` is set without it).

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `width` | no | `1280` | Output width in px (default 1280; capped 4096). |
| `height` | no | `720` | Output height in px (default 720; capped 4096). |
| `save_path` | no | `–` | Optional project-relative path to write the PNG (e.g. Screenshots/foo.png). When set, the result omits the inline base64 image unless `include_inline_image=true`. |
| `include_inline_image` | no | `false` | Also return the image inline as base64 when `save_path` is set. |
| `max_resolution` | no | `0` | Cap on the inline image's longest edge (e.g. 512), preserving aspect ratio. No effect when `save_path` is set without `include_inline_image=true`. The `save_path` file keeps the requested resolution; a downscaled inline copy is reported via `inlineWidth`/`inlineHeight`. |

**Returns:** `CaptureResult`

### `capture_editor_window`

Read the pixels of an open window. Pass its title or type as `window`. The command restores the previously visible tab in the same dock area.

### `capture_editor_element`

Crop the visible portion of a UI Toolkit element from a drawn Editor window. The window and element commands require a graphical Editor session.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `window` | yes | `–` | EditorWindow type name (e.g. InspectorWindow) or window title to capture from. |
| `selector` | yes | `–` | `#name` or `.class`. |
| `save_path` | no | `–` | Optional PNG path under the authoring root. |

**Returns:** `CaptureResult`

### `capture_asset_preview`

Pass an imported asset reference as `asset` to capture its texture or Unity-generated preview. For a cold thumbnail, `wait_ms` controls how long the command lets the Editor generate it. The range is 0 to 10000 ms and the default is 1500 ms. Use 0 to check once. A thumbnail obtained after waiting includes `previewWaitMs`. If no preview becomes available within the budget, the command fails before writing the PNG. The standard `save_path`, `include_inline_image` and `max_resolution` arguments apply.

### `capture_runtime_element`
Capture a UI Toolkit VisualElement (by selector) from a live runtime panel (UIDocument or PanelRenderer) to a PNG; returns path + base64.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `panel` | no | `–` | Name of the target panel: matches the PanelSettings asset name or the host GameObject name (UIDocument or PanelRenderer). Optional when exactly one panel exists. |
| `selector` | yes | `–` | Element selector: '#name', '.class', a type name (e.g. Button), descendant (space) / child ('>') chains, optional pseudo-states (:checked,:hover,:focus,:active,:enabled,:disabled,:not(...)). |
| `output` | no | `–` | Output PNG path (absolute, or relative to Application.persistentDataPath). Defaults to a timestamped file under Application.persistentDataPath. |

**Returns:** `CaptureElementResponse`
**Notes:** `MainThreadRequired = true`, `RuntimeOnly`. Unity 6000.7+ only.

See [Creating commands](../creating-commands.md) and [Connectivity](../connectivity.md).
