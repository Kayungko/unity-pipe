using System;
using System.Collections.Generic;
using System.IO;
using System.Threading;
using Unity.Pipeline;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Runtime.Telemetry;
using UnityEngine;
using UnityEngine.Profiling;
using Unity.Profiling.Memory;

namespace Unity.Pipeline.Runtime.Commands
{
    static class PlayerMemorySnapshotCommands
    {
        static object s_Result = new { status = "idle" };
        static bool s_Running;

        [CliCommand("player_memory_snapshot", "Capture a .snap of this local development Player. Poll player_memory_snapshot_status.", RuntimeOnly = true, Effect = CommandEffect.Destructive, Tags = new[] { "observability/profiling" })]
        public static object Snapshot(
            [CliArg("file", "File name ending in .snap in the Player's persistent Pipeline/Profiles folder.", Required = true)] string file,
            [CliArg("confirm", "Allow capturing this Player's object and memory data.")] bool confirm = false)
        {
            if (!Debug.isDebugBuild || Application.isEditor || !confirm)
                throw new InvalidOperationException("This command requires a development Player and confirm=true.");
            if (s_Running)
                throw new InvalidOperationException("A Player memory snapshot is already running.");
            if (string.IsNullOrWhiteSpace(file) || file != Path.GetFileName(file) ||
                file.IndexOfAny(Path.GetInvalidFileNameChars()) >= 0 ||
                !string.Equals(Path.GetExtension(file), ".snap", StringComparison.OrdinalIgnoreCase))
                throw new ArgumentException("Provide a file name ending in '.snap' without a directory.", nameof(file));

            var folder = Path.Combine(Application.persistentDataPath, "Pipeline", "Profiles");
            var path = Path.Combine(folder, file);
            if (File.Exists(path))
                throw new IOException("The snapshot file already exists. Choose a new file name.");
            Directory.CreateDirectory(folder);
            var staging = Path.Combine(folder, $".{Guid.NewGuid():N}.snap");
            var buildGuid = Application.buildGUID;
            s_Running = true;
            s_Result = new { status = "capturing", path, target = "player", buildGuid };
            try
            {
                MemoryProfiler.TakeSnapshot(staging, (_, success) =>
                {
                    try
                    {
                        var bytes = success && File.Exists(staging) ? new FileInfo(staging).Length : 0;
                        if (bytes == 0)
                            throw new IOException("Unity did not write the Player memory snapshot.");
                        File.Move(staging, path);
                        s_Result = new { status = "completed", path, bytes, target = "player", buildGuid };
                    }
                    catch (Exception ex)
                    {
                        s_Result = new { status = "failed", path, message = ex.Message, target = "player", buildGuid };
                    }
                    finally
                    {
                        s_Running = false;
                        try { File.Delete(staging); }
                        catch (IOException) { }
                        catch (UnauthorizedAccessException) { }
                    }
                }, CaptureFlags.ManagedObjects | CaptureFlags.NativeObjects);
            }
            catch
            {
                s_Running = false;
                s_Result = new { status = "failed", path, target = "player", buildGuid };
                throw;
            }
            return s_Result;
        }

        [CliCommand("player_memory_snapshot_status", "Read the latest Player memory snapshot status.", MainThreadRequired = false, RuntimeOnly = true, Effect = CommandEffect.ReadOnly, Tags = new[] { "observability/profiling" })]
        public static object Status() => Volatile.Read(ref s_Result);
    }

