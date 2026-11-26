#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using Newtonsoft.Json;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Editor.Authoring;
using Unity.Pipeline.Models;
using UnityEditor;
using UnityEngine;
using UnityEngine.Rendering;
using UnityEngine.UIElements;
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

        [CliCommand("capture_game_view", "Render the game view to a PNG. source=camera (default) renders a camera and misses Screen Space - Overlay UI; source=screen captures the composited backbuffer incl. overlay canvases (Play Mode only). Returns it inline as base64, unless save_path is set (path-only result; pass include_inline_image=true to get both).", Effect = CommandEffect.Destructive, Tags = new[] { "capture" })]
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

        [CliCommand("capture_scene_view", "Capture the active Scene View to PNG. Use source=screen for its displayed window with gizmos and toolbars. Returns it inline as base64, unless save_path is set (path-only result; pass include_inline_image=true to get both).", Effect = CommandEffect.Destructive, Tags = new[] { "capture" })]
        public static CaptureResult CaptureSceneView(
            [CliArg("width", "Output width in px (default 1280; capped 4096).")] int width = 1280,
            [CliArg("height", "Output height in px (default 720; capped 4096).")] int height = 720,
            [CliArg("save_path", "Optional project-relative path to write the PNG (e.g. Screenshots/foo.png). When set, the result omits the inline base64 image unless include_inline_image=true.")] string savePath = null,
            [CliArg("include_inline_image", "Also return the image inline as base64 when save_path is set (default false: path-only result). Only meaningful together with save_path.")] bool includeInlineImage = false,
            [CliArg("max_resolution", "Cap on the inline image's longest edge (e.g. 512). Only applies when an inline image is returned (no save_path, or save_path + include_inline_image=true); the save_path file keeps the requested resolution.")] int maxResolution = 0,
            [CliArg("source", "camera (default) renders the scene camera; screen reads the displayed Scene View window.")] string source = "camera")
        {
            var src = (source ?? "camera").Trim().ToLowerInvariant();
            if (src != "camera" && src != "screen")
                throw new ArgumentException("Use 'camera' or 'screen' as the source.");
            if (src == "screen")
                GuardWindowCapture();
            else
                GuardHasGpu();

            var sv = SceneView.lastActiveSceneView;
            if (sv == null || sv.camera == null)
                throw new ArgumentException("No active Scene View to capture.");

            if (src == "screen")
                return CaptureWindow(sv, OpenWindows(), "sceneView:screen", savePath, includeInlineImage, maxResolution);

            var cam = sv.camera;
            return Capture((w, h) => EncodeCameraToPng(cam, w, h), "sceneView",
                width, height, savePath, includeInlineImage, maxResolution);
        }

        [CliCommand("capture_editor_window", "Capture the pixels of an open Editor window by type or title.", Tags = new[] { "capture" })]
        public static CaptureResult CaptureEditorWindow(
            [CliArg("window", "An open Editor window title or type.", Required = true)] string window,
            [CliArg("save_path", "Optional project-relative PNG path.")] string savePath = null,
            [CliArg("include_inline_image", "Also return the image inline when saving.")] bool includeInlineImage = false,
            [CliArg("max_resolution", "Maximum inline image edge.")] int maxResolution = 0)
        {
            GuardWindowCapture();
            var windows = OpenWindows();
            var target = RequireWindow(windows, window);
            return CaptureWindow(target, windows, $"window:{target.titleContent.text}", savePath, includeInlineImage, maxResolution);
        }

        [CliCommand("capture_editor_element", "Crop a visible UI Toolkit element from an open Editor window.", Tags = new[] { "capture", "ui" })]
        public static CaptureResult CaptureEditorElement(
            [CliArg("window", "An open Editor window title or type.", Required = true)] string window,
            [CliArg("selector", "A UI Toolkit #name or .class selector.", Required = true)] string selector,
            [CliArg("save_path", "Optional project-relative PNG path.")] string savePath = null,
            [CliArg("include_inline_image", "Also return the image inline when saving.")] bool includeInlineImage = false,
            [CliArg("max_resolution", "Maximum inline image edge.")] int maxResolution = 0)
        {
            GuardWindowCapture();
            var windows = OpenWindows();
            var target = RequireWindow(windows, window);
            if (string.IsNullOrWhiteSpace(selector) || selector.Length < 2 || (selector[0] != '#' && selector[0] != '.'))
                throw new ArgumentException("Use a #name or .class selector.");

            var previousFront = windows.FirstOrDefault(w => w != target && IsFrontTab(w) && SameHost(w, target));
            try
            {
                target.ShowTab();
                RepaintImmediately(target);
                var root = target.rootVisualElement;
                var element = selector[0] == '#' ? root.Q(selector.Substring(1)) : root.Q(className: selector.Substring(1));
                if (element == null || element.panel == null)
                    throw new ArgumentException($"No attached element matches '{selector}'.");
                var bounds = element.worldBound;
                for (var parent = element; parent != null; parent = parent.parent)
                {
                    var style = parent.resolvedStyle;
                    if (style.display == DisplayStyle.None || style.visibility != Visibility.Visible || style.opacity <= 0)
                        throw new InvalidOperationException($"Element '{selector}' is hidden.");
                    if (parent == root || parent is ScrollView || parent.style.overflow.value == Overflow.Hidden)
                    {
                        var clip = parent.worldBound;
                        bounds = Rect.MinMaxRect(Mathf.Max(bounds.xMin, clip.xMin), Mathf.Max(bounds.yMin, clip.yMin),
                            Mathf.Min(bounds.xMax, clip.xMax), Mathf.Min(bounds.yMax, clip.yMax));
                    }
                }

                var scale = EditorGUIUtility.pixelsPerPoint;
                var (pixelW, pixelH) = WindowPixelSize(target);
                var left = Mathf.Clamp(Mathf.FloorToInt(bounds.xMin * scale), 0, pixelW);
                var right = Mathf.Clamp(Mathf.CeilToInt(bounds.xMax * scale), 0, pixelW);
                var top = Mathf.Clamp(Mathf.FloorToInt(bounds.yMin * scale), 0, pixelH);
                var bottom = Mathf.Clamp(Mathf.CeilToInt(bounds.yMax * scale), 0, pixelH);
                if (right <= left || bottom <= top)
                    throw new InvalidOperationException($"Element '{selector}' has no visible pixels.");

                var windowImage = RenderTexture.GetTemporary(pixelW, pixelH, 0, RenderTextureFormat.ARGB32);
                try
                {
                    if (!GrabEditorWindow(target, windowImage))
                        throw new InvalidOperationException($"Unable to read window '{window}'.");
                    var crop = new Rect((float)left / pixelW, (float)(pixelH - bottom) / pixelH,
                        (float)(right - left) / pixelW, (float)(bottom - top) / pixelH);
                    return Capture((w, h) => EncodeTextureRegion(windowImage, crop, w, h),
                        $"element:{target.titleContent.text}:{selector}", right - left, bottom - top, savePath, includeInlineImage, maxResolution);
                }
                finally
                {
                    RenderTexture.ReleaseTemporary(windowImage);
                }
            }
            finally
            {
                if (previousFront != null)
                    previousFront.ShowTab();
            }
        }

        [CliCommand("capture_asset_preview", "Capture an imported texture or Unity-generated asset thumbnail to PNG.", Tags = new[] { "capture", "assets" })]
        public static CaptureResult CaptureAssetPreview(
            [CliArg("asset", "Asset reference by path, global ID or GUID.", Required = true)] ObjectRef asset,
            [CliArg("save_path", "Optional project-relative PNG path.")] string savePath = null,
            [CliArg("include_inline_image", "Also return the image inline when saving.")] bool includeInlineImage = false,
            [CliArg("max_resolution", "Maximum inline image edge.")] int maxResolution = 0)
        {
            GuardHasGpu();
            if (!ObjectResolver.TryResolve(asset, out var obj, out var error))
                throw new ArgumentException(error);
            if (!EditorUtility.IsPersistent(obj))
                throw new ArgumentException("Expected an imported asset.");
            var preview = obj as Texture2D ?? AssetPreview.GetAssetPreview(obj);
            if (preview == null)
                throw new InvalidOperationException("Unity has no rendered preview yet. Retry after the preview has loaded.");
            var (width, height) = ClampToLongEdge(preview.width, preview.height, MaxDimension);
            return Capture((w, h) => EncodeTextureRegion(preview, new Rect(0, 0, 1, 1), w, h),
                $"asset:{AssetDatabase.GetAssetPath(obj)}", width, height, savePath, includeInlineImage, maxResolution);
        }

        private static byte[] EncodeTextureRegion(Texture source, Rect crop, int width, int height)
        {
            var target = RenderTexture.GetTemporary(width, height, 0, RenderTextureFormat.ARGB32);
            var previous = RenderTexture.active;
            var image = new Texture2D(width, height, TextureFormat.RGBA32, false);
            try
            {
                Graphics.Blit(source, target, crop.size, crop.position);
                RenderTexture.active = target;
                image.ReadPixels(new Rect(0, 0, width, height), 0, 0);
                image.Apply();
                return image.EncodeToPNG();
            }
            finally
            {
                RenderTexture.active = previous;
                RenderTexture.ReleaseTemporary(target);
                Object.DestroyImmediate(image);
            }
        }

        private static EditorWindow[] OpenWindows() => Resources.FindObjectsOfTypeAll<EditorWindow>();

        private static EditorWindow RequireWindow(EditorWindow[] windows, string window)
        {
            if (string.IsNullOrWhiteSpace(window))
                throw new ArgumentException("An Editor window title or type is required.");
            return FindEditorWindow(windows, window)
                ?? throw new ArgumentException($"No open Editor window matches '{window}'.");
        }

        private static void GuardWindowCapture()
        {
            if (Application.isBatchMode)
                throw new InvalidOperationException("Editor windows are not drawn in batch mode.");
            GuardHasGpu();
        }

        /// <summary>
        /// Capture <paramref name="target"/> as drawn, at its size in pixels. Only the visible tab of a
        /// dock area has pixels: a background tab is brought to the front and drawn now, so its back
        /// buffer holds the window when it is read, then the tab that was in front is put back.
        /// <paramref name="windows"/> are the open windows, to find that tab.
        /// </summary>
        private static CaptureResult CaptureWindow(UnityEditor.EditorWindow target, IEnumerable<UnityEditor.EditorWindow> windows,
            string source, string savePath, bool includeInlineImage, int maxResolution)
        {
            var previousFront = windows.FirstOrDefault(w => w != target && IsFrontTab(w) && SameHost(w, target));
            target.ShowTab();
            RepaintImmediately(target);
            try
            {
                // The window is grabbed at its full size; only the image is capped, keeping its aspect.
                var (pixelW, pixelH) = WindowPixelSize(target);
                var (outW, outH) = ClampToLongEdge(pixelW, pixelH, MaxDimension);
                return Capture((w, h) => EncodeEditorWindowToPng(target, w, h), source,
                    outW, outH, savePath, includeInlineImage, maxResolution);
            }
            finally
            {
                if (previousFront != null)
                    previousFront.ShowTab();
            }
        }

        // The view hosting the window: the dock area for a docked tab. Null when the window is closed.
        private static Object HostView(UnityEditor.EditorWindow window)
            => typeof(UnityEditor.EditorWindow).GetField("m_Parent", BindingFlags.Instance | BindingFlags.NonPublic)
                ?.GetValue(window) as Object;

        private static bool IsFrontTab(EditorWindow window)
        {
            var host = HostView(window);
            return host?.GetType().GetProperty("actualView", BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic)
                ?.GetValue(host) as EditorWindow == window;
        }

        private static bool SameHost(UnityEditor.EditorWindow a, UnityEditor.EditorWindow b)
        {
            var host = HostView(a);
            return host != null && host == HostView(b);
        }

        /// <summary>
        /// The open window <paramref name="window"/> names, by title or type name (case-insensitive),
        /// or null. <c>FindObjectsOfTypeAll</c> also returns closed windows still waiting to be
        /// destroyed, so only a window in a dock area or window of its own is a match; among matches,
        /// the one already in front wins.
        /// </summary>
        internal static UnityEditor.EditorWindow FindEditorWindow(IEnumerable<UnityEditor.EditorWindow> windows, string window)
        {
            var name = window.Trim();
            return windows
                .Where(IsDrawable)
                .Where(w => string.Equals(w.titleContent?.text, name, StringComparison.OrdinalIgnoreCase)
                    || string.Equals(w.GetType().Name, name, StringComparison.OrdinalIgnoreCase))
                .OrderByDescending(IsFrontTab)
                .FirstOrDefault();
        }

        // A window with no host view is closed or not shown yet: it has nothing to draw.
        private static bool IsDrawable(UnityEditor.EditorWindow window)
            => window != null && window.position.width >= 1f && window.position.height >= 1f && HostView(window) != null;

        /// <summary>
        /// The window's size in pixels: its position is in points, scaled by the display's DPI.
        /// <c>pixelsPerPoint</c> is the scale of the last view drawn, so this is right for a window on
        /// any monitor only because the window has just been drawn (see <see cref="CaptureWindow"/>).
        /// Not capped: it is the grab rectangle, and a smaller one would crop the window.
        /// </summary>
        private static (int w, int h) WindowPixelSize(UnityEditor.EditorWindow window)
        {
            var scale = EditorGUIUtility.pixelsPerPoint;
            return (Mathf.Max(1, Mathf.RoundToInt(window.position.width * scale)),
                Mathf.Max(1, Mathf.RoundToInt(window.position.height * scale)));
        }

        /// <summary>
        /// Copy the window's back buffer into <paramref name="target"/>, sized in pixels, the way
        /// <c>InternalEditorUtility.CaptureEditorWindow</c> does: grab it from the window's host view,
        /// then blit it through the same material, which flips it where the graphics API needs it.
        /// That public method passes the window's size in points as the grab rect, which crops the
        /// window whenever the display is scaled (at 150% it reads two thirds of it), so it is only
        /// the fallback when the internal grab is unavailable. Returns false when the window is not
        /// the visible tab of its dock area.
        /// </summary>
        private static bool GrabEditorWindow(UnityEditor.EditorWindow window, RenderTexture target)
        {
            if (!IsFrontTab(window))
                return false;

            object parent = null;
            MethodInfo grab = null;
            Material blit = null;
            try
            {
                parent = HostView(window);
                grab = parent?.GetType().GetMethod("GrabPixels", BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public);
                blit = EditorGUIUtility.LoadRequired("SceneView/BlitSceneViewCapture.mat") as Material;
            }
            catch
            {
                // Internal API changed: fall back to the public capture below.
            }

            if (grab == null || blit == null)
            {
                // Through reflection too: the public method is newer than the oldest supported Editor.
                var capture = typeof(UnityEditorInternal.InternalEditorUtility).GetMethod("CaptureEditorWindow",
                    BindingFlags.Static | BindingFlags.Public, null, new[] { typeof(UnityEditor.EditorWindow), typeof(RenderTexture) }, null);
                return capture != null && (bool)capture.Invoke(null, new object[] { window, target });
            }

            var grabbed = RenderTexture.GetTemporary(target.descriptor);
            try
            {
                grab.Invoke(parent, new object[] { grabbed, new Rect(0, 0, target.width, target.height) });
                Graphics.Blit(grabbed, target, blit);
                return true;
            }
            finally
            {
                RenderTexture.ReleaseTemporary(grabbed);
            }
        }

        // Draws the window now. False when the internal API is unavailable: the window then still holds
        // its last drawn frame.
        private static bool RepaintImmediately(UnityEditor.EditorWindow window)
        {
            try
            {
                var repaint = typeof(UnityEditor.EditorWindow).GetMethod("RepaintImmediately", BindingFlags.Instance | BindingFlags.NonPublic);
                if (repaint == null)
                    return false;

                repaint.Invoke(window, null);
                return true;
            }
            catch
            {
                return false;
            }
        }

        /// <summary>
        /// Read the window's back buffer, orient it, then scale it to <paramref name="w"/> x
        /// <paramref name="h"/> and encode it. See <see cref="GrabEditorWindow"/>.
        /// </summary>
        private static byte[] EncodeEditorWindowToPng(UnityEditor.EditorWindow window, int w, int h)
        {
            var (pixelW, pixelH) = WindowPixelSize(window);
            var grabRt = RenderTexture.GetTemporary(pixelW, pixelH, 0, RenderTextureFormat.ARGB32);
            RenderTexture outRt = null;
            var prevActive = RenderTexture.active;
            try
            {
                if (!GrabEditorWindow(window, grabRt))
                    throw new InvalidOperationException($"'{window.titleContent.text}' could not be read: it is not the visible tab of its window.");

                var readRt = grabRt;
                if (w != pixelW || h != pixelH)
                {
                    outRt = RenderTexture.GetTemporary(w, h, 0, RenderTextureFormat.ARGB32);
                    Graphics.Blit(grabRt, outRt);
                    readRt = outRt;
                }

                RenderTexture.active = readRt;
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
                RenderTexture.ReleaseTemporary(grabRt);
                if (outRt != null)
                    RenderTexture.ReleaseTemporary(outRt);
            }
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
