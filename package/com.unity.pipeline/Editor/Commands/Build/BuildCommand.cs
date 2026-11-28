#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Editor.Authoring;
using Unity.Pipeline.Preview;
using UnityEditor;
using UnityEditor.Build;
using UnityEditor.Build.Reporting;
using UnityEngine;
#if UNITY_6000_0_OR_NEWER
using UnityEditor.Build.Profile;
#endif
#if UNITY_6000_5_OR_NEWER
using Unity.Scripting.LifecycleManagement;
#endif

namespace Unity.Pipeline.Editor.Commands.Build
{
    /// <summary>
    /// Triggers a Player build and reports the full <see cref="BuildReport"/> (CLI-204). Fills out the
    /// original <c>build</c>/<c>build_status</c> stub (CLI-127) with the complete param contract and the
    /// structured report shape so build failures can be diagnosed without the Editor UI.
    ///
    /// Why this is async (trigger-then-poll, mirroring <see cref="RecompileCommand"/> /
    /// <see cref="PackageManager.PackageManagerCommand"/>): <see cref="BuildPipeline.BuildPlayer(BuildPlayerOptions)"/>
    /// runs synchronously and freezes the main thread for the whole build (seconds to minutes). If
    /// <c>build</c> ran it inline it would block the server's (serial) request loop and the call could
    /// never return. Instead <c>build</c> validates + queues and returns <c>queued</c> immediately; an
    /// <see cref="EditorApplication.update"/> hook runs the build on the next tick. Poll <c>build_status</c>
    /// until <c>status == "completed"</c>.
    ///
    /// <c>build_status</c> is deliberately <c>MainThreadRequired = false</c>: while the build holds the
    /// main thread, any main-thread command (e.g. editor_status) would hang, but build_status only reads
    /// a Temp status file off-thread, so polling keeps working throughout the build. The status file also
    /// survives the domain reload a build can incur, so the last report is retained until the next build.
    /// </summary>
    [InitializeOnLoad]
#if UNITY_6000_5_OR_NEWER
    [NoAutoStaticsCleanup]
#endif
    static class BuildCommand
    {
        const string StatusFile = "Temp/pipeline_build_status.json";

        // BuildOptions that callers may pass by name (the supported set from the ticket). Parsed
        // case-insensitively; anything outside this set is a validation error rather than silently
        // tolerated, so a typo surfaces instead of changing the build shape.
        static readonly Dictionary<string, BuildOptions> s_SupportedOptions =
            new Dictionary<string, BuildOptions>(StringComparer.OrdinalIgnoreCase)
            {
                ["Development"] = BuildOptions.Development,
                ["AllowDebugging"] = BuildOptions.AllowDebugging,
                ["ConnectWithProfiler"] = BuildOptions.ConnectWithProfiler,
                // EnableHeadlessMode is obsolete in Unity 6 (superseded by StandaloneBuildSubtarget.Server)
                // but still functions and is part of this command's documented option set (CLI-204), so we
                // keep accepting it and suppress only the deprecation warning here.
#pragma warning disable 618
                ["EnableHeadlessMode"] = BuildOptions.EnableHeadlessMode,
#pragma warning restore 618
                ["SymlinkSources"] = BuildOptions.SymlinkSources,
                ["BuildAdditionalStreamedScenes"] = BuildOptions.BuildAdditionalStreamedScenes,
                ["CleanBuildCache"] = BuildOptions.CleanBuildCache,
                ["DetailedBuildReport"] = BuildOptions.DetailedBuildReport,
                // Release-oriented compression options (available in 2022.3;
                // standard release builds use CompressWithLz4HC).
                ["CompressWithLz4"] = BuildOptions.CompressWithLz4,
                ["CompressWithLz4HC"] = BuildOptions.CompressWithLz4HC,
                // Strict-mode companion flags commonly paired with LZ4HC releases.
                // (CompressWithLz4HCNoChunks is a Unity 6+ API — not in 2022.3, do not add.)
                ["StrictMode"] = BuildOptions.StrictMode,
            };

        static readonly object s_Lock = new object();
        static bool s_Pending;
        static bool s_Building;

        // Queued request, captured under s_Lock by build() and consumed by the update pump.
        static BuildTarget s_PendingTarget;
        static string s_PendingOutput;
        static BuildOptions s_PendingOptions;
        static string[] s_PendingScenes;
        static string s_PendingProfileGuid;
        static string s_PendingBuildId;
        static string s_PendingPreviewBundle;