    /// <summary>
    /// Runtime status information for Unity Player builds.
    /// Provides Player-appropriate status without Editor-specific APIs.
    /// </summary>
    static class RuntimeStatusCommand
    {
        [CliCommand("runtime_status", "Get comprehensive runtime application status", MainThreadRequired = true, RuntimeOnly = true, Tags = new[] { "runtime" })]
        public static RuntimeStatusResponse GetRuntimeStatus()
        {
            try
            {
                return new RuntimeStatusResponse
                {
                    // Application info
                    UnityVersion = Application.unityVersion,
                    Platform = Application.platform.ToString(),
                    BuildGuid = Application.buildGUID,
                    Version = Application.version,

                    // Runtime state
                    IsPlaying = true, // Always true in Player builds
                    TargetFrameRate = Application.targetFrameRate,
                    TimeScale = Time.timeScale,
                    RealTimeSinceStartup = Time.realtimeSinceStartup,

                    // Performance info
                    FrameCount = Time.frameCount,
                    LoadedLevelName = UnityEngine.SceneManagement.SceneManager.GetActiveScene().name,
                    LoadedLevelBuildIndex = UnityEngine.SceneManagement.SceneManager.GetActiveScene().buildIndex,

                    // Live performance telemetry (fps, frame time, memory, profiler counters)
                    Performance = BuildPerformanceTelemetry(),

                    // The driver's actual state and in-effect config — not just what Project
                    // Settings currently says, which can differ (live JSON in the Editor vs. the
                    // snapshot frozen into the Player at build time).
                    PipelineDriver = BuildDriverStatus(),

                    // Timestamp
                    Timestamp = DateTime.UtcNow
                };
            }
            catch (Exception ex)
            {
                Debug.LogError($"Pipeline: Runtime status command failed: {ex.Message}");
                throw;
            }
        }

        /// <summary>
        /// Null when no driver was ever bootstrapped (e.g. enableInBuilds was false), otherwise the
        /// driver's actual running state and the config values presently governing it.
        /// maxWorkItemsPerFrame is read via CurrentMaxWorkItemsPerFrame rather than the config
        /// directly, since that field alone can be tuned live after the driver started.
        /// </summary>
        private static RuntimePipelineDriverStatus BuildDriverStatus()
        {
            var driver = RuntimePipelineBootstrap.Instance;
            if (driver == null)
                return null;

            var config = driver.Config;
            return new RuntimePipelineDriverStatus
            {
                IsServerRunning = driver.IsServerRunning,
                ActualPort = driver.ActualPort,
                EnableInBuilds = config != null && config.enableInBuilds,
                RequestTimeoutMs = config != null ? config.requestTimeoutMs : 0,
                EnableAuditLogging = config != null && config.enableAuditLogging,
                AutoStart = config != null && config.autoStart,
                MaxWorkItemsPerFrame = driver.CurrentMaxWorkItemsPerFrame
            };
        }

        /// <summary>
        /// Assemble the live performance section from two independent sources:
        /// the per-frame <see cref="FrameStatsSampler"/> (fps / frame time / profiler counters), which only
        /// exists while a <see cref="RuntimePipelineDriver"/> is feeding it, and the Profiler memory APIs,
        /// which can be read on demand at any time. Frame-stat fields stay at their defaults (and
        /// <see cref="RuntimePerformanceTelemetry.FrameStatsAvailable"/> stays false) when no sampler is
        /// running — e.g. in EditMode tests — so a missing sampler degrades gracefully rather than throwing.
        /// </summary>
        private static RuntimePerformanceTelemetry BuildPerformanceTelemetry()
        {
            var perf = new RuntimePerformanceTelemetry();

            var sampler = FrameStatsSampler.Shared;
            if (sampler != null)
            {
                var snap = sampler.GetSnapshot();
                perf.FrameStatsAvailable = snap.Available;
                perf.SampleWindow = snap.SampleWindow;
                perf.Fps = snap.Fps;
                perf.AverageFrameTimeMs = snap.AverageFrameTimeMs;
                perf.MinFrameTimeMs = snap.MinFrameTimeMs;
                perf.MaxFrameTimeMs = snap.MaxFrameTimeMs;
                perf.LastFrameTimeMs = snap.LastFrameTimeMs;
                perf.GpuTimingAvailable = snap.GpuTimingAvailable;
                perf.CpuFrameTimeMs = snap.CpuFrameTimeMs;
                perf.GpuFrameTimeMs = snap.GpuFrameTimeMs;
                perf.Counters = snap.Counters;
            }
            else
            {
                perf.Counters = new Dictionary<string, double>();
            }

            // Memory is sampled directly (no per-frame accumulation needed). These return 0 in a
            // non-development build where the profiler is stripped, which is reported faithfully.
            perf.TotalAllocatedMemory = Profiler.GetTotalAllocatedMemoryLong();
            perf.TotalReservedMemory = Profiler.GetTotalReservedMemoryLong();
            perf.MonoUsedSize = Profiler.GetMonoUsedSizeLong();
            perf.MonoHeapSize = Profiler.GetMonoHeapSizeLong();
            perf.GcMemory = GC.GetTotalMemory(false);

            return perf;
        }
    }

