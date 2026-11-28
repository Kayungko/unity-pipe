#if UNITY_EDITOR || (UPIPE_SCENE_PREVIEW && DEVELOPMENT_BUILD && UNITY_STANDALONE_WIN)
using System;
using System.IO;
using UnityEngine;
#if !UNITY_EDITOR
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using UnityEngine.Rendering;
using UnityEngine.SceneManagement;

// A preview-only host can have no scene reference to this assembly. Let UnityLinker
// process its runtime initialization callback even when Pipeline networking is disabled.
[assembly: UnityEngine.Scripting.AlwaysLinkAssembly]
#endif

namespace Unity.Pipeline.Preview
{
    [Serializable]
    public sealed class PreviewPackage
    {
        public const int CurrentSchema = 1;
        public const string FileName = "preview-package.json";
        public const string CatalogName = "preview-catalog";
        public const string WindowsTarget = "StandaloneWindows64";
        public int schema;
        public string target;
        public string unityVersion;
        public string projectGuid;
        public string asset;
        public string replacementKind;
        public bool imageReplacement;
        public string bundle;
        public string[] dependencies;

        // UGUI remains optional in projects that only use the Renderer or Prefab preview.
        internal static Type FindImageType() => Type.GetType("UnityEngine.UI.Image, UnityEngine.UI");

        public static PreviewPackage Read(string directory)
        {
            var package = JsonUtility.FromJson<PreviewPackage>(File.ReadAllText(Path.Combine(directory, FileName)));
            if (package == null || package.schema != CurrentSchema || string.IsNullOrEmpty(package.asset) ||
                !IsFileName(package.bundle) || package.dependencies == null || Array.Exists(package.dependencies, name => !IsFileName(name)))
                throw new InvalidDataException("Invalid preview package metadata");
            return package;
        }

        private static bool IsFileName(string name) =>
            !string.IsNullOrEmpty(name) && name != "." && name != ".." &&
            name.IndexOfAny(new[] { '/', '\\', ':' }) < 0 && name.IndexOfAny(Path.GetInvalidFileNameChars()) < 0;
    }

#if !UNITY_EDITOR
    static class ScenePreview
    {
        private const int ImageSize = 768;
        private const int PreviewLayer = 31;

        [Serializable]
        private class Result
        {
            public bool success;
            public string stage;
            public string message;
            public string asset;
            public string graphicsDevice;
            public string renderMode;
            public int width;
            public int height;
            public int canvases;
            public int renderers;
            public int missingScripts;
            public string[] behaviourTypes;
        }

        [Serializable]
        private class ReplacementResult : Result
        {
            public string scene;
            public string node;
            public string component;
            public string replacementKind;
            public string beforeAsset;
            public string afterAsset;
            public int materialSlot;
            public int beforeFrame;
            public int afterFrame;
        }

        [Serializable]
        private sealed class UrpReplacementResult : ReplacementResult
        {
            public string renderPipeline = "URP";
            public string camera;
        }

        [RuntimeInitializeOnLoadMethod(RuntimeInitializeLoadType.AfterSceneLoad)]
        private static void Initialize()
        {
            var args = Environment.GetCommandLineArgs();
            var index = Array.IndexOf(args, "-upipe-scene-preview");
            if (index < 0 || args.Length <= index + 2)
                return;
            var width = ImageSize;
            var height = ImageSize;
            if (args.Length > index + 4)
            {
                int.TryParse(args[index + 3], out width);
                int.TryParse(args[index + 4], out height);
            }
            var node = args.Length > index + 6 ? args[index + 5] : null;
            var slot = 0;
            if (node != null && !int.TryParse(args[index + 6], out slot))
                slot = -1;
            var image = args.Length > index + 7 && args[index + 7] == "image";
            _ = Run(args[index + 1], args[index + 2], width, height, node, slot, image);
        }

