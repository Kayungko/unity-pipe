#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using Newtonsoft.Json;
using Unity.Pipeline.Commands;
using Unity.Profiling;
using Unity.Profiling.LowLevel;
using UnityEditor;
using UnityEditor.Profiling;
using UnityEditorInternal;
using UnityEngine;
using UnityEngine.Profiling;

namespace Unity.Pipeline.Editor.Commands.Observability
{
    /// <summary>
    /// Render statistics for the most recently rendered Editor frame, read from
    /// <see cref="UnityStats"/>. These reflect the last frame the Editor drew (Game/Scene view), not
    /// an averaged measurement — treat them as a snapshot.
    /// </summary>
    [Serializable]
    class RenderStats
    {
        /// <summary>Draw calls issued for the last rendered frame.</summary>
        [JsonProperty("drawCalls")]
        public int DrawCalls { get; set; }

        /// <summary>Total batches (dynamic + static + instanced) for the last rendered frame.</summary>
        [JsonProperty("batches")]
        public int Batches { get; set; }

        /// <summary>SetPass calls (shader pass switches) for the last rendered frame.</summary>
        [JsonProperty("setPassCalls")]
        public int SetPassCalls { get; set; }

        /// <summary>Triangles submitted for the last rendered frame.</summary>
        [JsonProperty("triangles")]
        public int Triangles { get; set; }

        /// <summary>Vertices submitted for the last rendered frame.</summary>
        [JsonProperty("vertices")]
        public int Vertices { get; set; }
    }

    /// <summary>
    /// Process memory usage read from <see cref="Profiler"/>. Values are in bytes and are always
    /// available at runtime regardless of whether the Profiler window is open.
    /// </summary>
    [Serializable]
    class MemoryStats
    {
        /// <summary>Total memory currently allocated by Unity (bytes).</summary>
        [JsonProperty("totalAllocatedBytes")]
        public long TotalAllocatedBytes { get; set; }

        /// <summary>Total memory reserved by Unity (bytes).</summary>
        [JsonProperty("totalReservedBytes")]
        public long TotalReservedBytes { get; set; }

        /// <summary>Mono managed heap memory in use (bytes).</summary>
        [JsonProperty("monoUsedBytes")]
        public long MonoUsedBytes { get; set; }

        /// <summary>Mono managed heap size reserved (bytes).</summary>
        [JsonProperty("monoHeapBytes")]
        public long MonoHeapBytes { get; set; }
    }

    /// <summary>
    /// Latest CPU/GPU frame timing from <see cref="FrameTimingManager"/>. The Frame Timing Manager
    /// must have a timing captured for <see cref="Available"/> to be true; when unavailable the
    /// timing fields are left at zero.
    /// </summary>
    [Serializable]
    class FrameTimingStats
    {
        /// <summary>True when a frame timing was captured and the milliseconds fields are meaningful.</summary>
        [JsonProperty("available")]
        public bool Available { get; set; }

        /// <summary>Total CPU frame time in milliseconds.</summary>
        [JsonProperty("cpuFrameTimeMs")]
        public double CpuFrameTimeMs { get; set; }

        /// <summary>Total GPU frame time in milliseconds.</summary>
        [JsonProperty("gpuFrameTimeMs")]
        public double GpuFrameTimeMs { get; set; }

        /// <summary>CPU main-thread frame time in milliseconds.</summary>
        [JsonProperty("cpuMainThreadFrameTimeMs")]
        public double CpuMainThreadFrameTimeMs { get; set; }
    }

    /// <summary>
    /// Composite performance snapshot returned by <c>get_performance_stats</c>: render counters,
    /// process memory, and the latest frame timing.
    /// </summary>
    [Serializable]
    class PerformanceStats
    {
        /// <summary>Render counters for the last rendered Editor frame.</summary>
        [JsonProperty("render")]
        public RenderStats Render { get; set; }

        /// <summary>Process memory usage at capture time.</summary>
        [JsonProperty("memory")]
        public MemoryStats Memory { get; set; }

        /// <summary>Latest CPU/GPU frame timing, if one was captured.</summary>
        [JsonProperty("frameTiming")]
        public FrameTimingStats FrameTiming { get; set; }
    }

    /// <summary>
    /// Read-only telemetry command (CLI-198) that snapshots render, memory, and frame-timing stats so
    /// an agent can reason about Editor performance without opening the Profiler or Stats overlay.
    /// </summary>
    static class PerformanceCommands
    {
        [CliCommand("get_performance_stats", "Read render, memory, and frame-timing stats (structured, read-only).", Tags = new[] { "observability/performance" })]
        public static PerformanceStats GetPerformanceStats()
        {
            return new PerformanceStats
            {
                Render = new RenderStats
                {
                    // UnityStats reflects the last frame rendered by the Editor (Game/Scene view).
                    DrawCalls = UnityStats.drawCalls,
                    Batches = UnityStats.dynamicBatches + UnityStats.staticBatches + UnityStats.instancedBatches,
                    SetPassCalls = UnityStats.setPassCalls,
                    Triangles = UnityStats.triangles,
                    Vertices = UnityStats.vertices
                },
                Memory = new MemoryStats
                {
                    TotalAllocatedBytes = Profiler.GetTotalAllocatedMemoryLong(),
                    TotalReservedBytes = Profiler.GetTotalReservedMemoryLong(),
                    MonoUsedBytes = Profiler.GetMonoUsedSizeLong(),
                    MonoHeapBytes = Profiler.GetMonoHeapSizeLong()
                },
                FrameTiming = CaptureFrameTiming()
            };
        }

        /// <summary>
        /// Capture the latest CPU/GPU frame timing. The Frame Timing Manager only yields data once a
        /// timing has been captured (and platform support exists), so any failure or empty result is
        /// reported as <see cref="FrameTimingStats.Available"/> = false rather than throwing.
        /// </summary>
        private static FrameTimingStats CaptureFrameTiming()
        {
            try
            {
                FrameTimingManager.CaptureFrameTimings();

                var timings = new FrameTiming[1];
                uint received = FrameTimingManager.GetLatestTimings(1, timings);
                if (received > 0)
                {
                    return new FrameTimingStats
                    {
                        Available = true,
                        CpuFrameTimeMs = timings[0].cpuFrameTime,
                        GpuFrameTimeMs = timings[0].gpuFrameTime,
                        CpuMainThreadFrameTimeMs = timings[0].cpuMainThreadFrameTime
                    };
                }
            }
            catch
            {
                // Frame timing is platform-dependent and may be unsupported; report as unavailable.
            }

            return new FrameTimingStats { Available = false };
        }
    }

    [InitializeOnLoad]
    static class ProfilerCaptureCommands
    {
        const int EditorTargetId = -1;

        static Recording s_Recording;
        static object s_RecordResult = new { status = "idle" };
        static bool s_RecordSaved;
        static bool s_SnapshotRunning;
        static object s_SnapshotResult = new { status = "idle" };
        static readonly Dictionary<string, CaptureSession> Sessions = new Dictionary<string, CaptureSession>(StringComparer.Ordinal);
        static CaptureSession s_Resident;
        static long s_HistoryRevision;