    /// <summary>
    /// Response model for runtime status information.
    /// </summary>
    [Serializable]
    class RuntimeStatusResponse
    {
        // Application Information
        public string UnityVersion { get; set; }
        public string Platform { get; set; }
        public string BuildGuid { get; set; }
        public string Version { get; set; }
        
        // Runtime State
        public bool IsPlaying { get; set; } // Always true for Player builds
        public int TargetFrameRate { get; set; }
        public float TimeScale { get; set; }
        public float RealTimeSinceStartup { get; set; }

        // Performance Information
        public int FrameCount { get; set; }
        public string LoadedLevelName { get; set; }
        public int LoadedLevelBuildIndex { get; set; }

        // Live performance telemetry
        public RuntimePerformanceTelemetry Performance { get; set; }

        // The runtime Pipeline driver's actual state, or null if none was ever bootstrapped.
        public RuntimePipelineDriverStatus PipelineDriver { get; set; }

        // Metadata
        public DateTime Timestamp { get; set; }
    }

    /// <summary>
    /// The runtime Pipeline driver's actual state: whether its server is running, the port it's
    /// actually on, and the config values presently governing it. Distinct from what Project
    /// Settings > Pipeline > Runtime currently shows — that's live-editable in the Editor, while a
    /// running driver is working from whatever it was initialized with (a Player's is additionally
    /// frozen at build time). This is the authoritative "what's actually happening right now" view.
    /// </summary>
    [Serializable]
    class RuntimePipelineDriverStatus
    {
        public bool IsServerRunning { get; set; }
        public int ActualPort { get; set; }
        public bool EnableInBuilds { get; set; }
        public int RequestTimeoutMs { get; set; }
        public bool EnableAuditLogging { get; set; }
        public bool AutoStart { get; set; }

        /// <summary>The value actually governing the dispatcher right now — see RuntimePipelineDriver.CurrentMaxWorkItemsPerFrame.</summary>
        public int MaxWorkItemsPerFrame { get; set; }
    }

    /// <summary>
    /// Live performance telemetry for a running Player: rolling fps / frame-time statistics, optional
    /// CPU/GPU frame timing, memory usage, and a map of profiler counters. Frame-stat and counter fields
    /// are populated from the per-frame <see cref="FrameStatsSampler"/>; memory fields are read on demand.
    /// </summary>
    [Serializable]
    class RuntimePerformanceTelemetry
    {
        // Frame timing — sampled over a rolling window by the RuntimePipelineDriver.
        /// <summary>False when no sampler is running; the fps / frame-time fields below are then meaningless.</summary>
        public bool FrameStatsAvailable { get; set; }
        public int SampleWindow { get; set; }
        public float Fps { get; set; }
        public float AverageFrameTimeMs { get; set; }
        public float MinFrameTimeMs { get; set; }
        public float MaxFrameTimeMs { get; set; }
        public float LastFrameTimeMs { get; set; }

        // CPU/GPU frame timing via FrameTimingManager (not available on every platform).
        public bool GpuTimingAvailable { get; set; }
        public double CpuFrameTimeMs { get; set; }
        public double GpuFrameTimeMs { get; set; }

        // Memory (bytes) via the Profiler API; 0 when the profiler is stripped (non-development build).
        public long TotalAllocatedMemory { get; set; }
        public long TotalReservedMemory { get; set; }
        public long MonoUsedSize { get; set; }
        public long MonoHeapSize { get; set; }
        public long GcMemory { get; set; }

        // Profiler counters by reported name (e.g. DrawCalls, Triangles, GcAllocInFrame).
        public Dictionary<string, double> Counters { get; set; }
    }
}