        private static async Task Run(string directory, string output, int width, int height, string node, int slot, bool image)
        {
            var pipeline = GraphicsSettings.currentRenderPipeline;
            var urp = pipeline != null &&
                pipeline.GetType().FullName == "UnityEngine.Rendering.Universal.UniversalRenderPipelineAsset";
            Result result = node == null ? new Result() : urp ? new UrpReplacementResult() : new ReplacementResult();
            result.stage = "validate";
            result.graphicsDevice = SystemInfo.graphicsDeviceType.ToString();
            result.width = width;
            result.height = height;
            var bundles = new List<AssetBundle>();
            var scene = default(Scene);
            try
            {
                if (width < 64 || width > 4096 || height < 64 || height > 4096)
                    throw new ArgumentException("Preview width and height must be between 64 and 4096");
                var package = PreviewPackage.Read(directory);
                result.asset = package.asset;
                if (package.target != PreviewPackage.WindowsTarget || package.unityVersion != Application.unityVersion)
                    throw new InvalidDataException("Preview package target or Unity version does not match this Player");
                if (SystemInfo.graphicsDeviceType == GraphicsDeviceType.Null)
                    throw new InvalidOperationException("Scene Preview requires a graphics device");
                if (pipeline != null && !urp)
                    throw new InvalidOperationException("Scene Preview requires the Built-in Render Pipeline or URP");
                if (urp && (node == null || image))
                    throw new InvalidOperationException("URP preview supports scene SpriteRenderer and Renderer replacements. Prefab and UGUI Image preview require the Built-in Render Pipeline.");
                if (string.IsNullOrEmpty(package.replacementKind) != (node == null))
                    throw new ArgumentException("A scene replacement requires a Sprite or Material bundle and a target node");

                var host = SceneManager.GetSceneByBuildIndex(0);
                result.stage = "dependency";
                foreach (var dependency in package.dependencies)
                    await LoadBundle(directory, dependency, bundles);
                result.stage = "bundle";
                var root = await LoadBundle(directory, package.bundle, bundles);
                if (node != null)
                {
                    await RenderReplacement(host, root, package, node, slot, output, (ReplacementResult)result, image);
                }
                else
                {
                    var load = root.LoadAssetAsync<GameObject>(package.asset);
                    await Wait(load);
                    var prefab = load.asset as GameObject;
                    if (prefab == null)
                        throw new InvalidDataException("Preview Prefab is absent from its bundle");
                    result.stage = "scene";
                    scene = SceneManager.CreateScene("UPipe Scene Preview", new CreateSceneParameters(LocalPhysicsMode.Physics3D));
                    await RenderPrefab(scene, prefab, output, result);
                }
                result.success = true;
                result.stage = "completed";
            }
            catch (Exception ex)
            {
                result.message = ex.Message;
            }
            finally
            {
                try
                {
                    if (scene.IsValid())
                        await Wait(SceneManager.UnloadSceneAsync(scene));
                    for (var i = bundles.Count - 1; i >= 0; --i)
                        bundles[i].Unload(true);
                }
                catch (Exception ex)
                {
                    result.success = false;
                    result.stage = "cleanup";
                    result.message = ex.Message;
                }
                try
                {
                    File.WriteAllText(Path.Combine(output, "result.json"), JsonUtility.ToJson(result));
                }
                finally
                {
                    Application.Quit(result.success ? 0 : 1);
                }
            }
        }

        private static async Task RenderPrefab(Scene scene, GameObject prefab, string output, Result result)
        {
            var stage = CreateRoot("Preview", scene);
            stage.SetActive(false);
            var clone = UnityEngine.Object.Instantiate(prefab, stage.transform, false);
            foreach (var transform in clone.GetComponentsInChildren<Transform>(true))
                transform.gameObject.layer = PreviewLayer;
            var behaviours = clone.GetComponentsInChildren<MonoBehaviour>(true);
            result.behaviourTypes = behaviours.Where(item => item != null).Select(item => item.GetType().FullName).Distinct().ToArray();
            result.missingScripts = behaviours.Count(item => item == null);
            if (result.missingScripts > 0)
                throw new InvalidDataException("Prefab contains missing Player script types");
            stage.SetActive(true);
            await Task.Yield();

            result.stage = "render";
            var rootCanvases = clone.GetComponentsInChildren<Canvas>()
                .Where(canvas => canvas.isActiveAndEnabled && canvas.isRootCanvas)
                .ToArray();
            var screenCanvases = rootCanvases.Where(canvas => canvas.renderMode != RenderMode.WorldSpace).ToArray();
            var camera = CreateCamera(scene, result.width, result.height);
            if (screenCanvases.Length > 0)
            {
                result.renderMode = "canvas";
                result.canvases = screenCanvases.Length;
                foreach (var canvas in screenCanvases)
                {
                    canvas.renderMode = RenderMode.ScreenSpaceCamera;
                    canvas.worldCamera = camera;
                    canvas.targetDisplay = 0;
                    canvas.planeDistance = 0.5f;
                }
            }
            else
            {
                var renderers = clone.GetComponentsInChildren<Renderer>().Where(item => item.enabled).ToArray();
                if (renderers.Length > 0)
                {
                    result.renderMode = "mesh";
                    result.renderers = renderers.Length;
                    FrameMesh(camera, scene, renderers);
                }
                else
                {
                    result.renderMode = "worldCanvas";
                    Canvas.ForceUpdateCanvases();
                    result.canvases = FrameWorldCanvases(camera, rootCanvases);
                }
            }
            File.WriteAllBytes(Path.Combine(output, "preview.png"), Capture(camera, result.width, result.height));
        }

