# Scene commands

Commands for creating, opening, saving and inspecting scenes, controlling the active scene, snapshotting a scene's hierarchy, and managing the Build Settings scene list. Mutating scene operations are blocked while the editor is entering or in play mode (they fail with a recoverable error before touching any state); read-only commands are safe in play mode.

### `create_scene`
Create a new scene and save it to the given path under the authoring root.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | yes | – | Scene path relative to the authoring root (default Assets/); the Assets/ prefix and the .unity extension are optional. e.g. Scenes/Level1 |
| `additive` | no | `false` | Open the new scene additively alongside currently open scenes instead of replacing them. |
| `template` | no | `empty` | Initial contents: 'empty' (default) for a blank scene, or 'default' to seed a Main Camera + Directional Light matching Unity's built-in 3D template. |
| `confirm` | no | `false` | Must be true to (a) replace open scenes that have unsaved changes (non-additive mode) or (b) overwrite a scene asset that already exists at the target path; otherwise the command refuses (destructive guard — discarded edits and asset overwrites are not recoverable). |

**Returns:** `AuthoringResult`
**Notes:** Blocked during Play mode. Non-additive open refuses when any open scene is dirty (save first via `save_scene`/`save_all` or pass `confirm=true`); overwriting an existing scene asset at the path also requires `confirm=true`.

### `open_scene`
Open an existing scene from the given path.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | yes | – | Scene path relative to the authoring root (default Assets/); the Assets/ prefix and the .unity extension are optional. |
| `additive` | no | `false` | Open additively alongside currently open scenes instead of replacing them. |
| `confirm` | no | `false` | Must be true to discard unsaved changes in currently open scenes (non-additive mode replaces them all); otherwise the command refuses (destructive guard — discarded edits are not recoverable). |

**Returns:** `AuthoringResult`
**Notes:** Blocked during Play mode. Non-additive open refuses when any open scene is dirty (save first via `save_scene`/`save_all` or pass `confirm=true`); the refusal message lists the dirty scene names. Additive opens never replace scenes and are never blocked by the guard.

### `save_scene`
Save an open scene. Saves the active scene when no path is given.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | no | `–` | Path of the open scene to save (authoring-root relative; Assets/ prefix and .unity optional). Omit to save the active scene. |

**Returns:** `AuthoringResult`
**Notes:** Blocked during Play mode.

### `save_all`
Save all open scenes that have unsaved changes.

No parameters.

**Returns:** `object`
**Notes:** Blocked during Play mode.

### `list_open_scenes`
List all currently open scenes with their load/active/dirty state.

No parameters.

**Returns:** `object`

### `set_active_scene`
Set which open scene is the active scene (new objects are created in the active scene).

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | yes | – | Path of an already-open scene to make active (authoring-root relative; Assets/ prefix and .unity optional). |

**Returns:** `AuthoringResult`
**Notes:** Blocked during Play mode.

### `get_scene_hierarchy`
Return the GameObject tree of an open scene (or the active scene). Each node carries instanceId + hierarchyPath usable by GameObject commands.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | no | `–` | Path of the open scene to snapshot (authoring-root relative; Assets/ prefix and .unity optional). Omit for the active scene. |

**Returns:** `SceneHierarchy`

### `add_scene_to_build`
Add a scene to the Build Settings scene list (idempotent). Optionally enable it.

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | yes | – | Scene path to add (authoring-root relative; Assets/ prefix and .unity optional). |
| `enabled` | no | `true` | Whether the scene is enabled in the build list. |

**Returns:** `object`
**Notes:** Blocked during Play mode.

### `remove_scene_from_build`
Remove a scene from the Build Settings scene list (idempotent).

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `path` | yes | – | Scene path to remove (authoring-root relative; Assets/ prefix and .unity optional). |
| `confirm` | no | `false` | Must be true to actually remove; without it the command refuses (destructive guard — a stripped-from-build scene silently breaks scene-index-based loads). |

**Returns:** `object`
**Notes:** Blocked during Play mode. Destructive — `confirm=true` required; build-settings changes are saved immediately.

See [Creating commands](../creating-commands.md) and [Connectivity](../connectivity.md).
