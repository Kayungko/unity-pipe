#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Threading.Tasks;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;
using Unity.Pipeline.Editor.Authoring;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Models;
using Unity.Pipeline.Editor.Commands.Capture;
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
        [CliCommand("editor_status", "Get detailed Unity Editor status and state information", Effect = CommandEffect.ReadOnly, Tags = new[] { "editor" })]
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

        [CliCommand("wait", "Wait for an Editor, asset, or scene object condition without blocking the main thread.", Effect = CommandEffect.ReadOnly, Tags = new[] { "editor" })]
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

    [InitializeOnLoad]
    static class AsyncWaitCommands
    {
        const string SessionKey = "Unity.Pipeline.AsyncWaitResults";
        const int MaxPending = 32;
        const int MaxResults = 128;

        sealed class Pending
        {
            public UnityEngine.Object Target;
            public FieldInfo Field;
            public string Comparison;
            public JValue Expected;
            public string CapturePath;
            public string CaptureSource;
            public int Width;
            public int Height;
            public int TimeoutMs;
            public System.Diagnostics.Stopwatch Clock = System.Diagnostics.Stopwatch.StartNew();
            public JObject Result;
        }

        static readonly Dictionary<string, Pending> s_Pending = new Dictionary<string, Pending>();
        static readonly List<JObject> s_Results = RestoreResults();

        static AsyncWaitCommands()
        {
            AssemblyReloadEvents.beforeAssemblyReload += BeforeReload;
        }

        [CliCommand("wait_start", "Start a non-blocking wait on a public scalar field and optionally save the Game View when it matches. The capture can overwrite its output file.", Effect = CommandEffect.Destructive, Tags = new[] { "editor", "wait" })]
        public static JObject Start(
            [CliArg("target", "Object reference to an existing scene object or component.", Required = true)] ObjectRef target,
            [CliArg("field", "Public instance field name. Properties and method calls are not supported.", Required = true)] string field,
            [CliArg("value", "Scalar value to compare with.", Required = true, ArbitraryKeys = true)] JToken value,
            [CliArg("component", "Component short or full name when targeting a GameObject.")] string component = null,
            [CliArg("op", "eq, ne, gt, ge, lt or le.")] string comparison = "eq",
            [CliArg("timeout_ms", "Wait budget in milliseconds (1 to 600000).")] int timeoutMs = 10000,
            [CliArg("capture_path", "Optional PNG path under the authoring root.")] string capturePath = null,
            [CliArg("capture_source", "camera or screen. Screen requires a fresh Game View frame.")] string captureSource = "camera",
            [CliArg("capture_width", "Capture width (1 to 4096).")] int width = 1280,
            [CliArg("capture_height", "Capture height (1 to 4096).")] int height = 720)
        {
            if (s_Pending.Count >= MaxPending)
                throw new InvalidOperationException("Too many active waits.");
            if (timeoutMs < 1 || timeoutMs > 600000)
                throw new ArgumentOutOfRangeException(nameof(timeoutMs));
            if (!ObjectResolver.TryResolve(target, out var obj, out var error))
                throw new ArgumentException(error, nameof(target));

            if (!string.IsNullOrEmpty(component))
            {
                if (!(obj is GameObject gameObject))
                    throw new ArgumentException("component requires a GameObject target.", nameof(component));
                var matches = gameObject.GetComponents<Component>()
                    .Where(c => c != null && (c.GetType().Name == component || c.GetType().FullName == component)).ToArray();
                if (matches.Length != 1)
                    throw new ArgumentException("component must resolve to exactly one component.", nameof(component));
                obj = matches[0];
            }

            var member = obj.GetType().GetField(field, BindingFlags.Public | BindingFlags.Instance);
            if (member == null || !IsScalar(member.FieldType))
                throw new ArgumentException("field must name a public scalar instance field.", nameof(field));
            var expected = value as JValue ?? (value == null ? JValue.CreateNull() : null);
            if (expected == null)
                throw new ArgumentException("value must be a scalar.", nameof(value));
            ValidateComparison(member.FieldType, expected, comparison);

            if (capturePath != null)
            {
                capturePath = ProjectPaths.Resolve(capturePath, out error);
                if (capturePath == null || !string.Equals(Path.GetExtension(capturePath), ".png", StringComparison.OrdinalIgnoreCase))
                    throw new ArgumentException(error ?? "capture_path must end in .png.", nameof(capturePath));
                if (captureSource != "camera" && captureSource != "screen")
                    throw new ArgumentException("capture_source must be camera or screen.", nameof(captureSource));
                if (width < 1 || width > 4096 || height < 1 || height > 4096)
                    throw new ArgumentException("Capture dimensions must be between 1 and 4096.");
            }

            var id = Guid.NewGuid().ToString("N");
            var pending = new Pending
            {
                Target = obj, Field = member, Comparison = comparison, Expected = expected,
                CapturePath = capturePath, CaptureSource = captureSource, Width = width, Height = height,
                TimeoutMs = timeoutMs,
                Result = new JObject { ["waitId"] = id, ["status"] = "waiting", ["field"] = field }
            };
            if (s_Pending.Count == 0)
                EditorApplication.update += Tick;
            s_Pending.Add(id, pending);
            return (JObject)pending.Result.DeepClone();
        }

        [CliCommand("wait_status", "Read an async wait result by id.", Effect = CommandEffect.ReadOnly, Tags = new[] { "editor", "wait" })]
        public static JObject Status([CliArg("wait_id", "Wait id from wait_start.", Required = true)] string id)
        {
            if (s_Pending.TryGetValue(id, out var pending))
                return (JObject)pending.Result.DeepClone();
            var result = s_Results.FirstOrDefault(entry => (string)entry["waitId"] == id);
            if (result == null)
                throw new ArgumentException("Unknown wait id.", nameof(id));
            return (JObject)result.DeepClone();
        }

        [CliCommand("wait_cancel", "Cancel an active async wait and return its outcome.", Effect = CommandEffect.Stateful, Tags = new[] { "editor", "wait" })]
        public static JObject Cancel([CliArg("wait_id", "Wait id from wait_start.", Required = true)] string id)
        {
            if (s_Pending.TryGetValue(id, out var pending))
                Finish(id, pending, "cancelled");
            return Status(id);
        }

        static void Tick()
        {
            foreach (var entry in s_Pending.ToArray())
            {
                var id = entry.Key;
                var pending = entry.Value;
                try
                {
                    if (pending.Clock.ElapsedMilliseconds >= pending.TimeoutMs)
                    {
                        Finish(id, pending, "timed_out");
                        continue;
                    }

                    if (pending.Target == null)
                        throw new InvalidOperationException("Wait target was destroyed.");

                    var value = new JValue(pending.Field.GetValue(pending.Target));
                    pending.Result["value"] = value;
                    if (!Matches(value, pending.Expected, pending.Comparison))
                        continue;

                    pending.Result["matchedFrame"] = Time.frameCount;
                    if (pending.CapturePath != null)
                    {
                        // Read and capture on this same Editor update, without queueing another command.
                        var capture = CaptureCommands.CaptureGameView(pending.Width, pending.Height,
                            savePath: pending.CapturePath, source: pending.CaptureSource,
                            requireFreshFrame: pending.CaptureSource == "screen");
                        pending.Result["capture"] = JObject.FromObject(capture);
                        pending.Result["captureFrame"] = Time.frameCount;
                    }
                    Finish(id, pending, "matched");
                }
                catch (Exception ex)
                {
                    pending.Result["message"] = ex.Message;
                    Finish(id, pending, "failed");
                }
            }
        }

        static void Finish(string id, Pending pending, string status)
        {
            pending.Result["status"] = status;
            pending.Result["elapsedMs"] = pending.Clock.ElapsedMilliseconds;
            s_Pending.Remove(id);
            if (s_Pending.Count == 0)
                EditorApplication.update -= Tick;
            s_Results.Add(pending.Result);
            if (s_Results.Count > MaxResults)
                s_Results.RemoveAt(0);
        }

        static void BeforeReload()
        {
            foreach (var entry in s_Pending.ToArray())
            {
                entry.Value.Result["message"] = "Editor assemblies were reloaded while waiting.";
                Finish(entry.Key, entry.Value, "interrupted");
            }
            SessionState.SetString(SessionKey, new JArray(s_Results).ToString(Formatting.None));
        }

        static List<JObject> RestoreResults()
        {
            try { return JArray.Parse(SessionState.GetString(SessionKey, "[]")).OfType<JObject>().ToList(); }
            catch (JsonException) { return new List<JObject>(); }
        }

        static bool IsNumber(JTokenType type) => type == JTokenType.Integer || type == JTokenType.Float;

        static bool IsScalar(Type type) =>
            type == typeof(string) || type == typeof(bool) ||
            (type.IsPrimitive && type != typeof(char) && type != typeof(IntPtr) && type != typeof(UIntPtr)) ||
            type == typeof(decimal);

        static void ValidateComparison(Type type, JValue expected, string op)
        {
            switch (op)
            {
                case "eq":
                case "ne":
                case "gt":
                case "ge":
                case "lt":
                case "le":
                    break;
                default:
                    throw new ArgumentException("Unknown comparison. Use eq, ne, gt, ge, lt or le.", nameof(op));
            }

            if (type == typeof(bool) || type == typeof(string))
            {
                if (op != "eq" && op != "ne")
                    throw new ArgumentException("Boolean and string fields support eq and ne.");
                if (type == typeof(bool) && expected.Type != JTokenType.Boolean ||
                    type == typeof(string) && expected.Type != JTokenType.String && expected.Type != JTokenType.Null)
                    throw new ArgumentException("value type does not match the field.");
            }
            else if (!IsNumber(expected.Type))
            {
                throw new ArgumentException("Numeric fields require a numeric value.");
            }
        }

        static bool Matches(JValue current, JValue expected, string op)
        {
            var numeric = IsNumber(current.Type) && IsNumber(expected.Type);
            var comparison = numeric
                ? (current.Value is float || current.Value is double || expected.Value is float || expected.Value is double
                    ? current.Value<double>().CompareTo(expected.Value<double>())
                    : current.Value<decimal>().CompareTo(expected.Value<decimal>()))
                : (JToken.DeepEquals(current, expected) ? 0 : 1);
            switch (op)
            {
                case "eq": return comparison == 0;
                case "ne": return comparison != 0;
                case "gt": return comparison > 0;
                case "ge": return comparison >= 0;
                case "lt": return comparison < 0;
                case "le": return comparison <= 0;
                default: throw new ArgumentException("Invalid comparison.", nameof(op));
            }
        }
    }
}
#endif