        internal static bool IsBuildingPreviewPlayer { get; private set; }

        static BuildCommand()
        {
            RestoreInterruptedStatus();
            EditorApplication.update += OnUpdate;
        }

        [CliCommand("build",
            "Trigger an async Player build and report the full BuildReport. Returns immediately (queued); " +
            "poll build_status until status is 'completed'. DetailedBuildReport is included by default unless " +
            "'options' is supplied. Use dry_run to validate without building.",
            MainThreadRequired = false, Effect = CommandEffect.Destructive,
            Tags = new[] { "build" })]
        public static object Build(
            [CliArg("target", "BuildTarget name (e.g. StandaloneWindows64). Defaults to the active target. Must be installed.")] string target = "",
            [CliArg("outputPath", "Output path (absolute, or relative to the project root). Defaults to the last/auto path.")] string outputPath = "",
            [CliArg("profileName", "Build Profile to use by name (requires Unity 6). Its target and scenes are used.")] string profileName = "",
            [CliArg("options", "BuildOptions names. Omit to get just DetailedBuildReport; supplying any disables that default.")] string[] options = null,
            [CliArg("scenes", "Scene asset paths to build (e.g. Assets/Scenes/Main.unity). Defaults to EditorBuildSettings.")] string[] scenes = null,
            [CliArg("confirm", "Acknowledge and run the build; without it the call is refused. Use dry_run to validate only.")] bool confirm = false,
            [CliArg("dry_run", "Validate target/outputPath/scenes without building.")] bool dryRun = false,
            [CliArg("scenePreviewBundle", "Local preview bundle directory for a dedicated Windows Development Player.")] string scenePreviewBundle = "")
        {
            // Validation + queueing both touch Unity APIs, so run them on the main thread. When invoked
            // off-thread (the normal HTTP path) this marshals through the server dispatcher; in-process
            // (tests, no server) it runs inline.
            return RunOnMain<object>(() => ValidateAndQueue(target, outputPath, profileName, options, scenes, confirm, dryRun, scenePreviewBundle));
        }