        sealed class CaptureSession
        {
            public string Id;
            public string File;
            public long Bytes;
            public DateTime ModifiedUtc;
            public long LoadedRevision;
        }

        sealed class Recording
        {
            public string Path;
            public bool AllowOverwrite;
            public int FrameTarget;
            public double Deadline;
            public bool WasEnabled;
            public bool WasProfilingEditor;
            public int PreviousTarget;
            public bool WasCpuEnabled;
            public bool WasMemoryEnabled;
            public ProfilerMemoryRecordMode? PreviousMemoryMode;
        }

        static ProfilerCaptureCommands()
        {
            AssemblyReloadEvents.beforeAssemblyReload += RestoreProfiler;
            EditorApplication.quitting += RestoreProfiler;
            ProfilerDriver.profileLoaded += HistoryChanged;
            ProfilerDriver.profileCleared += HistoryChanged;
            ProfilerDriver.NewProfilerFrameRecorded += FrameRecorded;
        }

        static void HistoryChanged() => System.Threading.Interlocked.Increment(ref s_HistoryRevision);
        static void FrameRecorded(int _, int __) => HistoryChanged();
        static long HistoryRevision => System.Threading.Interlocked.Read(ref s_HistoryRevision);

        static bool IsResident(CaptureSession session) =>
            ReferenceEquals(s_Resident, session) && session.LoadedRevision == HistoryRevision;

