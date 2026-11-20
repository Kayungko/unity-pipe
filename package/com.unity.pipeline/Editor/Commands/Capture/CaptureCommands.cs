#if UNITY_EDITOR
using System;
using System.IO;
using System.Linq;
using System.Reflection;
using Newtonsoft.Json;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Editor.Authoring;
using UnityEditor;
using UnityEngine;
using UnityEngine.Rendering;
using Object = UnityEngine.Object;

namespace Unity.Pipeline.Editor.Commands.Capture
{
    /// <summary>
    /// Visual-feedback commands (CLI-199): render a camera, the Scene View, or the composited game view
    /// screen to a PNG and return it base64-encoded, so an agent can "see" the editor without a display.
    /// The screen source (AUTHAPI-10) also captures Screen Space - Overlay UI (HUDs) that never renders
    /// through a camera. Optionally writes the PNG under the project (sandboxed via
    /// <see cref="ProjectPaths"/>) and refreshes the AssetDatabase.
    ///
    /// Captures require a GPU; in batchmode/headless the device type is
    /// <see cref="GraphicsDeviceType.Null"/> and the commands throw so callers get a clear message
    /// instead of an empty image.
    /// </summary>
    static class CaptureCommands
    {
        private const int MaxDimension = 4096;

        [CliCommand("capture_game_view", "Render the game view to a PNG. source=camera (default) renders a camera and misses Screen Space - Overlay UI; source=screen captures the composited backbuffer incl. overlay canvases (Play Mode only). Returns it inline as base64, unless save_path is set (path-only result; pass include_inline_image=true to get both).", Tags = new[] { "capture" })]
        public static CaptureResult CaptureGameView(
            [CliArg("width", "Output width in px (default 1280; capped 4096).")] int width = 1280,
            [CliArg("height", "Output height in px (default 720; capped 4096).")] int height = 720,
            [CliArg("camera", "Optional camera name (source=camera only); defaults to Camera.main, else the first enabled camera.")] string camera = null,
            [CliArg("save_path", "Optional project-relative path to write the PNG (e.g. Screenshots/foo.png). When set, the result omits the inline base64 image unless include_inline_image=true.")] string savePath = null,
            [CliArg("include_inline_image", "Also return the image inline as base64 when save_path is set (default false: path-only result). Only meaningful together with save_path.")] bool includeInlineImage = false,
            [CliArg("max_resolution", "Cap on the inline image's longest edge (e.g. 512). Only applies when an inline image is returned (no save_path, or save_path + include_inline_image=true); the save_path file keeps the requested resolution.")] int maxResolution = 0,
            [CliArg("source", "What to capture: 'camera' (default) renders a camera (misses Screen Space - Overlay UI); 'screen' captures the composited game view incl. overlay canvases (HUDs), Play Mode only.")] string source = "camera",
            [CliArg("require_fresh_frame", "For source=screen, fail before saving or returning an image if a fresh Game view render cannot be verified.")] bool requireFreshFrame = false)
        {
            var src = (source ?? "camera").Trim().ToLowerInvariant();
            switch (src)
            {
                case "camera":
                {
                    GuardHasGpu();

                    var cam = ResolveCamera(camera);
                    if (cam == null)
                        throw new ArgumentException("No camera found to capture.");

                    return Capture((w, h) => EncodeCameraToPng(cam, w, h), $"camera:{cam.name}",
                        width, height, savePath, includeInlineImage, maxResolution);
                }
                case "screen":
                {
                    // Overlay UI only composites to the backbuffer at runtime, so screen capture is a
                    // Play-Mode operation; guard before touching the GPU so the message is deterministic.
                    if (!Application.isPlaying)
                        throw new InvalidOperationException(
                            "capture_game_view with source=\"screen\" requires Play Mode: Screen Space - Overlay UI only composites to the backbuffer at runtime. Use source=\"camera\" in Edit Mode.");

                    GuardHasGpu();

                    var (target, frameStatus) = PrepareScreenCapture();
                    var warning = frameStatus switch
                    {
                        "fresh" => null,
                        "cached" => "The Game view did not redraw for this capture. The image is its last rendered frame.",
                        _ => "A fresh Game view render could not be verified. The image may be from an earlier frame."
                    };
                    if (requireFreshFrame && warning != null)
                        throw new InvalidOperationException(warning + " Make the Game view visible and let it render before retrying.");

                    var result = Capture((w, h) => EncodeScreenToPng(target, w, h), "screen",
                        width, height, savePath, includeInlineImage, maxResolution);
                    result.FrameStatus = frameStatus;
                    result.Warning = warning;
                    return result;
                }
                default:
                    throw new ArgumentException($"Unknown source '{source}'. Use 'camera' or 'screen'.");
            }
        }