        static object ValidateAndQueue(string targetArg, string outputArg, string profileName,
            string[] optionArgs, string[] sceneArgs, bool confirm, bool dryRun, string scenePreviewBundle)
        {
            lock (s_Lock)
            {
                if (!dryRun && (s_Building || s_Pending))
                    return new { status = "busy", success = false, message = "A build is already queued or in progress. Poll build_status." };
            }

            var errors = new List<object>();
            if (!dryRun && (EditorApplication.isPlayingOrWillChangePlaymode || EditorApplication.isCompiling))
                return new { status = "busy", success = false, message = "Exit Play Mode and finish compiling before building." };

            // ---- target ----
            var target = EditorUserBuildSettings.activeBuildTarget;
            if (!string.IsNullOrWhiteSpace(targetArg))
            {
                if (!TryParseTarget(targetArg, out var requestedTarget))
                    errors.Add(Invalid("target", $"Unknown BuildTarget '{targetArg}'. Use a name from list_build_targets."));
                else
                    target = requestedTarget;
            }

            var profile = ValidateProfile(profileName, targetArg, sceneArgs, ref target, errors);
            if (!BuildPipeline.IsBuildTargetSupported(BuildPipeline.GetBuildTargetGroup(target), target))
                errors.Add(Invalid("target", $"Build target '{target}' is not installed (isInstalled=false in list_build_targets)."));

            // ---- options ----
            var options = BuildOptions.None;
            var optionNames = new List<string>();
            if (optionArgs != null && optionArgs.Length > 0)
            {
                foreach (var name in optionArgs)
                {
                    if (string.IsNullOrWhiteSpace(name))
                        continue;
                    if (s_SupportedOptions.TryGetValue(name.Trim(), out var opt))
                    {
                        options |= opt;
                        optionNames.Add(name.Trim());
                    }
                    else
                        errors.Add(Invalid("options", $"Unsupported BuildOptions value '{name}'."));
                }
            }
            else
            {
                // Strongly recommended: without it BuildReport.packedAssets is empty.
                options = BuildOptions.DetailedBuildReport;
                optionNames.Add(nameof(BuildOptions.DetailedBuildReport));
            }

            // ---- scenes ----
            var scenes = (sceneArgs != null && sceneArgs.Length > 0)
                ? sceneArgs
                : EditorBuildSettings.scenes.Where(s => s.enabled).Select(s => s.path).ToArray();

#if UNITY_6000_0_OR_NEWER
            if (profile != null)
                scenes = ProfileScenes(profile);
#endif
            foreach (var scene in scenes)
                if (string.IsNullOrWhiteSpace(scene) || AssetDatabase.GetMainAssetTypeAtPath(scene) != typeof(SceneAsset))
                    errors.Add(Invalid("scenes", $"No scene asset at '{scene}'."));

            // ---- output path ----
            // Non-mutating writability check: resolve the path and confirm an existing ancestor
            // directory exists (so the build's output folder can be created). BuildPipeline.BuildPlayer
            // creates the leaf directory itself, so we never create anything here — a dry run stays pure.
            string outputPath = null;
            try
            {
                outputPath = ResolveOutput(outputArg, target);
                if (!HasExistingAncestor(outputPath))
                    errors.Add(Invalid("outputPath", $"No existing parent directory for '{outputPath}'."));
            }
            catch (Exception ex)
            {
                errors.Add(Invalid("outputPath", $"Output path is invalid: {ex.Message}"));
            }

            string previewManifest = null;
            if (!string.IsNullOrWhiteSpace(scenePreviewBundle))
            {
                if (target != BuildTarget.StandaloneWindows64 || (options & BuildOptions.Development) == 0 ||
                    !string.IsNullOrEmpty(profileName) || sceneArgs?.Length != 1)
                    errors.Add(Invalid("scenePreviewBundle", "Specify StandaloneWindows64, Development and one explicit preview host scene."));
                if (outputPath != null && (File.Exists(outputPath) || File.Exists(outputPath + ".scene-preview.json")))
                    errors.Add(Invalid("outputPath", "The dedicated preview Player requires a new output path."));
                try
                {
                    previewManifest = ScenePreviewCommand.ValidatePlayerBundle(scenePreviewBundle);
                }
                catch (Exception ex)
                {
                    errors.Add(Invalid("scenePreviewBundle", ex.Message));
                }
            }

            if (dryRun)
            {
                return new
                {
                    status = "dry_run",
                    valid = errors.Count == 0,
                    validationErrors = errors,
                    target = target.ToString(),
                    scenes,
                    outputPath,
                    options = optionNames.Distinct(StringComparer.OrdinalIgnoreCase).ToArray(),
                    profileName = profile?.Name
                };
            }

            if (errors.Count > 0)
            {
                // Fail immediately — do not queue (acceptance criteria 5).
                return new { status = "error", success = false, validationErrors = errors, message = "Build configuration is invalid; nothing was queued." };
            }

            if (!confirm)
                return new
                {
                    status = "error",
                    success = false,
                    message = "Refused: this triggers a player build. Pass confirm=true to build, or dry_run=true to validate without building."
                };

            var buildId = NewBuildId();

            lock (s_Lock)
            {
                s_PendingTarget = target;
                s_PendingOutput = outputPath;
                s_PendingOptions = options;
                s_PendingScenes = scenes;
                s_PendingProfileGuid = profile?.Guid;
                s_PendingBuildId = buildId;
                s_PendingPreviewBundle = previewManifest;
                s_Pending = true;
            }

            WriteStatusJson(new { status = "queued", buildId, message = "Build queued. Poll build_status until status is 'completed'." });

            return new { status = "queued", buildId };
        }

        [CliCommand("build_status",
            "Status of the current/most recent build: idle | queued | building | completed | interrupted, with the full " +
            "BuildReport (files, packedAssets, buildSteps, errors, warnings) once completed. Retained until the next build.",
            MainThreadRequired = false,
            Tags = new[] { "build" })]
        public static string BuildStatus()
        {
            string text;
            lock (s_Lock)
            {
                if (!File.Exists(StatusFile))
                    return "{\"status\":\"idle\"}";
                text = File.ReadAllText(StatusFile);
            }

            // While building, recompute elapsedMs at read time so polling shows live progress.
            try
            {
                var doc = JObject.Parse(text);
                if ((string)doc["status"] == "building")
                {
                    long elapsed = 0;
                    var started = doc.Value<DateTime?>("buildStartedAt");
                    if (started.HasValue)
                    {
                        elapsed = (long)Math.Max(0, (DateTime.UtcNow - started.Value.ToUniversalTime()).TotalMilliseconds);
                    }

                    return JsonConvert.SerializeObject(new
                    {
                        status = "building",
                        buildId = (string)doc["buildId"],
                        elapsedMs = elapsed
                    });
                }
            }
            catch
            {
                // Malformed file — fall through and return it verbatim.
            }

            return text;
        }