        [CliCommand("profiler_session", "Open a saved .data capture as an Editor-local analysis session, or list sessions.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object OpenSession(
            [CliArg("file", "Capture filename under Library/Pipeline/Profiles. Omit to list sessions.")] string file = null,
            [CliArg("confirm", "Allow replacing current Profiler history.")] bool confirm = false)
        {
            if (file == null)
                return new { sessions = Sessions.Values.Select(session =>
                    new { sessionId = session.Id, file = session.File, resident = IsResident(session) }).ToArray() };

            var info = new FileInfo(ProfilePath(file, ".data"));
            if (!info.Exists)
                throw new IOException($"Unity could not open '{file}' as a Profiler capture.");
            var sessionForFile = Sessions.Values.FirstOrDefault(session =>
                session.File == file && session.Bytes == info.Length && session.ModifiedUtc == info.LastWriteTimeUtc);
            var candidate = sessionForFile ?? new CaptureSession
            {
                Id = Guid.NewGuid().ToString("N"), File = file, Bytes = info.Length, ModifiedUtc = info.LastWriteTimeUtc
            };
            var loaded = EnsureResident(candidate, confirm);
            if (sessionForFile == null)
                Sessions.Add(candidate.Id, candidate);
            return new { sessionId = candidate.Id, file, loaded, firstFrame = ProfilerDriver.firstFrameIndex, lastFrame = ProfilerDriver.lastFrameIndex };
        }

        [CliCommand("profiler_close", "Close an analysis session. Leaves the current Profiler history visible.", Effect = CommandEffect.Stateful, Tags = new[] { "observability/profiling" })]
        public static object CloseSession(
            [CliArg("session_id", "ID returned by profiler_session.", Required = true)] string sessionId)
        {
            var session = GetSession(sessionId);
            Sessions.Remove(sessionId);
            if (ReferenceEquals(s_Resident, session))
                s_Resident = null;
            return new { sessionId, closed = true };
        }

        static CaptureSession GetSession(string id)
        {
            if (string.IsNullOrEmpty(id) || !Sessions.TryGetValue(id, out var session))
                throw new ArgumentException("Unknown or expired Profiler session. Open the capture again after a domain reload or Editor restart.", nameof(id));
            return session;
        }

        static bool EnsureResident(CaptureSession session, bool confirm)
        {
            RequireProfilerIdle();
            var file = new FileInfo(ProfilePath(session.File, ".data"));
            if (!file.Exists || file.Length != session.Bytes || file.LastWriteTimeUtc != session.ModifiedUtc)
                throw new IOException("The session's capture changed or was removed. Open the file again.");
            if (IsResident(session))
                return false;
            LoadCapture(session.File, confirm);
            session.LoadedRevision = HistoryRevision;
            s_Resident = session;
            return true;
        }

        static void SelectCapture(string file, string sessionId, bool confirm)
        {
            if (string.IsNullOrEmpty(file) == string.IsNullOrEmpty(sessionId))
                throw new ArgumentException("Provide exactly one of file or session_id.");
            if (string.IsNullOrEmpty(sessionId))
                LoadCapture(file, confirm);
            else
                EnsureResident(GetSession(sessionId), confirm);
        }

        [CliCommand("profiler_targets", "List local Editor and discovered Unity Profiler targets.", Tags = new[] { "observability/profiling" })]
        public static object Targets()
        {
            var current = ProfilerDriver.connectedProfiler;
            var players = ProfilerDriver.GetAvailableProfilers().Where(id => id != EditorTargetId && ProfilerDriver.IsIdentifierConnectable(id));
            if (!ProfilerDriver.profileEditor && current != EditorTargetId)
                players = players.Append(current).Distinct();
            return new
            {
                editor = new { current = ProfilerDriver.profileEditor },
                players = players.Select(id => new { id, name = ProfilerDriver.GetConnectionIdentifier(id), current = !ProfilerDriver.profileEditor && id == current }).ToArray()
            };
        }

        [CliCommand("profiler_record", "Record Editor or selected development Player frames to a local .data file. Poll profiler_record_status.", Tags = new[] { "observability/profiling" })]
        public static object Record(
            [CliArg("file", "File name ending in .data under Library/Pipeline/Profiles.", Required = true)] string file,
            [CliArg("frames", "Number of frames to record, 3 through 200.")] int frames = 30,
            [CliArg("player_id", "Target ID from profiler_targets. Omit for the Editor.")] int? playerId = null,
            [CliArg("confirm", "Allow replacing the Editor Profiler's current recording/history and existing output.")] bool confirm = false,
            [CliArg("gc_callstacks", "Record call stacks for GC.Alloc samples. Adds profiling overhead.")] bool gcCallstacks = false)
        {
            RequireIdle();
            if (frames < 3 || frames > 200)
                throw new ArgumentOutOfRangeException(nameof(frames), "Use 3 through 200 frames.");
            var path = ProfilePath(file, ".data");
            if (!confirm && (ProfilerDriver.enabled || ProfilerDriver.lastFrameIndex >= 0 || File.Exists(path)))
                throw new InvalidOperationException("Pass confirm=true to replace Profiler history or an existing capture.");
            var alreadyAttached = playerId.HasValue && !ProfilerDriver.profileEditor && playerId.Value == ProfilerDriver.connectedProfiler;
            if (playerId.HasValue && (playerId.Value == EditorTargetId || (!alreadyAttached &&
                (!ProfilerDriver.GetAvailableProfilers().Contains(playerId.Value) || !ProfilerDriver.IsIdentifierConnectable(playerId.Value)))))
                throw new ArgumentException("The selected Player is no longer available. Refresh profiler_targets.");

            s_Recording = new Recording
            {
                Path = path,
                AllowOverwrite = confirm,
                FrameTarget = frames,
                Deadline = EditorApplication.timeSinceStartup + 30,
                WasEnabled = ProfilerDriver.enabled,
                WasProfilingEditor = ProfilerDriver.profileEditor,
                PreviousTarget = ProfilerDriver.connectedProfiler,
                WasCpuEnabled = ProfilerDriver.IsAreaEnabled(ProfilerArea.CPU),
                WasMemoryEnabled = ProfilerDriver.IsAreaEnabled(ProfilerArea.Memory),
                PreviousMemoryMode = gcCallstacks ? ProfilerDriver.memoryRecordMode : (ProfilerMemoryRecordMode?)null
            };
            s_RecordSaved = false;
            try
            {
                ProfilerDriver.enabled = false;
                if (ProfilerDriver.profileEditor == playerId.HasValue)
                    ProfilerDriver.profileEditor = !playerId.HasValue;
                if (playerId.HasValue && !alreadyAttached)
                {
                    ProfilerDriver.connectedProfiler = playerId.Value;
                    if (ProfilerDriver.connectedProfiler != playerId.Value)
                        throw new InvalidOperationException("Unity did not attach to the selected Player.");
                }
                ProfilerDriver.SetAreaEnabled(ProfilerArea.CPU, true);
                ProfilerDriver.SetAreaEnabled(ProfilerArea.Memory, true);
                if (gcCallstacks)
                    ProfilerDriver.memoryRecordMode |= ProfilerMemoryRecordMode.GCAlloc;
                ProfilerDriver.ClearAllFrames();
                ProfilerDriver.enabled = true;
                EditorApplication.update += UpdateRecording;
                s_RecordResult = new { status = "recording", target = playerId.HasValue ? $"player:{playerId.Value}" : "editor", frames };
                return s_RecordResult;
            }
            catch (Exception ex)
            {
                s_RecordResult = new { status = "failed", message = ex.Message };
                RestoreProfiler();
                throw;
            }
        }

        [CliCommand("profiler_record_status", "Read the current recording status or completed .data path.", MainThreadRequired = false, Tags = new[] { "observability/profiling" })]
        public static object RecordStatus() => System.Threading.Volatile.Read(ref s_RecordResult);

        [CliCommand("profiler_record_stop", "Stop an active Pipeline recording and save the frames captured so far.", Effect = CommandEffect.Stateful, Tags = new[] { "observability/profiling" })]
        public static object StopRecord()
        {
            var recording = s_Recording;
            if (recording == null)
            {
                if (s_RecordSaved)
                    return s_RecordResult;
                throw new InvalidOperationException("No Pipeline Profiler recording is active.");
            }

            if (RecordedFrameCount() == 0)
                throw new InvalidOperationException("No frames have arrived yet. The Profiler is still recording.");

            return SaveRecording(recording);
        }

        [CliCommand("profiler_cancel", "Cancel a Pipeline recording and restore the previous Profiler settings.", Tags = new[] { "observability/profiling" })]
        public static object CancelRecord()
        {
            if (s_Recording != null)
            {
                RestoreProfiler();
                s_RecordResult = new { status = "cancelled" };
            }
            return s_RecordResult;
        }

        static int RecordedFrameCount()
        {
            var first = ProfilerDriver.firstFrameIndex;
            var last = ProfilerDriver.lastFrameIndex;
            return first >= 0 && last >= first ? last - first + 1 : 0;
        }

        static void UpdateRecording()
        {
            var recording = s_Recording;
            // Unity may have copied its update delegates before a stop command removed ours.
            if (recording == null)
                return;
            var count = RecordedFrameCount();
            if (count < recording.FrameTarget)
            {
                if (EditorApplication.timeSinceStartup >= recording.Deadline)
                {
                    s_RecordResult = new { status = "failed", message = $"Only {count} frames arrived within 30 seconds." };
                    RestoreProfiler();
                }
                return;
            }

            try
            {
                SaveRecording(recording);
            }
            catch
            {
                // SaveRecording publishes the failure for profiler_record_status.
            }
        }

        static object SaveRecording(Recording recording)
        {
            try
            {
                ProfilerDriver.enabled = false;
                var first = ProfilerDriver.firstFrameIndex;
                var last = ProfilerDriver.lastFrameIndex;
                if (first < 0 || last < first)
                    throw new InvalidOperationException("The Profiler has no captured frames to save.");

                Directory.CreateDirectory(Path.GetDirectoryName(recording.Path));
                var staging = Path.Combine(Path.GetDirectoryName(recording.Path), $".{Guid.NewGuid():N}.data");
                try
                {
                    ProfilerDriver.SaveProfile(staging);
                    if (!File.Exists(staging) || new FileInfo(staging).Length == 0)
                        throw new IOException("Unity did not write the Profiler capture.");
                    if (recording.AllowOverwrite && File.Exists(recording.Path))
                        File.Replace(staging, recording.Path, null);
                    else
                        File.Move(staging, recording.Path);
                }
                finally
                {
                    if (File.Exists(staging))
                        File.Delete(staging);
                }
                s_RecordResult = new { status = "completed", path = recording.Path, frames = last - first + 1, firstFrame = first, lastFrame = last };
                s_RecordSaved = true;
                return s_RecordResult;
            }
            catch (Exception ex)
            {
                s_RecordResult = new { status = "failed", message = ex.Message };
                throw;
            }
            finally
            {
                RestoreProfiler();
            }
        }

        static void RestoreProfiler()
        {
            if (s_Recording == null)
                return;
            var recording = s_Recording;
            EditorApplication.update -= UpdateRecording;
            s_Recording = null;
            ProfilerDriver.enabled = false;
            if (recording.PreviousTarget != ProfilerDriver.connectedProfiler)
                ProfilerDriver.connectedProfiler = recording.PreviousTarget;
            if (ProfilerDriver.profileEditor != recording.WasProfilingEditor)
                ProfilerDriver.profileEditor = recording.WasProfilingEditor;
            ProfilerDriver.SetAreaEnabled(ProfilerArea.CPU, recording.WasCpuEnabled);
            ProfilerDriver.SetAreaEnabled(ProfilerArea.Memory, recording.WasMemoryEnabled);
            if (recording.PreviousMemoryMode.HasValue)
                ProfilerDriver.memoryRecordMode = recording.PreviousMemoryMode.Value;
            ProfilerDriver.enabled = recording.WasEnabled;
        }

        [CliCommand("profiler_analyze", "Read frame timings, GC allocation and marker hot spots from a saved .data capture.", Tags = new[] { "observability/profiling" })]
        public static object Analyze(
            [CliArg("file", "Saved .data file under Library/Pipeline/Profiles.", Required = true)] string file,
            [CliArg("first_frame", "Optional first frame index.")] int? firstFrame = null,
            [CliArg("max_frames", "Maximum frames to read, 1 through 300.")] int maxFrames = 120,
            [CliArg("thread", "Profiler thread index. Defaults to 0.")] int thread = 0,
            [CliArg("top", "Maximum number of markers to return, 1 through 50.")] int top = 15,
            [CliArg("confirm", "Allow replacing existing Profiler history with the file.")] bool confirm = false)
        {
            if (maxFrames < 1 || maxFrames > 300 || top < 1 || top > 50 || thread < 0)
                throw new ArgumentException("Use 1–300 frames, 1–50 markers and a non-negative thread index.");
            LoadCapture(file, confirm);

            var markers = new Dictionary<string, MarkerTotals>(StringComparer.Ordinal);
            var frames = new List<object>();
            var first = firstFrame ?? ProfilerDriver.firstFrameIndex;
            var last = ProfilerDriver.lastFrameIndex;
            if (first < ProfilerDriver.firstFrameIndex || first > last)
                throw new ArgumentException("The requested first frame is outside the capture.");
            var maxDuration = 0f;
            double totalDuration = 0;
            long totalGc = 0;
            var gcFrames = 0;
            var end = first + Math.Min(maxFrames - 1, last - first);
            for (var frame = first; frame <= end; frame++)
            {
                using (var data = ProfilerDriver.GetRawFrameDataView(frame, thread))
                {
                    if (!data.valid)
                        continue;
                    var gcBytes = ReadGcBytes(data, requireCounterSample: false);
                    if (gcBytes.HasValue)
                    {
                        gcFrames++;
                        totalGc += gcBytes.Value;
                    }
                    frames.Add(new { frame, thread = data.threadName, ms = data.frameTimeMs, gcBytes });
                    maxDuration = Math.Max(maxDuration, data.frameTimeMs);
                    totalDuration += data.frameTimeMs;
                    for (var sample = 0; sample < data.sampleCount; sample++)
                    {
                        var name = data.GetSampleName(sample);
                        if (string.IsNullOrEmpty(name))
                            continue;
                        if (!markers.TryGetValue(name, out var sum))
                            markers.Add(name, sum = new MarkerTotals());
                        var inclusive = data.GetSampleTimeMs(sample);
                        sum.Calls++;
                        sum.TotalMs += inclusive;
                        sum.SelfMs += SampleSelfTime(data, sample);
                    }
                }
            }

            if (frames.Count == 0)
                throw new InvalidOperationException("This capture has no frames for the requested thread.");
            return new
            {
                frames,
                summary = new { frameCount = frames.Count, averageMs = totalDuration / frames.Count, maxMs = maxDuration,
                    gcFrames, gcBytes = gcFrames > 0 ? totalGc : (long?)null, truncated = end < last },
                hotspots = markers.OrderByDescending(pair => pair.Value.SelfMs).ThenBy(pair => pair.Key, StringComparer.Ordinal).Take(top).Select(pair =>
                    new { marker = pair.Key, calls = pair.Value.Calls, selfMs = pair.Value.SelfMs, totalMs = pair.Value.TotalMs }).ToArray()
            };
        }

        [CliCommand("profiler_rank_frames", "Rank frames in an offline capture by frame duration or GC allocation.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object RankFrames(
            [CliArg("file", "Capture filename under Library/Pipeline/Profiles.")] string file = null,
            [CliArg("metric", "duration_ms or gc_bytes (recorded GC.Alloc samples across threads).")] string metric = "duration_ms",
            [CliArg("first_frame", "First frame to scan.")] int? firstFrame = null,
            [CliArg("max_frames", "Number of frames to scan (1–300).")] int maxFrames = 120,
            [CliArg("top", "Number of ranked frames to return (1–50).")] int top = 10,
            [CliArg("confirm", "Allow replacing existing Profiler history.")] bool confirm = false,
            [CliArg("session_id", "Analysis session ID instead of a file.")] string sessionId = null)
        {
            if (metric != "duration_ms" && metric != "gc_bytes")
                throw new ArgumentException("Use duration_ms or gc_bytes.", nameof(metric));
            if (maxFrames < 1 || maxFrames > 300 || top < 1 || top > 50)
                throw new ArgumentException("Use 1–300 frames and 1–50 ranked results.");
            SelectCapture(file, sessionId, confirm);
            var first = firstFrame ?? ProfilerDriver.firstFrameIndex;
            ValidateFrame(first);
            var last = ProfilerDriver.lastFrameIndex;
            var end = first + Math.Min(last - first, maxFrames - 1);
            var rows = new List<(int frame, double ms, long? gcBytes)>();
            var validFrames = 0;
            for (var frame = first; frame <= end; frame++)
            {
                using (var data = ProfilerDriver.GetRawFrameDataView(frame, 0))
                {
                    if (!data.valid)
                        continue;
                    validFrames++;
                    var gcBytes = metric == "gc_bytes" ? SumGcSampleBytes(frame, data) : ReadGcBytes(data);
                    if (metric == "duration_ms" || gcBytes.HasValue)
                        rows.Add((frame, data.frameTimeMs, gcBytes));
                }
            }

            var ordered = metric == "gc_bytes"
                ? rows.OrderByDescending(row => row.gcBytes)
                : rows.OrderByDescending(row => row.ms);
            return new
            {
                metric, gcSource = metric == "gc_bytes" ? "allocation_samples_or_frame_counter" : "frame_counter",
                firstFrame = first, lastFrame = end, validFrames, eligibleFrames = rows.Count,
                truncated = end < last,
                frames = ordered.ThenBy(row => row.frame).Take(top)
                    .Select(row => new { row.frame, row.ms, row.gcBytes }).ToArray()
            };
        }

        [CliCommand("profiler_frame_paths", "Rank sample instances by self time and return their recorded parent chains for one frame.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object FramePaths(
            [CliArg("frame", "Profiler frame index.", Required = true)] int? frameIndex,
            [CliArg("file", "Capture filename under Library/Pipeline/Profiles.")] string file = null,
            [CliArg("thread", "Profiler thread index.")] int thread = 0,
            [CliArg("top", "Maximum samples to return (1–50).")] int top = 10,
            [CliArg("marker", "Optional exact sample marker name.")] string marker = null,
            [CliArg("confirm", "Allow replacing existing Profiler history.")] bool confirm = false,
            [CliArg("session_id", "Analysis session ID instead of a file.")] string sessionId = null,
            [CliArg("sample_index", "Optional exact sample index from a Profiler result.")] int? sampleIndex = null,
            [CliArg("exclude_markers", "Exact marker names to omit from the ranking.")] string[] excludeMarkers = null)
        {
            if (top < 1 || top > 50 || thread < 0)
                throw new ArgumentException("Use 1–50 samples and a non-negative thread index.");
            var frame = frameIndex ?? throw new ArgumentException("Provide a frame index.");
            SelectCapture(file, sessionId, confirm);
            ValidateFrame(frame);
            using (var data = ProfilerDriver.GetRawFrameDataView(frame, thread))
            {
                if (!data.valid)
                    throw new ArgumentException("The requested thread is not present in this frame.", nameof(thread));
                if (sampleIndex < 0 || sampleIndex >= data.sampleCount)
                    throw new ArgumentException("The requested sample index is outside this thread.", nameof(sampleIndex));
                var excluded = new HashSet<string>(excludeMarkers ?? Array.Empty<string>(), StringComparer.Ordinal);
                var samples = new List<(int index, string name, float inclusive, float self)>();
                var parents = new int[data.sampleCount];
                var ancestors = new Stack<(int index, int end)>();
                for (var index = 0; index < data.sampleCount; index++)
                {
                    while (ancestors.Count > 0 && index >= ancestors.Peek().end)
                        ancestors.Pop();
                    parents[index] = ancestors.Count == 0 ? -1 : ancestors.Peek().index;
                    var children = data.GetSampleChildrenCountRecursive(index);
                    if (children > 0)
                        ancestors.Push((index, index + children + 1));
                    var name = data.GetSampleName(index);
                    if ((!sampleIndex.HasValue || index == sampleIndex.Value) &&
                        (string.IsNullOrEmpty(marker) || name == marker) && !excluded.Contains(name))
                        samples.Add((index, name, data.GetSampleTimeMs(index), SampleSelfTime(data, index)));
                }
                return new
                {
                    frame, threadIndex = thread, thread = data.threadName, ms = data.frameTimeMs,
                    samples = samples.OrderByDescending(sample => sample.self).ThenBy(sample => sample.index).Take(top)
                        .Select(sample => new
                        {
                            sampleIndex = sample.index, marker = sample.name, totalMs = sample.inclusive,
                            selfMs = sample.self, path = SamplePath(data, parents, sample.index)
                        }).ToArray()
                };
            }
        }

        static string[] SamplePath(UnityEditor.Profiling.RawFrameDataView data, int[] parents, int index)
        {
            var path = new Stack<string>();
            for (var sample = index; sample >= 0; sample = parents[sample])
                path.Push(data.GetSampleName(sample));
            return path.ToArray();
        }

        [CliCommand("profiler_threads", "List the threads recorded in a capture frame.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object ListThreads(
            [CliArg("session_id", "Analysis session ID.", Required = true)] string sessionId,
            [CliArg("frame", "Profiler frame index.", Required = true)] int? frameIndex,
            [CliArg("confirm", "Allow replacing existing Profiler history.")] bool confirm = false)
        {
            var frame = frameIndex ?? throw new ArgumentException("Provide a frame index.");
            EnsureResident(GetSession(sessionId), confirm);
            ValidateFrame(frame);
            var threads = new List<object>();
            for (var index = 0; ; index++)
            {
                using (var data = ProfilerDriver.GetRawFrameDataView(frame, index))
                {
                    if (!data.valid)
                        break;
                    threads.Add(new
                    {
                        threadIndex = index, threadId = data.threadId.ToString(CultureInfo.InvariantCulture),
                        thread = data.threadName, group = data.threadGroupName, samples = data.sampleCount
                    });
                }
            }
            return new { sessionId, frame, threads };
        }

        [CliCommand("profiler_rank_markers", "Aggregate marker timings by thread across an inclusive capture range.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object RankMarkers(
            [CliArg("session_id", "Analysis session ID.", Required = true)] string sessionId,
            [CliArg("first_frame", "First frame in the range. Defaults to the first captured frame.")] int? firstFrame = null,
            [CliArg("last_frame", "Last frame in the range. Defaults to the last captured frame.")] int? lastFrame = null,
            [CliArg("thread_id", "Optional thread ID from profiler_threads.")] string threadId = null,
            [CliArg("marker", "Optional exact marker name.")] string marker = null,
            [CliArg("exclude_markers", "Exact marker names to omit from the ranking.")] string[] excludeMarkers = null,
            [CliArg("top", "Maximum ranked results (1–50).")] int top = 10,
            [CliArg("confirm", "Allow replacing existing Profiler history.")] bool confirm = false)
        {
            if (top < 1 || top > 50)
                throw new ArgumentOutOfRangeException(nameof(top), "Use 1–50 markers.");
            ulong selectedThread = 0;
            if (threadId != null && !ulong.TryParse(threadId, NumberStyles.None, CultureInfo.InvariantCulture, out selectedThread))
                throw new ArgumentException("Use a thread ID from profiler_threads.", nameof(threadId));
            EnsureResident(GetSession(sessionId), confirm);
            var first = firstFrame ?? ProfilerDriver.firstFrameIndex;
            var last = lastFrame ?? ProfilerDriver.lastFrameIndex;
            ValidateFrame(first);
            ValidateFrame(last);
            if (last < first || last - first >= 300)
                throw new ArgumentException("Select an inclusive range of 1–300 frames.");

            var excluded = new HashSet<string>(excludeMarkers ?? Array.Empty<string>(), StringComparer.Ordinal);
            var rows = new Dictionary<(ulong id, string group, string thread, string name), MarkerAggregate>();
            for (var frame = first; frame <= last; frame++)
            {
                for (var thread = 0; ; thread++)
                {
                    using (var data = ProfilerDriver.GetRawFrameDataView(frame, thread))
                    {
                        if (!data.valid)
                            break;
                        if (threadId == null || data.threadId == selectedThread)
                            AccumulateMarkers(data, marker, excluded, rows);
                    }
                }
            }

            var ranked = rows.OrderByDescending(entry => entry.Value.SelfMs)
                .ThenBy(entry => entry.Key.name, StringComparer.Ordinal).ThenBy(entry => entry.Key.id)
                .ThenBy(entry => entry.Key.group, StringComparer.Ordinal).ThenBy(entry => entry.Key.thread, StringComparer.Ordinal)
                .Take(top).Select(entry => new
                {
                    marker = entry.Key.name, threadId = entry.Key.id.ToString(CultureInfo.InvariantCulture),
                    thread = entry.Key.thread, group = entry.Key.group,
                    samples = entry.Value.Samples, frames = entry.Value.Frames,
                    totalMs = entry.Value.TotalMs, selfMs = entry.Value.SelfMs, coveredMs = entry.Value.CoveredMs,
                    peak = new
                    {
                        frame = entry.Value.PeakFrame, threadIndex = entry.Value.PeakThread, sampleIndex = entry.Value.PeakSample,
                        selfMs = entry.Value.PeakSelfMs
                    }
                }).ToArray();
            return new { sessionId, firstFrame = first, lastFrame = last, matchingMarkers = rows.Count, markers = ranked };
        }

        sealed class MarkerAggregate
        {
            public int Samples, Frames;
            public double TotalMs, SelfMs, CoveredMs;
            public float PeakSelfMs = -1;
            public int PeakFrame, PeakThread, PeakSample;
        }

        static void AccumulateMarkers(RawFrameDataView data, string marker, HashSet<string> excluded,
            Dictionary<(ulong id, string group, string thread, string name), MarkerAggregate> rows)
        {
            var intervalsByMarker = new Dictionary<MarkerAggregate, List<(double start, double end)>>();
            var threadId = data.threadId;
            var threadGroup = data.threadGroupName;
            var threadName = data.threadName;
            for (var sample = 0; sample < data.sampleCount; sample++)
            {
                var name = data.GetSampleName(sample);
                if ((marker != null && name != marker) || excluded.Contains(name))
                    continue;
                var key = (threadId, threadGroup, threadName, name);
                if (!rows.TryGetValue(key, out var row))
                    rows.Add(key, row = new MarkerAggregate());
                if (!intervalsByMarker.TryGetValue(row, out var intervals))
                {
                    intervalsByMarker.Add(row, intervals = new List<(double start, double end)>());
                    row.Frames++;
                }
                var duration = data.GetSampleTimeMs(sample);
                var self = SampleSelfTime(data, sample);
                row.Samples++;
                row.TotalMs += duration;
                row.SelfMs += self;
                if (self > row.PeakSelfMs)
                {
                    row.PeakSelfMs = self;
                    row.PeakFrame = data.frameIndex;
                    row.PeakThread = data.threadIndex;
                    row.PeakSample = sample;
                }
                if (duration > 0)
                {
                    var start = data.GetSampleStartTimeMs(sample);
                    intervals.Add((start, start + duration));
                }
            }
            foreach (var entry in intervalsByMarker)
                entry.Key.CoveredMs += IntervalCoverage(entry.Value);
        }

        static double IntervalCoverage(List<(double start, double end)> intervals)
        {
            intervals.Sort((left, right) => left.start.CompareTo(right.start));
            var lastEnd = double.NegativeInfinity;
            var covered = 0.0;
            foreach (var (start, end) in intervals)
            {
                if (end <= lastEnd)
                    continue;
                covered += end - Math.Max(start, lastEnd);
                lastEnd = end;
            }
            return covered;
        }

        [CliCommand("profiler_gc_callstacks", "Read recorded GC.Alloc call stacks for one frame and thread.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object GcCallstacks(
            [CliArg("frame", "Profiler frame index.", Required = true)] int? frameIndex,
            [CliArg("file", "Capture filename under Library/Pipeline/Profiles.")] string file = null,
            [CliArg("thread", "Profiler thread index.")] int thread = 0,
            [CliArg("limit", "Maximum allocation stacks to return (1–50).")] int limit = 20,
            [CliArg("confirm", "Allow replacing existing Profiler history.")] bool confirm = false,
            [CliArg("session_id", "Analysis session ID instead of a file.")] string sessionId = null)
        {
            if (thread < 0 || limit < 1 || limit > 50)
                throw new ArgumentException("Use 1–50 stacks and a non-negative thread index.");
            var frame = frameIndex ?? throw new ArgumentException("Provide a frame index.");
            SelectCapture(file, sessionId, confirm);
            ValidateFrame(frame);
            using (var data = ProfilerDriver.GetRawFrameDataView(frame, thread))
            {
                if (!data.valid)
                    throw new ArgumentException("The requested thread is not present in this frame.", nameof(thread));
                var samples = new List<object>();
                var addresses = new List<ulong>();
                var allocations = 0;
                var withCallstacks = 0;
                for (var sample = 0; sample < data.sampleCount; sample++)
                {
                    if (data.GetSampleName(sample) != "GC.Alloc")
                        continue;
                    allocations++;
                    addresses.Clear();
                    data.GetSampleCallstack(sample, addresses);
                    if (addresses.Count == 0)
                        continue;
                    withCallstacks++;
                    if (samples.Count < limit)
                    {
                        var stack = addresses.Select(address =>
                        {
                            var info = data.ResolveMethodInfo(address);
                            return new { address = $"0x{address:x}", method = string.IsNullOrEmpty(info.methodName) ? null : info.methodName };
                        }).ToArray();
                        samples.Add(new { sampleIndex = sample, bytes = GcSampleBytes(data, sample), stack });
                    }
                }
                return new { frame, threadIndex = thread, thread = data.threadName, allocations, withCallstacks,
                    truncated = withCallstacks > limit, samples };
            }
        }

        static float SampleSelfTime(UnityEditor.Profiling.RawFrameDataView data, int sample)
        {
            var self = data.GetSampleTimeMs(sample);
            var child = sample + 1;
            var children = data.GetSampleChildrenCount(sample);
            for (var n = 0; n < children && child < data.sampleCount; n++)
            {
                self -= data.GetSampleTimeMs(child);
                child += 1 + data.GetSampleChildrenCountRecursive(child);
            }
            return Math.Max(0, self);
        }

        static long? ReadGcBytes(UnityEditor.Profiling.RawFrameDataView data, bool requireCounterSample = true)
        {
            var marker = data.GetMarkerId("GC Allocated In Frame");
            return marker >= 0 && (!requireCounterSample || data.HasCounterValue(marker))
                ? Math.Max(0, data.GetCounterValueAsLong(marker)) : (long?)null;
        }

        static long? GcSampleBytes(UnityEditor.Profiling.RawFrameDataView data, int sample)
        {
            return data.GetSampleMetadataCount(sample) > 0 ? data.GetSampleMetadataAsLong(sample, 0) : (long?)null;
        }

        static long? SumGcSampleBytes(int frame, UnityEditor.Profiling.RawFrameDataView mainThread)
        {
            var marker = mainThread.GetMarkerId("GC.Alloc");
            if (marker < 0)
                return ReadGcBytes(mainThread);
            long total = 0;
            for (var thread = 0; ; thread++)
            {
                using (var data = ProfilerDriver.GetRawFrameDataView(frame, thread))
                {
                    if (!data.valid)
                        break;
                    for (var sample = 0; sample < data.sampleCount; sample++)
                    {
                        if (data.GetSampleMarkerId(sample) != marker)
                            continue;
                        var bytes = GcSampleBytes(data, sample);
                        if (!bytes.HasValue)
                            return null;
                        total += bytes.Value;
                    }
                }
            }
            return total;
        }

        [CliCommand("profiler_summary", "Summarize CPU frame durations, GC allocations and frame budget over an inclusive frame range.", Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object Summarize(
            [CliArg("session_id", "Analysis session ID.", Required = true)] string sessionId,
            [CliArg("first_frame", "First frame in the range. Defaults to the first captured frame.")] int? firstFrame = null,
            [CliArg("last_frame", "Last frame in the range. Defaults to the last captured frame.")] int? lastFrame = null,
            [CliArg("frame_budget_ms", "Positive frame budget in milliseconds. Defaults to 60 FPS.")] double frameBudgetMs = 1000.0 / 60,
            [CliArg("top", "Number of slow frames to return (1–50).")] int top = 5,
            [CliArg("confirm", "Allow switching captures and replacing existing Profiler history.")] bool confirm = false)
        {
            if (frameBudgetMs <= 0 || double.IsNaN(frameBudgetMs) || double.IsInfinity(frameBudgetMs))
                throw new ArgumentException("Use a finite, positive frame budget.", nameof(frameBudgetMs));
            if (top < 1 || top > 50)
                throw new ArgumentOutOfRangeException(nameof(top), "Use 1–50 slow frames.");
            EnsureResident(GetSession(sessionId), confirm);
            var first = firstFrame ?? ProfilerDriver.firstFrameIndex;
            var last = lastFrame ?? ProfilerDriver.lastFrameIndex;
            ValidateFrame(first);
            ValidateFrame(last);
            if (last < first)
                throw new ArgumentException("The last frame must be at or after the first frame.");

            var rows = new List<(int frame, double ms, long? gcBytes)>();
            for (var frame = first; frame <= last; frame++)
            {
                using (var data = ProfilerDriver.GetRawFrameDataView(frame, 0))
                {
                    if (data.valid)
                        rows.Add((frame, data.frameTimeMs, SumGcSampleBytes(frame, data)));
                }
            }
            if (rows.Count == 0)
                throw new InvalidOperationException("No valid frames in the requested range.");

            var ranked = rows.OrderByDescending(row => row.ms).ThenBy(row => row.frame).ToArray();
            var cpu = rows.Select(row => row.ms).OrderBy(value => value).ToArray();
            var gc = rows.Where(row => row.gcBytes.HasValue).Select(row => row.gcBytes.Value).OrderBy(value => value).ToArray();
            var overBudget = rows.Count(row => row.ms > frameBudgetMs);
            return new
            {
                sessionId, firstFrame = first, lastFrame = last,
                validFrames = rows.Count, skippedFrames = last - first + 1 - rows.Count, gcFrames = gc.Length,
                cpuMs = new { median = Percentile(cpu, 0.5), p95 = Percentile(cpu, 0.95), max = cpu[cpu.Length - 1] },
                gcBytes = gc.Length == 0 ? null : new { median = Percentile(gc, 0.5), p95 = Percentile(gc, 0.95), total = gc.Sum() },
                budget = new { frameBudgetMs, framesOverBudget = overBudget, percentOverBudget = 100.0 * overBudget / rows.Count },
                slowFrames = ranked.Take(top).Select(row => new { row.frame, row.ms, row.gcBytes, overBudget = row.ms > frameBudgetMs }).ToArray()
            };
        }

        sealed class CounterSeries
        {
            public readonly Dictionary<int, IComparable> Samples = new Dictionary<int, IComparable>();

            public object Statistics()
            {
                var sorted = Samples.Values.OrderBy(value => value).ToArray();
                if (sorted.Length == 0)
                    return null;

                return new
                {
                    min = sorted[0], p25 = Percentile(sorted, 0.25), median = Percentile(sorted, 0.5),
                    p75 = Percentile(sorted, 0.75), p95 = Percentile(sorted, 0.95), max = sorted[sorted.Length - 1],
                    mean = sorted.Sum(value => Convert.ToDouble(value, CultureInfo.InvariantCulture) / sorted.Length)
                };
            }
        }

        [CliCommand("profiler_counters", "List recorded non-GPU counters or read their statistics and optional frame values.",
            Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object ReadCounters(
            [CliArg("session_id", "Analysis session ID.", Required = true)] string sessionId,
            [CliArg("counters", "Exact counter names or Category/Name selectors. Omit to list the recorded counters.")] string[] counters = null,
            [CliArg("first_frame", "First frame in the range. Defaults to the first captured frame.")] int? firstFrame = null,
            [CliArg("last_frame", "Last frame in the range. Defaults to the last captured frame.")] int? lastFrame = null,
            [CliArg("per_frame", "Include the raw value and availability on each selected frame.")] bool perFrame = false,
            [CliArg("confirm", "Allow switching captures and replacing existing Profiler history.")] bool confirm = false)
        {
            var selectors = (counters ?? Array.Empty<string>()).Distinct(StringComparer.Ordinal).ToArray();
            if (selectors.Any(string.IsNullOrWhiteSpace))
                throw new ArgumentException("Use non-empty counter names or Category/Name selectors.", nameof(counters));

            EnsureResident(GetSession(sessionId), confirm);
            var first = firstFrame ?? ProfilerDriver.firstFrameIndex;
            var last = lastFrame ?? ProfilerDriver.lastFrameIndex;
            ValidateFrame(first);
            ValidateFrame(last);
            if (last < first || last - first >= 300)
                throw new ArgumentException("Select an inclusive range of 1–300 frames.");

            var requested = new HashSet<string>(selectors, StringComparer.Ordinal);
            var matched = new HashSet<string>(StringComparer.Ordinal);
            var series = new Dictionary<(string category, string name, ProfilerMarkerDataType type, ProfilerMarkerDataUnit unit), CounterSeries>();
            var markers = new List<FrameDataView.MarkerInfo>();
            for (var frame = first; frame <= last; frame++)
            {
                using (var data = ProfilerDriver.GetRawFrameDataView(frame, 0))
                {
                    if (!data.valid)
                        continue;

                    markers.Clear();
                    data.GetMarkers(markers);
                    foreach (var marker in markers)
                    {
                        if ((marker.flags & MarkerFlags.Counter) == 0)
                            continue;

                        var category = data.GetCategoryInfo(marker.category).name;
                        if ((marker.flags & MarkerFlags.SampleGPU) != 0 ||
                            category?.IndexOf("GPU", StringComparison.OrdinalIgnoreCase) >= 0 ||
                            marker.name.IndexOf("GPU", StringComparison.OrdinalIgnoreCase) >= 0)
                            continue;

                        var qualified = $"{category}/{marker.name}";
                        var nameSelected = requested.Contains(marker.name);
                        var categorySelected = requested.Contains(qualified);
                        if (selectors.Length != 0 && !nameSelected && !categorySelected)
                            continue;

                        var metadata = marker.metadataInfo;
                        if (metadata == null || metadata.Length != 1)
                            continue;

                        var hasSample = data.HasCounterValue(marker.id);
                        if (selectors.Length == 0 && !hasSample)
                            continue;
                        if (nameSelected)
                            matched.Add(marker.name);
                        if (categorySelected)
                            matched.Add(qualified);

                        var key = (category, name: marker.name, type: metadata[0].type, unit: metadata[0].unit);
                        if (!series.TryGetValue(key, out var counter))
                            series.Add(key, counter = new CounterSeries());
                        var value = hasSample ? ReadCounterValue(data, marker.id, key.type) : null;
                        if (value != null)
                            counter.Samples[frame] = value;
                    }
                }
            }

            var rows = series.OrderBy(entry => entry.Key.category, StringComparer.Ordinal)
                .ThenBy(entry => entry.Key.name, StringComparer.Ordinal).ThenBy(entry => entry.Key.type)
                .ThenBy(entry => entry.Key.unit).Select(entry => new
                {
                    entry.Key.name, entry.Key.category, dataType = entry.Key.type.ToString(),
                    unit = entry.Key.unit.ToString(), availableFrames = entry.Value.Samples.Count,
                    stats = selectors.Length == 0 ? null : entry.Value.Statistics(),
                    values = perFrame ? Enumerable.Range(first, last - first + 1).Select(frame =>
                    {
                        var available = entry.Value.Samples.TryGetValue(frame, out var value);
                        return new { frame, available, value };
                    }).ToArray() : null
                }).ToArray();
            return new { sessionId, firstFrame = first, lastFrame = last, counters = rows, unknown = selectors.Where(name => !matched.Contains(name)).ToArray() };
        }

        static IComparable ReadCounterValue(RawFrameDataView data, int marker, ProfilerMarkerDataType type)
        {
            switch (type)
            {
                case ProfilerMarkerDataType.Int32:
                    return data.GetCounterValueAsInt(marker);
                case ProfilerMarkerDataType.UInt32:
                    return (uint)data.GetCounterValueAsLong(marker);
                case ProfilerMarkerDataType.Int64:
                    return data.GetCounterValueAsLong(marker);
                case ProfilerMarkerDataType.UInt64:
                    return unchecked((ulong)data.GetCounterValueAsLong(marker));
                case ProfilerMarkerDataType.Float:
                    var single = data.GetCounterValueAsFloat(marker);
                    return float.IsNaN(single) || float.IsInfinity(single) ? null : (IComparable)single;
                case ProfilerMarkerDataType.Double:
                    var number = data.GetCounterValueAsDouble(marker);
                    return double.IsNaN(number) || double.IsInfinity(number) ? null : (IComparable)number;
                default:
                    return null;
            }
        }

        [CliCommand("profiler_marker_info", "Read the native category, flags and payload schema of an offline capture marker.",
            Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object ReadMarkerInfo(
            [CliArg("session_id", "Analysis session ID.", Required = true)] string sessionId,
            [CliArg("marker", "Exact marker name.", Required = true)] string marker,
            [CliArg("frame", "Captured frame. Defaults to the first frame.")] int? frame = null,
            [CliArg("confirm", "Allow switching captures and replacing existing Profiler history.")] bool confirm = false)
        {
            if (string.IsNullOrWhiteSpace(marker))
                throw new ArgumentException("Use an exact marker name.", nameof(marker));
            EnsureResident(GetSession(sessionId), confirm);
            var frameIndex = frame ?? ProfilerDriver.firstFrameIndex;
            ValidateFrame(frameIndex);
            using (var data = ProfilerDriver.GetRawFrameDataView(frameIndex, 0))
            {
                var markers = new List<FrameDataView.MarkerInfo>();
                if (data.valid)
                    data.GetMarkers(markers);

                var definitions = markers.Where(item => item.name == marker).Select(item => new
                {
                    id = item.id, category = data.GetCategoryInfo(item.category).name,
                    flags = (uint)item.flags, isCounter = (item.flags & MarkerFlags.Counter) != 0,
                    metadata = (item.metadataInfo ?? Array.Empty<FrameDataView.MarkerMetadataInfo>()).Select(field =>
                        new { field.name, dataType = field.type.ToString(), unit = field.unit.ToString() }).ToArray()
                }).OrderBy(item => item.category, StringComparer.Ordinal).ThenBy(item => item.id).ToArray();
                return new { sessionId, frame = frameIndex, marker, available = definitions.Length != 0, definitions };
            }
        }

        static T Percentile<T>(T[] ordered, double fraction) => ordered[(int)Math.Floor((ordered.Length - 1) * fraction)];

        static void RequireProfilerIdle()
        {
            RequireIdle();
            if (ProfilerDriver.enabled)
                throw new InvalidOperationException("Stop the active Profiler recording before loading a capture.");
        }

        static void LoadCapture(string file, bool confirm)
        {
            RequireProfilerIdle();
            if (ProfilerDriver.lastFrameIndex >= 0 && !confirm)
                throw new InvalidOperationException("Pass confirm=true to replace existing Profiler history.");
            var path = ProfilePath(file, ".data");
            if (!File.Exists(path))
                throw new IOException($"Unity could not open '{file}' as a Profiler capture.");
            s_Resident = null;
            if (!ProfilerDriver.LoadProfile(path, false))
                throw new IOException($"Unity could not open '{file}' as a Profiler capture.");
        }

        static void ValidateFrame(int frame)
        {
            if (frame < ProfilerDriver.firstFrameIndex || frame > ProfilerDriver.lastFrameIndex)
                throw new ArgumentException("The requested frame is outside the capture.", nameof(frame));
        }

        sealed class MarkerTotals
        {
            public int Calls;
            public double TotalMs;
            public double SelfMs;
        }

        [CliCommand("memory_snapshot", "Take a .snap memory snapshot of the Editor process. Poll memory_snapshot_status.", Tags = new[] { "observability/profiling" })]
        public static object Snapshot(
            [CliArg("file", "File name ending in .snap under Library/Pipeline/Profiles.", Required = true)] string file)
        {
            RequireIdle();
            var path = ProfilePath(file, ".snap");
            if (File.Exists(path))
                throw new IOException("The snapshot file already exists. Choose a new file name.");
            Directory.CreateDirectory(Path.GetDirectoryName(path));
            s_SnapshotRunning = true;
            s_SnapshotResult = new { status = "capturing", path };
            try
            {
                Unity.Profiling.Memory.MemoryProfiler.TakeSnapshot(path, (written, success) =>
                {
                    s_SnapshotRunning = false;
                    if (success && File.Exists(written))
                        s_SnapshotResult = new { status = "completed", path = written, bytes = new FileInfo(written).Length };
                    else
                        s_SnapshotResult = new { status = "failed", path, message = "Unity did not write the memory snapshot." };
                }, Unity.Profiling.Memory.CaptureFlags.ManagedObjects | Unity.Profiling.Memory.CaptureFlags.NativeObjects);
            }
            catch
            {
                s_SnapshotRunning = false;
                s_SnapshotResult = new { status = "failed", path };
                throw;
            }
            return s_SnapshotResult;
        }

        [CliCommand("memory_snapshot_status", "Read memory snapshot progress or its completed path.", MainThreadRequired = false, Tags = new[] { "observability/profiling" })]
        public static object SnapshotStatus() => System.Threading.Volatile.Read(ref s_SnapshotResult);

        static void RequireIdle()
        {
            if (s_Recording != null || s_SnapshotRunning)
                throw new InvalidOperationException("A Pipeline Profiler capture or memory snapshot is already active.");
        }

        static string ProfilePath(string file, string extension)
        {
            if (string.IsNullOrWhiteSpace(file) || file != Path.GetFileName(file) ||
                file.IndexOfAny(Path.GetInvalidFileNameChars()) >= 0 ||
                !string.Equals(Path.GetExtension(file), extension, StringComparison.OrdinalIgnoreCase))
                throw new ArgumentException($"Provide a file name ending in '{extension}' without a directory.");
            return Path.Combine(Path.GetDirectoryName(Application.dataPath), "Library", "Pipeline", "Profiles", file);
        }
    }
}
#endif