        [CliCommand("capture_scene_view", "Render the active Scene View to a PNG. Returns it inline as base64, unless save_path is set (path-only result; pass include_inline_image=true to get both).", Tags = new[] { "capture" })]
        public static CaptureResult CaptureSceneView(
            [CliArg("width", "Output width in px (default 1280; capped 4096).")] int width = 1280,
            [CliArg("height", "Output height in px (default 720; capped 4096).")] int height = 720,
            [CliArg("save_path", "Optional project-relative path to write the PNG (e.g. Screenshots/foo.png). When set, the result omits the inline base64 image unless include_inline_image=true.")] string savePath = null,
            [CliArg("include_inline_image", "Also return the image inline as base64 when save_path is set (default false: path-only result). Only meaningful together with save_path.")] bool includeInlineImage = false,
            [CliArg("max_resolution", "Cap on the inline image's longest edge (e.g. 512). Only applies when an inline image is returned (no save_path, or save_path + include_inline_image=true); the save_path file keeps the requested resolution.")] int maxResolution = 0)
        {
            GuardHasGpu();

            var sv = SceneView.lastActiveSceneView;
            if (sv == null || sv.camera == null)
                throw new ArgumentException("No active Scene View to capture.");

            var cam = sv.camera;
            return Capture((w, h) => EncodeCameraToPng(cam, w, h), "sceneView",
                width, height, savePath, includeInlineImage, maxResolution);
        }

        /// <summary>
        /// Shared capture core. Renders at the requested (clamped) size via <paramref name="renderPng"/>
        /// and writes the file when <paramref name="savePath"/> is set. The image is inlined as base64
        /// only when there is no file, or on <paramref name="includeInlineImage"/> — a save_path result
        /// is otherwise path-only, so agent tool results stay small (AUTHAPI-8).
        /// <paramref name="maxResolution"/> caps the inline image's longest edge (no-op when no inline
        /// image is returned); the saved file keeps the requested resolution. <paramref name="renderPng"/>
        /// is the source-specific encoder — camera render (source=camera / scene view) or screen
        /// backbuffer (source=screen, incl. overlay canvases, AUTHAPI-10).
        /// </summary>
        private static CaptureResult Capture(Func<int, int, byte[]> renderPng, string source,
            int width, int height, string savePath, bool includeInlineImage, int maxResolution)
        {
            var w = Mathf.Clamp(width, 1, MaxDimension);
            var h = Mathf.Clamp(height, 1, MaxDimension);

            var wantsFile = !string.IsNullOrEmpty(savePath);
            var wantsInline = !wantsFile || includeInlineImage;

            // Without a file the inline image is the only artifact: the cap applies to the render itself.
            if (!wantsFile && maxResolution > 0)
                (w, h) = ClampToLongEdge(w, h, maxResolution);

            var png = renderPng(w, h);
            string base64 = null;
            int? inlineW = null, inlineH = null;
            if (wantsInline)
            {
                var inlinePng = png;
                if (wantsFile && maxResolution > 0 && maxResolution < Mathf.Max(w, h))
                {
                    // Re-render small for the inline copy; the file already has the full resolution.
                    var (iw, ih) = ClampToLongEdge(w, h, maxResolution);
                    inlinePng = renderPng(iw, ih);
                    inlineW = iw;
                    inlineH = ih;
                }
                base64 = Convert.ToBase64String(inlinePng);
            }

            return new CaptureResult
            {
                Width = w,
                Height = h,
                Encoding = "png",
                Base64 = base64,
                Bytes = png.Length,
                Source = source,
                SavedPath = WriteIfRequested(png, savePath),
                InlineWidth = inlineW,
                InlineHeight = inlineH
            };
        }