        /// <summary>
        /// Main-thread pump: picks up a queued request and runs the (blocking) build. Guarded so a build
        /// runs at most once at a time.
        /// </summary>
        static void OnUpdate()
        {
            BuildTarget target;
            string output;
            BuildOptions options;
            string[] scenes;
            string profile;
            string buildId;
            string previewManifest;

            lock (s_Lock)
            {
                if (!s_Pending || s_Building)
                    return;
                s_Pending = false;
                s_Building = true;
                target = s_PendingTarget;
                output = s_PendingOutput;
                options = s_PendingOptions;
                scenes = s_PendingScenes;
                profile = s_PendingProfileGuid;
                buildId = s_PendingBuildId;
                previewManifest = s_PendingPreviewBundle;
            }

            try
            {
                RunBuild(buildId, target, output, options, scenes, profile, previewManifest);
            }
            catch (Exception ex)
            {
                WriteStatusJson(new
                {
                    status = "completed",
                    buildId,
                    result = "Failed",
                    totalSizeBytes = 0,
                    errors = new[] { new { message = $"Build failed to start: {ex.Message}" } },
                    warnings = Array.Empty<object>()
                });
            }
            finally
            {
                lock (s_Lock) { s_Building = false; }
            }
        }

        static void RunBuild(string buildId, BuildTarget target, string outputPath, BuildOptions options,
            string[] scenes, string profileGuid, string previewManifest)
        {
            var startedAt = DateTime.UtcNow;
            WriteStatusJson(new
            {
                status = "building",
                buildId,
                buildStartedAt = startedAt.ToString("o"),
                platform = target.ToString(),
                outputPath
            });

#if UNITY_6000_0_OR_NEWER
            if (profileGuid != null)
            {
                var profile = AssetDatabase.LoadAssetAtPath<BuildProfile>(AssetDatabase.GUIDToAssetPath(profileGuid));
                var info = BuildProfiles.List().FirstOrDefault(item => item.Guid == profileGuid);
                if (profile == null || info == null || !TryParseTarget(info.Platform, out var profileTarget)
                    || profileTarget != target || !BuildScenes(profile).SequenceEqual(scenes))
                    throw new InvalidOperationException("The selected Build Profile or its scene list changed after validation.");
                var profileReport = BuildPipeline.BuildPlayer(new BuildPlayerWithProfileOptions
                {
                    buildProfile = profile, locationPathName = outputPath, options = options
                });
                WriteStatusJson(Project(buildId, profileReport));
                return;
            }
#endif
            var buildOptions = new BuildPlayerOptions
            {
                scenes = scenes,
                locationPathName = outputPath,
                target = target,
                targetGroup = BuildPipeline.GetBuildTargetGroup(target),
                options = options
            };

            if (previewManifest != null)
            {
                buildOptions.assetBundleManifestPath = previewManifest;
                buildOptions.extraScriptingDefines = new[] { ScenePreviewCommand.RuntimeDefine };
            }
            BuildReport report;
            IsBuildingPreviewPlayer = previewManifest != null;
            try
            {
                report = BuildPipeline.BuildPlayer(buildOptions);
            }
            finally
            {
                IsBuildingPreviewPlayer = false;
            }
            if (previewManifest != null && report.summary.result == BuildResult.Succeeded)
            {
                ScenePreviewCommand.WritePlayerMetadata(previewManifest, outputPath);
            }
            WriteStatusJson(Project(buildId, report));
        }

        // ---- BuildReport projection ------------------------------------------------------------

        static BuildReportResult Project(string buildId, BuildReport report)
        {
            var summary = report.summary;
            var success = summary.result == BuildResult.Succeeded;
            var started = summary.buildStartedAt;
            var ended = started + summary.totalTime;

            var result = new BuildReportResult
            {
                Status = "completed",
                BuildId = buildId,
                Result = summary.result.ToString(),
                Platform = summary.platform.ToString(),
                OutputPath = summary.outputPath,
                TotalSizeBytes = (long)summary.totalSize,
                BuildTimeMs = (long)summary.totalTime.TotalMilliseconds,
                BuildStartedAt = started.ToUniversalTime().ToString("o"),
                BuildEndedAt = ended.ToUniversalTime().ToString("o"),
                TotalWarnings = summary.totalWarnings,
                TotalErrors = summary.totalErrors,
                BuildSteps = ProjectSteps(report, out var errors, out var warnings),
                Errors = errors,
                Warnings = warnings
            };

            // Files / packed assets are only meaningful on success (and packedAssets needs DetailedBuildReport).
            if (success)
            {
                result.Files = ProjectFiles(report);
                result.PackedAssets = ProjectPackedAssets(report);
            }

            return result;
        }