        private static async Task RenderReplacement(Scene host, AssetBundle bundle, PreviewPackage package, string node,
            int slot, string output, ReplacementResult result, bool image)
        {
            result.renderMode = "sceneReplacement";
            result.node = node;
            result.replacementKind = package.replacementKind;
            result.stage = "scene";
            if (!host.IsValid() || !host.isLoaded)
                throw new InvalidOperationException("The preview host scene is not loaded");
            result.scene = host.path;

            var target = FindNode(host, node);
            if (!target.activeInHierarchy)
                throw new InvalidOperationException("Target node is inactive: " + node);
            if (image)
            {
                result.stage = "replacement";
                await RenderImageReplacement(host, target, bundle, package, output, result);
                return;
            }
            var urp = result as UrpReplacementResult;
            var camera = FindHostCamera(host, urp != null);
            if (urp != null)
                urp.camera = camera.name;
            result.stage = "replacement";
            if (package.replacementKind == "sprite")
            {
                if (slot != 0)
                    throw new ArgumentException("Material slots only apply to Material replacement");
                var renderers = target.GetComponents<SpriteRenderer>();
                if (renderers.Length != 1 || !renderers[0].enabled)
                    throw new InvalidOperationException("Target node needs exactly one enabled SpriteRenderer");
                var renderer = renderers[0];
                var replacement = await LoadReplacementSprite(bundle, package.asset);
                var original = renderer.sprite;
                result.component = nameof(SpriteRenderer);
                result.beforeAsset = original != null ? original.name : null;
                result.afterAsset = replacement.name;
                CaptureReplacement(camera, output, result, () => renderer.sprite = replacement, () => renderer.sprite = original);
            }
            else if (package.replacementKind == "material")
            {
                var renderers = target.GetComponents<Renderer>();
                if (renderers.Length != 1 || !renderers[0].enabled)
                    throw new InvalidOperationException("Target node needs exactly one enabled Renderer");
                var renderer = renderers[0];
                var replacement = await LoadReplacementMaterial(bundle, package.asset);
                var original = renderer.sharedMaterials;
                if (slot < 0 || slot >= original.Length)
                    throw new ArgumentOutOfRangeException(nameof(slot), "Material slot is outside the renderer's material array");
                var updated = (Material[])original.Clone();
                updated[slot] = replacement;
                result.component = renderer.GetType().Name;
                result.materialSlot = slot;
                result.beforeAsset = original[slot] != null ? original[slot].name : null;
                result.afterAsset = replacement.name;
                CaptureReplacement(camera, output, result, () => renderer.sharedMaterials = updated, () => renderer.sharedMaterials = original);
            }
            else
            {
                throw new InvalidDataException("Unknown replacement kind in the preview bundle");
            }
        }

        private static async Task<Sprite> LoadReplacementSprite(AssetBundle bundle, string asset)
        {
            var load = bundle.LoadAssetWithSubAssetsAsync<Sprite>(asset);
            await Wait(load);
            var sprites = load.allAssets.OfType<Sprite>().ToArray();
            if (sprites.Length != 1)
                throw new InvalidDataException("Expected exactly one Sprite in the preview bundle");
            return sprites[0];
        }