        /// <summary>Scale (w, h) down so the longest edge is at most <paramref name="maxEdge"/>, preserving aspect.</summary>
        private static (int w, int h) ClampToLongEdge(int w, int h, int maxEdge)
        {
            var longest = Mathf.Max(w, h);
            if (maxEdge <= 0 || longest <= maxEdge)
                return (w, h);

            var scale = (float)maxEdge / longest;
            return (Mathf.Max(1, Mathf.RoundToInt(w * scale)), Mathf.Max(1, Mathf.RoundToInt(h * scale)));
        }

        /// <summary>Throw when no GPU is available (batchmode/headless), where a render would be blank.</summary>
        private static void GuardHasGpu()
        {
            if (SystemInfo.graphicsDeviceType == GraphicsDeviceType.Null)
                throw new InvalidOperationException("No GPU available (batchmode/headless); cannot capture.");
        }

        /// <summary>
        /// Resolve the camera to capture: by name (if provided), else <see cref="Camera.main"/>,
        /// else the first enabled camera. Returns null when nothing matches.
        /// </summary>
        private static Camera ResolveCamera(string name)
        {
            if (!string.IsNullOrEmpty(name))
            {
                // Camera.allCameras only includes enabled cameras; also scan all loaded cameras so a
                // disabled-but-named camera can still be targeted explicitly.
                var byName = Camera.allCameras.FirstOrDefault(c => c.name == name)
                    ?? PipelineUtils.FindObjectsByType<Camera>().FirstOrDefault(c => c.name == name);
                return byName;
            }

            if (Camera.main != null)
                return Camera.main;

            return Camera.allCameras.FirstOrDefault();
        }

        /// <summary>
        /// Render <paramref name="cam"/> off-screen into a temporary RenderTexture and encode the
        /// result to PNG bytes. The camera's target texture and the active RenderTexture are restored
        /// in a finally block so capturing never leaves global render state dirty.
        /// </summary>
        private static byte[] EncodeCameraToPng(Camera cam, int w, int h)
        {
            var rt = RenderTexture.GetTemporary(w, h, 24);
            var prevTarget = cam.targetTexture;
            var prevActive = RenderTexture.active;
            try
            {
                cam.targetTexture = rt;
                cam.Render();
                RenderTexture.active = rt;

                var tex = new Texture2D(w, h, TextureFormat.RGB24, false);
                try
                {
                    tex.ReadPixels(new Rect(0, 0, w, h), 0, 0);
                    tex.Apply();
                    return ImageConversion.EncodeToPNG(tex);
                }
                finally
                {
                    Object.DestroyImmediate(tex);
                }
            }
            finally
            {
                cam.targetTexture = prevTarget;
                RenderTexture.active = prevActive;
                RenderTexture.ReleaseTemporary(rt);
            }
        }