        static List<BuildFileEntry> ProjectFiles(BuildReport report)
        {
            var files = new List<BuildFileEntry>();
            try
            {
                foreach (var f in report.GetFiles())
                    files.Add(new BuildFileEntry { Path = f.path, Role = f.role, SizeBytes = (long)f.size });
            }
            catch (Exception ex)
            {
                Debug.LogWarning($"[pipeline] build file projection failed: {ex.Message}");
            }
            return files;
        }

        static List<PackedAssetEntry> ProjectPackedAssets(BuildReport report)
        {
            var packed = new List<PackedAssetEntry>();
            if (report.packedAssets == null)
                return packed;

            foreach (var bundle in report.packedAssets)
            {
                var entry = new PackedAssetEntry
                {
                    BundlePath = bundle.shortPath,
                    OverheadBytes = (long)bundle.overhead,
                    Contents = new List<PackedAssetContent>()
                };

                if (bundle.contents != null)
                {
                    foreach (var c in bundle.contents)
                    {
                        entry.Contents.Add(new PackedAssetContent
                        {
                            AssetPath = c.sourceAssetPath,
                            Type = c.type != null ? c.type.Name : null,
                            Guid = c.sourceAssetGUID.ToString(),
                            SizeBytes = (long)c.packedSize
                        });
                    }
                }

                packed.Add(entry);
            }
            return packed;
        }

        static List<BuildStepEntry> ProjectSteps(BuildReport report,
            out List<BuildIssue> errors, out List<BuildIssue> warnings)
        {
            errors = new List<BuildIssue>();
            warnings = new List<BuildIssue>();

            var steps = new List<BuildStepEntry>();
            if (report.steps == null)
                return steps;

            foreach (var step in report.steps)
            {
                var entry = new BuildStepEntry
                {
                    Name = step.name,
                    DurationMs = (long)step.duration.TotalMilliseconds,
                    Depth = (int)step.depth,
                    Messages = new List<BuildStepMessageEntry>()
                };

                if (step.messages != null)
                {
                    foreach (var m in step.messages)
                    {
                        entry.Messages.Add(new BuildStepMessageEntry { Type = MapLogType(m.type), Content = m.content });

                        if (m.type == LogType.Error || m.type == LogType.Assert || m.type == LogType.Exception)
                            errors.Add(BuildIssue.From(m.content));
                        else if (m.type == LogType.Warning)
                            warnings.Add(BuildIssue.From(m.content));
                    }
                }

                steps.Add(entry);
            }
            return steps;
        }

        static string MapLogType(LogType type)
        {
            switch (type)
            {
                case LogType.Warning: return "Warning";
                case LogType.Error:
                case LogType.Assert:
                case LogType.Exception: return "Error";
                default: return "Log";
            }
        }

        // ---- Helpers ---------------------------------------------------------------------------

        /// <summary>Case-insensitive parse of a <see cref="BuildTarget"/> enum name.</summary>
        static bool TryParseTarget(string name, out BuildTarget target)
        {
            try
            {
                target = (BuildTarget)Enum.Parse(typeof(BuildTarget), name.Trim(), ignoreCase: true);
                return Enum.IsDefined(typeof(BuildTarget), target);
            }
            catch
            {
                target = EditorUserBuildSettings.activeBuildTarget;
                return false;
            }
        }

        static object Invalid(string field, string message) => new { field, message };

        static string NewBuildId() => "build_" + Guid.NewGuid().ToString("N").Substring(0, 12);

        static string ResolveOutput(string output, BuildTarget target)
        {
            var projectRoot = ProjectPaths.ProjectRoot;

            if (!string.IsNullOrWhiteSpace(output))
                return Path.IsPathRooted(output) ? output : Path.GetFullPath(Path.Combine(projectRoot, output));

            var product = string.IsNullOrEmpty(PlayerSettings.productName) ? "Player" : PlayerSettings.productName;
            return Path.Combine(projectRoot, "Builds", target.ToString(), product + ExtensionFor(target));
        }

