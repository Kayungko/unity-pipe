#if UNITY_EDITOR
using System;
using System.IO;
using System.Threading.Tasks;
using Unity.Pipeline.Editor.Authoring;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Models;
using UnityEditor;
using UnityEngine;

namespace Unity.Pipeline.Editor.Commands
{
    /// <summary>
    /// Command to get detailed Editor status information.
    /// Returns rich Editor state including compilation, play mode, and project details.
    /// Accessible via "unity request editor_status" and "/api/editor_status" endpoint.
    /// </summary>
    static class EditorStatusCommand
    {
        [CliCommand("editor_status", "Get detailed Unity Editor status and state information", Tags = new[] { "editor" })]
        public static StatusResponse GetEditorStatus()
        {
            return new StatusResponse
            {
                Status = GetOverallStatus(),
                Compiling = EditorApplication.isCompiling,
                DomainReloadInProgress = EditorApplication.isUpdating,
                PlayMode = GetPlayModeStatus(),
                LastHeartbeat = DateTime.UtcNow,
                ProjectPath = Path.GetDirectoryName(Application.dataPath),
                UnityVersion = Application.unityVersion
            };
        }

        [CliCommand("wait", "Wait for an Editor, asset, or scene object condition without blocking the main thread.", Tags = new[] { "editor" })]
        public static Task<object> Wait(
            [CliArg("condition", "editor_ready, playing, stopped, not_compiling, asset_exists, or object_active.", Required = true)] string condition,
            [CliArg("path", "Asset path for asset_exists.")] string path = null,
            [CliArg("target", "Scene object reference for object_active.")] ObjectRef target = null,
            [CliArg("timeout_ms", "Wait budget in milliseconds (1 to 600000).")] int timeoutMs = 10000)
        {
            if (timeoutMs < 1 || timeoutMs > 600000)
                throw new ArgumentOutOfRangeException(nameof(timeoutMs));

            Func<bool> probe;
            switch (condition)
            {
                case "editor_ready":
                    probe = () => !EditorApplication.isCompiling && !EditorApplication.isUpdating &&
                        EditorApplication.isPlayingOrWillChangePlaymode == EditorApplication.isPlaying;
                    break;
                case "playing":
                    probe = () => EditorApplication.isPlaying;
                    break;
                case "stopped":
                    probe = () => !EditorApplication.isPlaying && !EditorApplication.isPlayingOrWillChangePlaymode;
                    break;
                case "not_compiling":
                    probe = () => !EditorApplication.isCompiling;
                    break;
                case "asset_exists":
                    var resolved = ProjectPaths.Resolve(path, out var error);
                    if (resolved == null)
                        throw new ArgumentException(error, nameof(path));
                    probe = () => AssetDatabase.LoadMainAssetAtPath(resolved) != null;
                    break;
                case "object_active":
                    if (target == null || target.IsEmpty)
                        throw new ArgumentException("target is required for object_active.", nameof(target));
                    probe = () => ObjectResolver.TryResolve(target, out var obj, out _) &&
                        obj is GameObject go && go.scene.IsValid() && go.activeInHierarchy;
                    break;
                default:
                    throw new ArgumentException($"Unknown wait condition: {condition}", nameof(condition));
            }

            var completion = new TaskCompletionSource<object>(TaskCreationOptions.RunContinuationsAsynchronously);
            var clock = System.Diagnostics.Stopwatch.StartNew();
            var job = BasePipelineServer.CurrentServer?.JobRegistry.CurrentRunning;
            EditorApplication.update += Poll;
            Poll();
            return completion.Task;

            void Poll()
            {
                try
                {
                    if (job != null && job.CancellationRequested)
                        completion.TrySetCanceled();
                    // The transitioning domain cannot keep this HTTP request alive.
                    else if (EditorApplication.isPlayingOrWillChangePlaymode != EditorApplication.isPlaying)
                        completion.TrySetResult(new { status = "transitioning", condition });
                    else if (clock.ElapsedMilliseconds >= timeoutMs)
                        completion.TrySetResult(new { status = "timed_out", condition, elapsedMs = clock.ElapsedMilliseconds });
                    else if (probe())
                        completion.TrySetResult(new { status = "matched", condition, elapsedMs = clock.ElapsedMilliseconds });
                }
                catch (Exception ex)
                {
                    completion.TrySetException(ex);
                }
                finally
                {
                    if (completion.Task.IsCompleted)
                        EditorApplication.update -= Poll;
                }
            }
        }

        /// <summary>
        /// Get overall Editor status based on current state.
        /// </summary>
        private static string GetOverallStatus()
        {
            if (EditorApplication.isCompiling)
                return "compiling";

            if (EditorApplication.isUpdating)
                return "reloading";

            if (EditorApplication.isPlaying || EditorApplication.isPaused)
                return "playing";

            return "ready";
        }

        /// <summary>
        /// Get current play mode status.
        /// </summary>
        private static string GetPlayModeStatus()
        {
            if (EditorApplication.isPaused)
                return "paused";

            if (EditorApplication.isPlaying)
                return "playing";

            return "stopped";
        }
    }
}
#endif