        private static async Task<Material> LoadReplacementMaterial(AssetBundle bundle, string asset)
        {
            var load = bundle.LoadAssetAsync<Material>(asset);
            await Wait(load);
            var material = load.asset as Material;
            if (material == null)
                throw new InvalidDataException("Material is absent from the preview bundle");
            return material;
        }

        private static Camera FindHostCamera(Scene host, bool urp = false)
        {
            var hostCameras = Camera.allCameras.Where(camera => camera.gameObject.scene == host);
            var cameras = urp ? FindUrpBaseCameras(hostCameras) : hostCameras.ToArray();
            var main = cameras.Where(camera => camera.CompareTag("MainCamera")).ToArray();
            var candidates = main.Length > 0 ? main : cameras;
            if (candidates.Length != 1)
                throw new InvalidOperationException(urp
                    ? "The host scene needs one enabled URP Base camera tagged MainCamera or exactly one enabled Base camera"
                    : "The host scene needs one enabled MainCamera or one enabled camera");
            return candidates[0];
        }

        private static Camera[] FindUrpBaseCameras(IEnumerable<Camera> cameras)
        {
            var cameraDataType = Type.GetType(
                "UnityEngine.Rendering.Universal.UniversalAdditionalCameraData, Unity.RenderPipelines.Universal.Runtime");
            var renderType = cameraDataType?.GetProperty("renderType");
            if (renderType == null)
                throw new InvalidOperationException("Could not determine the URP camera render type in this Player");
            return cameras.Where(camera =>
            {
                // URP treats a camera without additional camera data as a Base camera.
                var data = camera.GetComponent(cameraDataType);
                return data == null || renderType.GetValue(data)?.ToString() == "Base";
            }).ToArray();
        }

        private static async Task RenderImageReplacement(Scene host, GameObject target, AssetBundle bundle,
            PreviewPackage package, string output, ReplacementResult result)
        {
            var imageType = PreviewPackage.FindImageType();
            if (imageType == null)
                throw new InvalidOperationException("UGUI Image is unavailable in this preview Player");
            var images = target.GetComponents(imageType);
            if (images.Length != 1 || !(images[0] is Behaviour image) || !image.isActiveAndEnabled)
                throw new InvalidOperationException("Target node needs exactly one enabled UGUI Image");
            var canvas = image.GetComponentInParent<Canvas>()?.rootCanvas;
            if (canvas == null || !canvas.isActiveAndEnabled)
                throw new InvalidOperationException("The UGUI Image needs an enabled root Canvas");
            var camera = canvas.renderMode == RenderMode.ScreenSpaceCamera ? canvas.worldCamera : FindHostCamera(host);
            if (camera == null || !camera.isActiveAndEnabled || camera.gameObject.scene != host)
                throw new InvalidOperationException("The UGUI Canvas needs an enabled camera in the host scene");

            result.component = "Image";
            if (package.replacementKind == "sprite")
            {
                var replacement = await LoadReplacementSprite(bundle, package.asset);
                var property = imageType.GetProperty("overrideSprite");
                var original = (Sprite)property.GetValue(image);
                // overrideSprite reads the effective sprite even when there is no override.
                var baseSprite = (Sprite)imageType.GetProperty("sprite").GetValue(image);
                var restore = original == baseSprite ? null : original;
                result.beforeAsset = original != null ? original.name : null;
                result.afterAsset = replacement.name;
                CaptureImageReplacement(host, camera, output, result,
                    () => property.SetValue(image, replacement), () => property.SetValue(image, restore));
            }
            else if (package.replacementKind == "material")
            {
                var replacement = await LoadReplacementMaterial(bundle, package.asset);
                var property = imageType.GetProperty("material");
                var original = (Material)property.GetValue(image);
                result.beforeAsset = original != null ? original.name : null;
                result.afterAsset = replacement.name;
                // Let UGUI derive the stencil material from Image.material on each rebuild.
                CaptureImageReplacement(host, camera, output, result,
                    () => property.SetValue(image, replacement), () => property.SetValue(image, original));
            }
            else
            {
                throw new InvalidDataException("Unknown replacement kind in the preview bundle");
            }
        }