        /// <summary>True if some ancestor directory of <paramref name="path"/> already exists — a cheap,
        /// non-mutating check that the build output folder is creatable.</summary>
        static bool HasExistingAncestor(string path)
        {
            var dir = Path.GetDirectoryName(path);
            while (!string.IsNullOrEmpty(dir))
            {
                if (Directory.Exists(dir))
                    return true;
                dir = Path.GetDirectoryName(dir);
            }
            return false;
        }

        static string ExtensionFor(BuildTarget target)
        {
            switch (target)
            {
                case BuildTarget.StandaloneWindows:
                case BuildTarget.StandaloneWindows64:
                    return ".exe";
                case BuildTarget.StandaloneOSX:
                    return ".app";
                case BuildTarget.Android:
                    return ".apk";
                default:
                    return string.Empty; // Linux standalone, dedicated server, etc.
            }
        }

        static BuildProfileInfo ValidateProfile(string name, string targetArg, string[] sceneArgs,
            ref BuildTarget target, List<object> errors)
        {
            if (string.IsNullOrWhiteSpace(name))
                return null;
            if (!BuildProfiles.IsSupported)
            {
                errors.Add(Invalid("profileName", "Build Profiles require Unity 6 (6000.0) or later."));
                return null;
            }
            var matches = BuildProfiles.List().Where(profile => string.Equals(profile.Name, name.Trim(),
                StringComparison.OrdinalIgnoreCase)).ToArray();
            if (matches.Length != 1)
            {
                errors.Add(Invalid("profileName", matches.Length == 0
                    ? $"Build Profile '{name}' was not found." : $"Build Profile '{name}' is ambiguous."));
                return null;
            }
            var profile = matches[0];
            if (!TryParseTarget(profile.Platform, out var platform))
                errors.Add(Invalid("profileName", $"The target of Build Profile '{name}' could not be determined."));
            else if (!string.IsNullOrWhiteSpace(targetArg) && platform != target)
                errors.Add(Invalid("target", $"Build Profile '{name}' targets '{platform}', not '{target}'."));
            else
                target = platform;
            if (sceneArgs != null && sceneArgs.Length > 0)
                errors.Add(Invalid("scenes", "A Build Profile supplies its own scene list. Omit scenes."));
            return profile;
        }

#if UNITY_6000_0_OR_NEWER
        static string[] ProfileScenes(BuildProfileInfo info)
        {
            var asset = AssetDatabase.LoadAssetAtPath<BuildProfile>(AssetDatabase.GUIDToAssetPath(info.Guid));
            if (asset == null)
                throw new InvalidOperationException("The selected Build Profile no longer exists.");
            return BuildScenes(asset);
        }

        static string[] BuildScenes(BuildProfile asset) =>
            asset.GetScenesForBuild().Where(scene => scene.enabled).Select(scene => scene.path).ToArray();
#endif

        /// <summary>
        /// Run <paramref name="fn"/> on the Unity main thread. Off-thread (the HTTP path) it marshals
        /// through the live server's dispatcher; on the main thread (a direct in-process/test call, or no
        /// server running) it runs inline. Mirrors <see cref="PackageManager.PackageManagerCommand"/>.
        /// </summary>
        static T RunOnMain<T>(Func<T> fn)
        {
            var dispatcher = PipelineServerStartup.Server?.Dispatcher;
            if (dispatcher != null && dispatcher.IsInitialized && !dispatcher.IsMainThread())
                return dispatcher.Invoke(fn);
            return fn();
        }

        static void RestoreInterruptedStatus()
        {
            try
            {
                var previous = JObject.Parse(BuildStatus());
                var status = (string)previous["status"];
                if (status == "queued" || status == "building")
                    WriteStatusJson(new { status = "interrupted", buildId = (string)previous["buildId"],
                        message = "The Editor reloaded or restarted before the build returned a report." });
            }
            catch (Exception ex)
            {
                Debug.LogWarning($"[Build] Could not read previous build status: {ex.Message}");
            }
        }

        static void WriteStatusJson(object status)
        {
            try
            {
                var text = JsonConvert.SerializeObject(status);
                lock (s_Lock)
                    File.WriteAllText(StatusFile, text);
            }
            catch (Exception ex)
            {
                Debug.LogError($"[Build] Failed to write status file: {ex.Message}");
            }
        }
    }
}
#endif
