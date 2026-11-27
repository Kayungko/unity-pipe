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
        public string bundle;
        public string[] dependencies;

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
        private sealed class Result
        {
            public bool success;
            public string stage;
            public string message;
            public string asset;
            public string graphicsDevice;
            public int renderers;
            public int missingScripts;
            public string[] behaviourTypes;
        }

        [RuntimeInitializeOnLoadMethod(RuntimeInitializeLoadType.AfterSceneLoad)]
        private static void Initialize()
        {
            var args = Environment.GetCommandLineArgs();
            var index = Array.IndexOf(args, "-upipe-scene-preview");
            if (index >= 0 && args.Length > index + 2)
                _ = Run(args[index + 1], args[index + 2]);
        }

        private static async Task Run(string directory, string output)
        {
            var result = new Result { stage = "validate", graphicsDevice = SystemInfo.graphicsDeviceType.ToString() };
            var bundles = new List<AssetBundle>();
            var scene = default(Scene);
            try
            {
                var package = PreviewPackage.Read(directory);
                result.asset = package.asset;
                if (package.target != PreviewPackage.WindowsTarget || package.unityVersion != Application.unityVersion)
                    throw new InvalidDataException("Preview package target or Unity version does not match this Player");
                if (SystemInfo.graphicsDeviceType == GraphicsDeviceType.Null || GraphicsSettings.currentRenderPipeline != null)
                    throw new InvalidOperationException("Scene Preview requires the Built-in Render Pipeline and a graphics device");

                result.stage = "dependency";
                foreach (var dependency in package.dependencies)
                    await LoadBundle(directory, dependency, bundles);
                result.stage = "bundle";
                var root = await LoadBundle(directory, package.bundle, bundles);
                var load = root.LoadAssetAsync<GameObject>(package.asset);
                await Wait(load);
                var prefab = load.asset as GameObject;
                if (prefab == null)
                    throw new InvalidDataException("Preview Prefab is absent from its bundle");

                result.stage = "scene";
                scene = SceneManager.CreateScene("UPipe Scene Preview", new CreateSceneParameters(LocalPhysicsMode.Physics3D));
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
                var renderers = clone.GetComponentsInChildren<Renderer>().Where(item => item.enabled).ToArray();
                if (renderers.Length == 0)
                    throw new InvalidDataException("Prefab has no enabled 3D Renderer to frame");
                result.renderers = renderers.Length;
                var bounds = renderers[0].bounds;
                foreach (var renderer in renderers.Skip(1))
                    bounds.Encapsulate(renderer.bounds);
                File.WriteAllBytes(Path.Combine(output, "preview.png"), Capture(scene, bounds));
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

        private static GameObject CreateRoot(string name, Scene scene)
        {
            var root = new GameObject(name) { layer = PreviewLayer };
            SceneManager.MoveGameObjectToScene(root, scene);
            return root;
        }

        private static byte[] Capture(Scene scene, Bounds bounds)
        {
            var camera = CreateRoot("Preview Camera", scene).AddComponent<Camera>();
            camera.enabled = false;
            camera.orthographic = true;
            camera.cullingMask = 1 << PreviewLayer;
            camera.clearFlags = CameraClearFlags.SolidColor;
            camera.backgroundColor = new Color(0.12f, 0.14f, 0.17f);
            var radius = Mathf.Max(bounds.extents.magnitude, 0.05f);
            camera.transform.position = bounds.center + new Vector3(1, 0.72f, -1.4f).normalized * radius * 4;
            camera.transform.LookAt(bounds.center);
            camera.nearClipPlane = Mathf.Max(radius * 0.02f, 0.001f);
            camera.farClipPlane = radius * 8;
            camera.orthographicSize = radius * 1.12f;

            var light = CreateRoot("Preview Light", scene).AddComponent<Light>();
            light.type = LightType.Directional;
            light.cullingMask = camera.cullingMask;
            light.intensity = 1.25f;
            light.transform.rotation = Quaternion.Euler(42, -35, 0);

            var target = RenderTexture.GetTemporary(ImageSize, ImageSize, 24, RenderTextureFormat.ARGB32);
            var previous = RenderTexture.active;
            var pixels = new Texture2D(ImageSize, ImageSize, TextureFormat.RGB24, false);
            try
            {
                camera.targetTexture = target;
                camera.Render();
                RenderTexture.active = target;
                pixels.ReadPixels(new Rect(0, 0, ImageSize, ImageSize), 0, 0);
                return ImageConversion.EncodeToPNG(pixels);
            }
            finally
            {
                camera.targetTexture = null;
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