        /// <summary>
        /// Capture the composited game view — including Screen Space - Overlay canvases and other
        /// screen-space UI that never render through a camera (AUTHAPI-10) — and encode it to PNG,
        /// scaled to the requested size.
        ///
        /// Read the Game view's own target at its true pixel resolution to avoid
        /// focus-dependent ScreenCapture output and OS display scaling.
        /// </summary>
        private static byte[] EncodeScreenToPng(RenderTexture source, int w, int h)
        {
            var outRt = RenderTexture.GetTemporary(w, h, 0, RenderTextureFormat.ARGB32);
            var prevActive = RenderTexture.active;
            try
            {
                // Scale to the requested output size. Default read/write semantics keep the blit
                // conversion-neutral, so the display-ready values reach the PNG unchanged (no double
                // gamma). The source holds GPU-UV-oriented content: on APIs where UV(0,0) is the top
                // (D3D, Metal, Vulkan) it arrives vertically flipped, so un-flip in the blit.
                if (SystemInfo.graphicsUVStartsAtTop)
                    Graphics.Blit(source, outRt, new Vector2(1f, -1f), new Vector2(0f, 1f));
                else
                    Graphics.Blit(source, outRt);
                RenderTexture.active = outRt;

                var tex = new Texture2D(w, h, TextureFormat.RGB24, false);
                try
                {
                    tex.ReadPixels(new Rect(0, 0, w, h), 0, 0);
                    tex.Apply();
                    return ImageConversion.EncodeToPNG(tex);
                }
                finally
                {
                    Object.DestroyImmediate(tex);
                }
            }
            finally
            {
                RenderTexture.active = prevActive;
                RenderTexture.ReleaseTemporary(outRt);
            }
        }

        /// <summary>
        /// Get the main Game view's target after attempting a synchronous repaint.
        /// Never activate a background tab on the caller's behalf.
        /// </summary>
        private static (RenderTexture target, string frameStatus) PrepareScreenCapture()
        {
            const BindingFlags staticFlags = BindingFlags.Static | BindingFlags.NonPublic | BindingFlags.Public;
            const BindingFlags instanceFlags = BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public;
            const string missingFrame = "The Game view has no readable frame. Open the Game view and let it render before retrying source=screen.";
            var playModeView = Type.GetType("UnityEditor.PlayModeView,UnityEditor");
            EditorWindow view;
            FieldInfo targetField;
            try
            {
                view = playModeView?.GetMethod("GetMainPlayModeView", staticFlags)?.Invoke(null, null) as EditorWindow;
                targetField = playModeView?.GetField("m_TargetTexture", instanceFlags);
            }
            catch (TargetInvocationException ex)
            {
                throw new InvalidOperationException(missingFrame, ex);
            }
            if (view == null || targetField == null)
                throw new InvalidOperationException(missingFrame);

            var frameStatus = RepaintGameView(view, playModeView);
            var target = targetField.GetValue(view) as RenderTexture;
            if (target == null || !target.IsCreated())
                throw new InvalidOperationException(missingFrame);
            return (target, frameStatus);
        }

        /// <summary>
        /// A repaint can be skipped by the Editor. Treat a synchronous camera render
        /// callback during the repaint as evidence that the Game view drew a frame.
        /// </summary>
        private static string RepaintGameView(EditorWindow view, Type playModeView)
        {
            const BindingFlags staticFlags = BindingFlags.Static | BindingFlags.NonPublic | BindingFlags.Public;
            const BindingFlags instanceFlags = BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public;
            try
            {
                var parent = typeof(EditorWindow).GetField("m_Parent", instanceFlags)?.GetValue(view);
                var activeView = parent?.GetType().GetProperty("actualView", instanceFlags);
                var repaint = Type.GetType("UnityEditor.GUIView,UnityEditor")
                    ?.GetMethod("RepaintImmediately", instanceFlags, null, Type.EmptyTypes, null);
                if (activeView == null || repaint == null)
                    return "unknown";
                if (!ReferenceEquals(activeView.GetValue(parent), view))
                    return "cached";

                var renderNeeded = playModeView.GetProperty("renderViewCallNeededInOnGUI", staticFlags)?.GetValue(null);
                if (renderNeeded is bool needed && !needed)
                    return "cached";

                var rendered = false;
                void OnCameraRendered(Camera camera) => rendered |= camera != null && camera.cameraType == CameraType.Game;
                void OnFrameRendered(ScriptableRenderContext context, Camera[] cameras)
                    => rendered |= cameras.Any(camera => camera != null && camera.cameraType == CameraType.Game);

                Camera.onPostRender += OnCameraRendered;
                RenderPipelineManager.endFrameRendering += OnFrameRendered;
                try
                {
                    repaint.Invoke(parent, null);
                }
                finally
                {
                    Camera.onPostRender -= OnCameraRendered;
                    RenderPipelineManager.endFrameRendering -= OnFrameRendered;
                }
                return rendered ? "fresh" : "unknown";
            }
            catch (TargetInvocationException)
            {
                return "unknown";
            }
        }

