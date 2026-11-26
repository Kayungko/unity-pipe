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
            public ObjectRef Target;
            public Type FindType;
            public string Component;
            public string[] Path;
            public UnityEngine.Object Bound;
            public MemberInfo[] Members;
            public Type LeafType;
            public string Comparison;
            public JValue Expected;
            public JValue Initial;
            public bool TolerateMissing;
            public bool Pause;
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

        [CliCommand("wait_start", "Wait for a public scalar member on a scene object, optionally saving the Game View or pausing on the matching frame.", Effect = CommandEffect.Destructive, Tags = new[] { "editor", "wait" })]
        public static JObject Start(
            [CliArg("target", "Object reference to a scene object or component. Use target or find_type.")] ObjectRef target = null,
            [CliArg("field", "Dotted public instance member path ending in a scalar. Read-only properties are supported.", Required = true)] string field = null,
            [CliArg("value", "Scalar value to compare with. Omit for changed.", ArbitraryKeys = true)] JToken value = null,
            [CliArg("component", "Component short or full name when targeting a GameObject.")] string component = null,
            [CliArg("op", "eq, ne, gt, ge, lt, le or changed.")] string comparison = "eq",
            [CliArg("timeout_ms", "Wait budget in milliseconds (1 to 600000).")] int timeoutMs = 10000,
            [CliArg("capture_path", "Optional PNG path under the authoring root.")] string capturePath = null,
            [CliArg("capture_source", "camera or screen. Screen requires a fresh Game View frame.")] string captureSource = "camera",
            [CliArg("capture_width", "Capture width (1 to 4096).")] int width = 1280,
            [CliArg("capture_height", "Capture height (1 to 4096).")] int height = 720,
            [CliArg("find_type", "Fully qualified Component type. Waits for a single live scene instance.")] string findType = null,
            [CliArg("tolerate_missing", "Keep waiting when the target or an intermediate member is absent.")] bool tolerateMissing = false,
            [CliArg("pause", "Pause Play Mode when the condition matches, after any same-frame capture.")] bool pause = false)
        {
            if (s_Pending.Count >= MaxPending)
                throw new InvalidOperationException("Too many active waits.");
            if (timeoutMs < 1 || timeoutMs > 600000)
                throw new ArgumentOutOfRangeException(nameof(timeoutMs));
            var hasTarget = target != null && !target.IsEmpty;
            var hasType = !string.IsNullOrWhiteSpace(findType);
            if (hasTarget == hasType || (hasType && !string.IsNullOrEmpty(component)))
                throw new ArgumentException("Choose either target (with optional component) or find_type.");
            var segments = field?.Split('.');
            if (segments == null || segments.Any(segment => string.IsNullOrWhiteSpace(segment)))
                throw new ArgumentException("field must be a dotted member path with no empty segments.", nameof(field));
            var expected = value as JValue;
            if (comparison == "changed" ? expected != null && expected.Type != JTokenType.Null : expected == null)
                throw new ArgumentException(comparison == "changed" ? "Omit value for changed." : "value must be a scalar.", nameof(value));
            if (value != null && expected == null)
                throw new ArgumentException("value must be a scalar.", nameof(value));
            ValidateOperator(comparison);

            if (capturePath != null)
            {
                capturePath = ProjectPaths.Resolve(capturePath, out var error);
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
                Target = hasTarget ? target : null, FindType = hasType ? ResolveComponentType(findType) : null,
                Component = component, Path = segments, Comparison = comparison, Expected = expected,
                TolerateMissing = tolerateMissing || hasType, Pause = pause,
                CapturePath = capturePath, CaptureSource = captureSource, Width = width, Height = height,
                TimeoutMs = timeoutMs,
                Result = new JObject { ["waitId"] = id, ["status"] = "waiting", ["field"] = field }
            };
            if (pending.FindType != null)
                BindMembers(pending, pending.FindType);
            BindTarget(pending);
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

                    if (!BindTarget(pending) || !ReadMemberValue(pending, out var value))
                        continue;
                    pending.Result["value"] = value;
                    if (pending.Initial == null)
                    {
                        pending.Initial = (JValue)value.DeepClone();
                        if (pending.Comparison == "changed")
                        {
                            pending.Result["initialValue"] = pending.Initial.DeepClone();
                            continue;
                        }
                    }
                    var expected = pending.Comparison == "changed" ? pending.Initial : pending.Expected;
                    if (!Matches(value, expected, pending.Comparison, pending.LeafType))
                        continue;

                    if (pending.Pause && !EditorApplication.isPlaying)
                        throw new InvalidOperationException("Pause on match requires Play Mode.");
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
                    if (pending.Pause)
                    {
                        EditorApplication.isPaused = true;
                        pending.Result["paused"] = EditorApplication.isPaused;
                        pending.Result["pausedFrame"] = Time.frameCount;
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

        static Type ResolveComponentType(string name)
        {
            var types = AppDomain.CurrentDomain.GetAssemblies()
                .Select(assembly => assembly.GetType(name, false)).Where(type => type != null).ToArray();
            if (types.Length != 1 || !typeof(Component).IsAssignableFrom(types[0]))
                throw new ArgumentException("find_type must name a unique, fully qualified Unity Component type.", nameof(name));
            return types[0];
        }

        static bool BindTarget(Pending pending)
        {
            if (pending.Bound != null)
                return true;
            if (!ReferenceEquals(pending.Bound, null) && !pending.TolerateMissing)
                throw new InvalidOperationException("Wait target was destroyed.");
            UnityEngine.Object obj;
            if (pending.FindType != null)
            {
                var matches = UnityEngine.Object.FindObjectsByType(pending.FindType, FindObjectsInactive.Include, FindObjectsSortMode.None)
                    .OfType<Component>().Where(IsLiveSceneComponent).Take(2).ToArray();
                if (matches.Length > 1)
                    throw new InvalidOperationException("find_type matched multiple scene objects. Pass an explicit target.");
                if (matches.Length == 0)
                    return false;
                obj = matches[0];
            }
            else if (!ObjectResolver.TryResolve(pending.Target, out obj, out var error))
            {
                if (pending.TolerateMissing)
                    return false;
                throw new ArgumentException(error, nameof(pending.Target));
            }

            if (!string.IsNullOrEmpty(pending.Component))
            {
                if (!(obj is GameObject gameObject))
                    throw new ArgumentException("component requires a GameObject target.");
                var matches = gameObject.GetComponents<Component>()
                    .Where(c => c != null && (c.GetType().Name == pending.Component || c.GetType().FullName == pending.Component))
                    .Take(2).ToArray();
                if (matches.Length == 0 && pending.TolerateMissing)
                    return false;
                if (matches.Length != 1)
                    throw new ArgumentException("component must resolve to exactly one component.");
                obj = matches[0];
            }
            BindMembers(pending, obj.GetType());
            pending.Bound = obj;
            return true;
        }

        static bool IsLiveSceneComponent(Component component)
        {
            var sceneObject = component.gameObject;
            return !EditorUtility.IsPersistent(component) && sceneObject.scene.IsValid() && sceneObject.scene.isLoaded &&
                !UnityEditor.SceneManagement.EditorSceneManager.IsPreviewScene(sceneObject.scene) &&
                component.hideFlags == HideFlags.None && sceneObject.hideFlags == HideFlags.None;
        }

        static void BindMembers(Pending pending, Type type)
        {
            const BindingFlags flags = BindingFlags.Public | BindingFlags.Instance;
            var members = new MemberInfo[pending.Path.Length];
            for (var i = 0; i < pending.Path.Length; i++)
            {
                var name = pending.Path[i];
                var field = type.GetField(name, flags);
                if (field != null)
                {
                    members[i] = field;
                    type = field.FieldType;
                    continue;
                }
                var property = type.GetProperty(name, flags);
                if (property == null || property.GetGetMethod(false) == null || property.GetSetMethod(false) != null ||
                    property.GetIndexParameters().Length != 0)
                    throw new ArgumentException($"'{name}' on {type.Name} is not a public instance field or readable property without a public setter.");
                members[i] = property;
                type = property.PropertyType;
            }
            type = Nullable.GetUnderlyingType(type) ?? type;
            if (!IsScalar(type))
                throw new ArgumentException("The last member must be a scalar.");
            ValidateComparison(type, pending.Expected, pending.Comparison);
            pending.Members = members;
            pending.LeafType = type;
        }

        static bool ReadMemberValue(Pending pending, out JValue value)
        {
            object current = pending.Bound;
            foreach (var member in pending.Members)
            {
                if (current == null || (current is UnityEngine.Object unityObject && unityObject == null))
                {
                    if (!pending.TolerateMissing)
                        throw new InvalidOperationException($"An intermediate member was null before '{member.Name}'.");
                    value = null;
                    return false;
                }
                current = member is FieldInfo field ? field.GetValue(current) : ((PropertyInfo)member).GetValue(current);
            }
            value = new JValue(pending.LeafType.IsEnum ? current?.ToString() : current);
            return true;
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
            type == typeof(string) || type == typeof(bool) || type.IsEnum ||
            (type.IsPrimitive && type != typeof(char) && type != typeof(IntPtr) && type != typeof(UIntPtr)) ||
            type == typeof(decimal);

        static void ValidateOperator(string op)
        {
            switch (op)
            {
                case "eq":
                case "ne":
                case "gt":
                case "ge":
                case "lt":
                case "le":
                case "changed":
                    break;
                default:
                    throw new ArgumentException("Unknown comparison. Use eq, ne, gt, ge, lt, le or changed.", nameof(op));
            }
        }

        static void ValidateComparison(Type type, JValue expected, string op)
        {
            if (op == "changed")
                return;
            if (type == typeof(bool) || type == typeof(string) || type.IsEnum)
            {
                if (op != "eq" && op != "ne")
                    throw new ArgumentException("Boolean, string and enum members support eq, ne and changed.");
                if (type == typeof(bool) && expected.Type != JTokenType.Boolean ||
                    (type == typeof(string) || type.IsEnum) && expected.Type != JTokenType.String && expected.Type != JTokenType.Null)
                    throw new ArgumentException("value type does not match the field.");
            }
            else if (!IsNumber(expected.Type))
            {
                throw new ArgumentException("Numeric fields require a numeric value.");
            }
        }

        static bool Matches(JValue current, JValue expected, string op, Type type)
        {
            var numeric = IsNumber(current.Type) && IsNumber(expected.Type);
            if (!numeric && op != "eq" && op != "ne" && op != "changed")
                return false;
            var comparison = JToken.DeepEquals(current, expected) ? 0 : 1;
            if (numeric)
            {
                if (type == typeof(float))
                    comparison = current.Value<float>().CompareTo(expected.Value<float>());
                else if (type == typeof(double))
                    comparison = current.Value<double>().CompareTo(expected.Value<double>());
                else
                    comparison = current.Value<decimal>().CompareTo(expected.Value<decimal>());
            }
            switch (op)
            {
                case "eq": return comparison == 0;
                case "ne": return comparison != 0;
                case "changed": return comparison != 0;
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