        private static void CaptureImageReplacement(Scene host, Camera camera, string output, ReplacementResult result,
            Action replace, Action restore)
        {
            var overlays = UnityEngine.Object.FindObjectsOfType<Canvas>()
                .Where(canvas => canvas.isActiveAndEnabled && canvas.isRootCanvas && canvas.gameObject.scene == host &&
                    canvas.renderMode == RenderMode.ScreenSpaceOverlay && canvas.targetDisplay == camera.targetDisplay)
                .Select(canvas => (canvas, canvas.worldCamera, canvas.planeDistance)).ToArray();
            try
            {
                // Screen-space Overlay is composed after camera rendering. Use the same temporary
                // camera binding for both captures so the selected display's UI stays in the shot.
                foreach (var overlay in overlays)
                {
                    overlay.canvas.renderMode = RenderMode.ScreenSpaceCamera;
                    overlay.canvas.worldCamera = camera;
                    overlay.canvas.planeDistance = camera.nearClipPlane +
                        Mathf.Min(0.01f, (camera.farClipPlane - camera.nearClipPlane) * 0.5f);
                }
                CaptureReplacement(camera, output, result, replace, restore);
            }
            finally
            {
                foreach (var overlay in overlays)
                {
                    overlay.canvas.renderMode = RenderMode.ScreenSpaceOverlay;
                    overlay.canvas.worldCamera = overlay.worldCamera;
                    overlay.canvas.planeDistance = overlay.planeDistance;
                }
            }
        }

        private static GameObject FindNode(Scene host, string path)
        {
            var parts = path.Split('/');
            if (parts.Length < 2 || parts[0] != "" || parts.Skip(1).Any(string.IsNullOrEmpty))
                throw new ArgumentException("Expected an absolute node path such as /Stage/Target");
            var siblings = host.GetRootGameObjects().Select(root => root.transform);
            Transform current = null;
            foreach (var part in parts.Skip(1))
            {
                var matches = siblings.Where(item => item.name == part).Take(2).ToArray();
                if (matches.Length == 0)
                    throw new InvalidOperationException("Target node was not found: " + path);
                if (matches.Length > 1)
                    throw new InvalidOperationException("Target node path is ambiguous at: " + part);
                current = matches[0];
                siblings = current.Cast<Transform>();
            }
            return current.gameObject;
        }

        private static void CaptureReplacement(Camera camera, string output, ReplacementResult result, Action replace, Action restore)
        {
            result.stage = "render";
            result.beforeFrame = Time.frameCount;
            var urp = result is UrpReplacementResult;
            var before = Capture(camera, result.width, result.height, urp);
            byte[] after;
            try
            {
                replace();
                result.afterFrame = Time.frameCount;
                after = Capture(camera, result.width, result.height, urp);
            }
            finally
            {
                restore();
            }
            File.WriteAllBytes(Path.Combine(output, "before.png"), before);
            File.WriteAllBytes(Path.Combine(output, "preview.png"), after);
        }

        private static GameObject CreateRoot(string name, Scene scene)
        {
            var root = new GameObject(name) { layer = PreviewLayer };
            SceneManager.MoveGameObjectToScene(root, scene);
            return root;
        }

        private static Camera CreateCamera(Scene scene, int width, int height)
        {
            var camera = CreateRoot("Preview Camera", scene).AddComponent<Camera>();
            camera.enabled = false;
            camera.orthographic = true;
            camera.cullingMask = 1 << PreviewLayer;
            camera.clearFlags = CameraClearFlags.SolidColor;
            camera.backgroundColor = new Color(0.12f, 0.14f, 0.17f);
            camera.aspect = (float)width / height;
            camera.nearClipPlane = 0.01f;
            return camera;
        }