        /// <summary>
        /// When <paramref name="savePath"/> is set, validate it through the project-path sandbox,
        /// write the PNG to its absolute filesystem location, refresh the AssetDatabase, and return
        /// the resolved project-relative asset path. Returns null when no path was requested.
        /// </summary>
        private static string WriteIfRequested(byte[] png, string savePath)
        {
            if (string.IsNullOrEmpty(savePath))
                return null;

            var resolved = ProjectPaths.Resolve(savePath, out var err);
            if (resolved == null)
                throw new ArgumentException(err);

            // ProjectPaths.Resolve returns a project-relative path; combine it with the project root
            // (the folder that contains Assets/) to get the absolute file to write.
            var absolute = Path.Combine(ProjectPaths.ProjectRoot, resolved);
            var directory = Path.GetDirectoryName(absolute);
            if (!string.IsNullOrEmpty(directory))
                Directory.CreateDirectory(directory);

            File.WriteAllBytes(absolute, png);
            AssetDatabase.Refresh();
            return resolved;
        }
    }

    /// <summary>
    /// Result of a capture command: the rendered dimensions, the source, the project-relative path
    /// the PNG was written to (null when not saved), and the base64 payload — omitted from the JSON
    /// when a save_path result is path-only (AUTHAPI-8).
    /// </summary>
    [Serializable]
    class CaptureResult
    {
        /// <summary>Rendered width in pixels (after clamping).</summary>
        [JsonProperty("width")]
        public int Width { get; set; }

        /// <summary>Rendered height in pixels (after clamping).</summary>
        [JsonProperty("height")]
        public int Height { get; set; }

        /// <summary>Image encoding; always "png".</summary>
        [JsonProperty("encoding")]
        public string Encoding { get; set; }

        /// <summary>Base64-encoded PNG bytes; null (omitted) when save_path is set without include_inline_image.</summary>
        [JsonProperty("base64", NullValueHandling = NullValueHandling.Ignore)]
        public string Base64 { get; set; }

        /// <summary>Length of the raw PNG byte array.</summary>
        [JsonProperty("bytes")]
        public int Bytes { get; set; }

        /// <summary>What was captured, e.g. "camera:Main Camera" or "sceneView".</summary>
        [JsonProperty("source")]
        public string Source { get; set; }

        /// <summary>Freshness of a composited Game view capture.</summary>
        [JsonProperty("frameStatus", NullValueHandling = NullValueHandling.Ignore)]
        public string FrameStatus { get; set; }

        /// <summary>Why the capture could contain an earlier frame.</summary>
        [JsonProperty("warning", NullValueHandling = NullValueHandling.Ignore)]
        public string Warning { get; set; }

        /// <summary>Project-relative path the PNG was also written to, or null.</summary>
        [JsonProperty("savedPath")]
        public string SavedPath { get; set; }

        /// <summary>Inline image width when max_resolution downscaled it below the saved file's; omitted otherwise.</summary>
        [JsonProperty("inlineWidth", NullValueHandling = NullValueHandling.Ignore)]
        public int? InlineWidth { get; set; }

        /// <summary>Inline image height when max_resolution downscaled it below the saved file's; omitted otherwise.</summary>
        [JsonProperty("inlineHeight", NullValueHandling = NullValueHandling.Ignore)]
        public int? InlineHeight { get; set; }
    }
}
#endif
