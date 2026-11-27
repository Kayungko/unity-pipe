#if UNITY_EDITOR
using System;
using System.IO;
using System.Linq;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Preview;
using UnityEditor;
using UnityEngine;

namespace Unity.Pipeline.Editor.Commands.Build
{
    static class ScenePreviewCommand
    {
        internal const string RuntimeDefine = "UPIPE_SCENE_PREVIEW";
        private const string BundleName = "preview-prefab";
        private const string DependencyName = "preview-dependencies";

        [CliCommand("pack_scene_preview", "Package one saved Prefab and its dependencies for a local preview Player.",
            MainThreadRequired = true, Effect = CommandEffect.Destructive, Tags = new[] { "build", "preview" })]
        public static PipelineCommandResult Pack(string asset, string output_dir, bool confirm = false)
        {
            if (!confirm)
                return PipelineCommandResult.Fail("Packaging requires confirm=true", new { stage = "validate" });
            if (!asset.StartsWith("Assets/", StringComparison.Ordinal) || !asset.EndsWith(".prefab", StringComparison.OrdinalIgnoreCase) ||
                AssetDatabase.LoadAssetAtPath<GameObject>(asset) == null)
                return PipelineCommandResult.Fail("Expected a saved Prefab under Assets/", new { stage = "validate", asset });
            if (EditorApplication.isPlayingOrWillChangePlaymode || EditorApplication.isCompiling ||
                BuildPipeline.isBuildingPlayer || EditorUserBuildSettings.activeBuildTarget != BuildTarget.StandaloneWindows64)
                return PipelineCommandResult.Fail("Use an idle Editor with StandaloneWindows64 active", new { stage = "validate" });

            string staging = null;
            var stage = "output";
            try
            {
                var destination = Path.GetFullPath(output_dir);
                var project = Directory.GetParent(Application.dataPath).FullName;
                if (destination.Equals(project, StringComparison.OrdinalIgnoreCase) ||
                    destination.StartsWith(project + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
                    throw new ArgumentException("Choose an output directory outside the Unity project");
                var parent = Path.GetDirectoryName(destination);
                if (!Directory.Exists(parent) || Directory.Exists(destination) || File.Exists(destination))
                    throw new IOException("Output directory must be new and its parent must already exist");

                staging = Path.Combine(parent, ".upipe-preview-" + Guid.NewGuid().ToString("N"));
                var bundleDirectory = Path.Combine(staging, PreviewPackage.CatalogName);
                Directory.CreateDirectory(bundleDirectory);
                stage = "build";
                var root = new AssetBundleBuild { assetBundleName = BundleName, assetNames = new[] { asset } };
                var dependencyPaths = AssetDatabase.GetDependencies(asset)
                    .Where(path => path != asset && AssetImporter.GetAtPath(path) != null &&
                        AssetDatabase.GetMainAssetTypeAtPath(path) != null &&
                        AssetDatabase.GetMainAssetTypeAtPath(path) != typeof(MonoScript) &&
                        AssetDatabase.GetMainAssetTypeAtPath(path) != typeof(SceneAsset) &&
                        !path.EndsWith(".dll", StringComparison.OrdinalIgnoreCase))
                    .ToArray();
                var builds = dependencyPaths.Length == 0 ? new[] { root } :
                    new[] { root, new AssetBundleBuild { assetBundleName = DependencyName, assetNames = dependencyPaths } };
                var catalog = BuildPipeline.BuildAssetBundles(bundleDirectory, builds,
                    BuildAssetBundleOptions.ChunkBasedCompression | BuildAssetBundleOptions.StrictMode,
                    BuildTarget.StandaloneWindows64);
                if (catalog == null || !catalog.GetAllAssetBundles().Contains(BundleName))
                    throw new InvalidOperationException("Unity failed to build the preview AssetBundle");

                var package = new PreviewPackage
                {
                    schema = PreviewPackage.CurrentSchema,
                    target = PreviewPackage.WindowsTarget,
                    unityVersion = Application.unityVersion,
                    projectGuid = PlayerSettings.productGUID.ToString(),
                    asset = asset,
                    bundle = BundleName,
                    dependencies = catalog.GetAllDependencies(BundleName)
                };
                File.WriteAllText(Path.Combine(bundleDirectory, PreviewPackage.FileName), JsonUtility.ToJson(package));
                stage = "output";
                Directory.Move(bundleDirectory, destination);
                return PipelineCommandResult.Succeed(new { directory = destination, package });
            }
            catch (Exception ex)
            {
                return PipelineCommandResult.Fail(ex.Message, new { stage, asset });
            }
            finally
            {
                if (staging != null && Directory.Exists(staging))
                    Directory.Delete(staging, true);
            }
        }

        internal static string ValidatePlayerBundle(string directory)
        {
            var path = Path.GetFullPath(directory);
            var package = PreviewPackage.Read(path);
            if (package.target != PreviewPackage.WindowsTarget || package.unityVersion != Application.unityVersion ||
                package.projectGuid != PlayerSettings.productGUID.ToString())
                throw new InvalidDataException("Preview bundle target, Unity version or project does not match");
            var manifest = Path.Combine(path, PreviewPackage.CatalogName + ".manifest");
            if (!File.Exists(manifest))
                throw new FileNotFoundException("The Unity AssetBundle build manifest is missing", manifest);
            return manifest;
        }
    }
}
#endif