        private static void FrameMesh(Camera camera, Scene scene, Renderer[] renderers)
        {
            var bounds = renderers[0].bounds;
            foreach (var renderer in renderers.Skip(1))
                bounds.Encapsulate(renderer.bounds);
            var radius = Mathf.Max(bounds.extents.magnitude, 0.05f);
            camera.transform.position = bounds.center + new Vector3(1, 0.72f, -1.4f).normalized * radius * 4;
            camera.transform.LookAt(bounds.center);
            camera.nearClipPlane = Mathf.Max(radius * 0.02f, 0.001f);
            camera.farClipPlane = radius * 8;
            camera.orthographicSize = radius * 1.12f * Mathf.Max(1, 1 / camera.aspect);

            var light = CreateRoot("Preview Light", scene).AddComponent<Light>();
            light.type = LightType.Directional;
            light.cullingMask = camera.cullingMask;
            light.intensity = 1.25f;
            light.transform.rotation = Quaternion.Euler(42, -35, 0);
        }

        private static int FrameWorldCanvases(Camera camera, Canvas[] canvases)
        {
            var orientation = Quaternion.identity;
            var inverse = Quaternion.identity;
            var bounds = default(Bounds);
            var count = 0;
            var corners = new Vector3[4];
            foreach (var canvas in canvases)
            {
                if (!(canvas.transform is RectTransform rect))
                    continue;
                rect.GetWorldCorners(corners);
                var right = corners[3] - corners[0];
                var up = corners[1] - corners[0];
                var area = Vector3.Cross(right, up).sqrMagnitude;
                if (float.IsNaN(area) || float.IsInfinity(area) || area <= 0)
                    continue;
                if (count == 0)
                {
                    // Project all roots into the first drawable Canvas's authored orientation.
                    orientation = rect.rotation;
                    inverse = Quaternion.Inverse(orientation);
                    bounds = new Bounds(inverse * corners[0], Vector3.zero);
                }
                foreach (var corner in corners)
                    bounds.Encapsulate(inverse * corner);
                count++;
            }
            if (count == 0)
                throw new InvalidDataException("Prefab needs an enabled Renderer or a non-empty root Canvas");

            var distance = Mathf.Max(0.05f, bounds.extents.magnitude);
            var position = bounds.center - Vector3.forward * (bounds.extents.z + distance);
            camera.transform.SetPositionAndRotation(orientation * position, orientation);
            camera.farClipPlane = bounds.size.z + distance * 2f;
            camera.orthographicSize = Mathf.Max(bounds.extents.y, bounds.extents.x / camera.aspect) * 1.12f;
            return count;
        }

        private static byte[] Capture(Camera camera, int width, int height, bool urp = false)
        {
            var target = RenderTexture.GetTemporary(width, height, 24, RenderTextureFormat.ARGB32);
            var previous = RenderTexture.active;
            var previousTarget = camera.targetTexture;
            var pixels = new Texture2D(width, height, TextureFormat.RGB24, false);
            try
            {
                if (urp)
                {
                    var request = new RenderPipeline.StandardRequest { destination = target };
                    if (!RenderPipeline.SupportsRenderRequest(camera, request))
                        throw new InvalidOperationException("The URP Base camera does not support a full camera stack render request");
                    RenderPipeline.SubmitRenderRequest(camera, request);
                }
                else
                {
                    camera.targetTexture = target;
                    Canvas.ForceUpdateCanvases();
                    camera.Render();
                }
                RenderTexture.active = target;
                pixels.ReadPixels(new Rect(0, 0, width, height), 0, 0);
                return ImageConversion.EncodeToPNG(pixels);
            }
            finally
            {
                camera.targetTexture = previousTarget;
                RenderTexture.active = previous;
                RenderTexture.ReleaseTemporary(target);
                UnityEngine.Object.Destroy(pixels);
            }
        }

        private static async Task Wait(AsyncOperation operation)
        {
            if (operation == null)
                throw new InvalidOperationException("Unity did not start the requested preview operation");
            while (!operation.isDone)
                await Task.Yield();
        }

        private static async Task<AssetBundle> LoadBundle(string directory, string file, List<AssetBundle> bundles)
        {
            var path = Path.Combine(directory, file);
            if (!File.Exists(path))
                throw new FileNotFoundException("Preview bundle is missing: " + file);
            var load = AssetBundle.LoadFromFileAsync(path);
            await Wait(load);
            if (load.assetBundle == null)
                throw new InvalidDataException("Unity could not load preview bundle: " + file);
            bundles.Add(load.assetBundle);
            return load.assetBundle;
        }
    }
#endif
}
#endif
