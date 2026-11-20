#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.Reflection;
using Unity.Pipeline.Models;
using UnityEngine;
using System.Globalization;
using UnityEditor;
using Unity.Pipeline.Console;

namespace Unity.Pipeline.Editor.Console
{
    /// <summary>
    /// Editor-only bootstrap for <see cref="ConsoleLogCapture"/>. The capture itself (the shared
    /// buffer and the log-callback subscription) lives in the runtime assembly so it also works in
    /// player builds; this type adds the things that only make sense in the Editor:
    ///
    ///  - <c>[InitializeOnLoad]</c> starts capture on every editor load and after every domain reload.
    ///  - Persistence to a Temp file across domain reloads, so entries and the cursor survive a
    ///    <c>recompile</c>. Players have no domain reloads, so the runtime path skips persistence.
    ///  - Re-subscribing on every play-mode transition: Unity clears
    ///    <see cref="UnityEngine.Application.logMessageReceivedThreaded"/>'s subscribers on play-mode
    ///    exit even though exiting play mode does not reload the domain, so capture would otherwise go
    ///    dead until the next recompile.
    ///  - A session identifier and a sequence floor in <see cref="SessionState"/>, so a client can
    ///    tell a cursor this editor session issued from one an earlier session did.
    ///
    /// The static constructor restores the persisted buffer first, then starts capture, so logs are
    /// never appended ahead of a restore.
    /// </summary>
    [InitializeOnLoad]
    public static class EditorConsoleCaptureBootstrap
    {
        // Lives under Temp/ like the recompile status file: cleared by Unity on project-level cleanup,
        // survives domain reloads, and never committed.
        internal const string PersistencePath = "Temp/pipeline_console_log.json";

        // SessionState survives domain reloads and is cleared when the Editor restarts, which is
        // exactly the lifetime of the Temp file the buffer persists to. Both therefore start over
        // together, and the session identifier is what tells a client that happened.
        internal const string SessionKey = "pipeline.console.session";
        internal const string SeqFloorKey = "pipeline.console.seq";

        static EditorConsoleCaptureBootstrap()
        {
            var session = SessionState.GetString(SessionKey, string.Empty);
            if (string.IsNullOrEmpty(session))
            {
                session = System.Guid.NewGuid().ToString("N");
                SessionState.SetString(SessionKey, session);
            }

            ConsoleLogCapture.Buffer.SetSession(session);

            // Restore before capture starts so restored entries precede any newly captured ones, and
            // load before raising the floor: the floor is the same value the snapshot recorded, so
            // raising it first would make every snapshot entry look already-issued and drop it.
            ConsoleLogCapture.Buffer.Load(PersistencePath);
            if (long.TryParse(SessionState.GetString(SeqFloorKey, "0"), NumberStyles.Integer, CultureInfo.InvariantCulture, out var floor))
                ConsoleLogCapture.Buffer.RaiseLastSeq(floor);

            ConsoleLogCapture.EnsureCapturing();

            AssemblyReloadEvents.beforeAssemblyReload -= Persist;
            AssemblyReloadEvents.beforeAssemblyReload += Persist;

            EditorApplication.quitting -= Persist;
            EditorApplication.quitting += Persist;

            EditorApplication.playModeStateChanged -= OnPlayModeStateChanged;
            EditorApplication.playModeStateChanged += OnPlayModeStateChanged;
        }

        static void Persist()
        {
            ConsoleLogCapture.Buffer.Save(PersistencePath);
            SessionState.SetString(SeqFloorKey, ConsoleLogCapture.Buffer.LastSeq.ToString(CultureInfo.InvariantCulture));
        }

        static void OnPlayModeStateChanged(PlayModeStateChange state) => ConsoleLogCapture.EnsureCapturing();
    }

    /// <summary>
    /// Samples what the Editor knows about the console — the compile-failure flag and the console's
    /// own entry counts — and publishes it for the <c>console</c> and <c>recompile_status</c>
    /// commands. Those commands are deliberately not main-thread commands, because clients poll
    /// them while the Editor is blocked compiling or importing, so they cannot read this state
    /// themselves; sampling it here and handing over an immutable snapshot is what lets them report
    /// it at all.
    ///
    /// The same pass does two more jobs that need the same main-thread tick:
    ///  - Backfills the buffer from the console's own store once per session, so sticky compile
    ///    errors logged before capture started are visible (see <see cref="EditorConsoleEntries"/>).
    ///  - Watches for the console gaining entries while the buffer's cursor stands still, which is
    ///    the only observable symptom of the log-callback subscription having been dropped, and
    ///    re-arms capture when it sees it.
    ///
    /// Sampling runs on the Editor update loop even when the Console window is closed.
    /// </summary>
    [InitializeOnLoad]
    static class EditorConsoleGroundTruth
    {
        const string SeededKey = "pipeline.console.seeded";
        const double SampleIntervalSeconds = 0.25;
        const double ReseedCooldownSeconds = 5.0;

        static double s_NextSampleTime;
        static double s_NextReseedTime;
        static int s_LastConsoleTotal = -1;
        static long s_LastBufferSeq = -1;
        static bool s_GapPending;
        static long s_GapSeq;
        static bool s_Seeded;

        static EditorConsoleGroundTruth()
        {
            s_Seeded = SessionState.GetBool(SeededKey, false);

            EditorApplication.update -= Sample;
            EditorApplication.update += Sample;

            AssemblyReloadEvents.afterAssemblyReload -= OnAfterAssemblyReload;
            AssemblyReloadEvents.afterAssemblyReload += OnAfterAssemblyReload;
        }

        static void Sample()
        {
            if (EditorApplication.timeSinceStartup < s_NextSampleTime)
                return;
            s_NextSampleTime = EditorApplication.timeSinceStartup + SampleIntervalSeconds;

            // Seed before publishing, so the first snapshot a client sees already reports whether
            // the backfill happened.
            if (!s_Seeded)
                TrySeed();

            EditorConsoleEntries.ReadCounts(out var errors, out var warnings, out var logs);
            var total = errors + warnings + logs;
            var seq = ConsoleLogCapture.Buffer.LastSeq;

            // The console gained entries while the buffer's cursor stood still. Unity unregisters
            // the native log callback on domain unload and on play-mode exit, while the managed
            // event keeps its subscriber list, so capture can be dead with nothing to test directly.
            //
            // A gap is confirmed by the cursor still sitting where it was one sample later, not by
            // the console count growing again: a dead subscription that missed a single burst — a
            // failed compile, say — would otherwise be spotted once and then forgotten. Waiting for
            // the second sample keeps a console entry that legitimately never reaches the managed
            // callback from re-arming on its own. A Clear lowers the counts, so it can never be
            // mistaken for a gap.
            if (s_GapPending && seq == s_GapSeq)
            {
                if (EditorApplication.timeSinceStartup >= s_NextReseedTime)
                {
                    ConsoleLogCapture.EnsureCapturing();
                    Reseed();
                    seq = ConsoleLogCapture.Buffer.LastSeq;
                    s_NextReseedTime = EditorApplication.timeSinceStartup + ReseedCooldownSeconds;
                    s_GapPending = false;
                }
            }
            else if (s_LastConsoleTotal >= 0 && total > s_LastConsoleTotal && seq == s_LastBufferSeq)
            {
                s_GapSeq = seq;
                s_GapPending = true;
            }
            else
            {
                // The buffer caught up on its own.
                s_GapPending = false;
            }

            s_LastConsoleTotal = total;
            s_LastBufferSeq = seq;

            ConsoleLogCapture.GroundTruth = new ConsoleGroundTruth
            {
                SampledUtc = DateTime.UtcNow,
                CompilationFailed = EditorUtility.scriptCompilationFailed,
                Compiling = EditorApplication.isCompiling,
                ConsoleErrors = errors,
                ConsoleWarnings = warnings,
                ConsoleLogs = logs,
                Seeded = s_Seeded
            };
        }

        static void OnAfterAssemblyReload()
        {
            ConsoleLogCapture.EnsureCapturing();

            // The snapshot describes the domain that just went away, and on 6000.5+ it survives the
            // reload with the rest of the statics. Drop it so nothing reads it as current until the
            // next sample replaces it.
            ConsoleLogCapture.GroundTruth = null;

            // Let the next sample establish a fresh baseline instead of comparing against counts
            // and a cursor from before the reload, which would look like a gap.
            s_LastConsoleTotal = -1;
            s_LastBufferSeq = -1;
            s_GapPending = false;
            s_GapSeq = 0;
        }

        static void TrySeed()
        {
            if (!Reseed())
                return;

            s_Seeded = true;
            SessionState.SetBool(SeededKey, true);
        }

        internal static void ResampleAfterClear()
        {
            // Unity retains sticky compiler errors when the Console is cleared.
            s_Seeded = !EditorUtility.scriptCompilationFailed;
            s_NextSampleTime = 0;
        }

        /// <summary>
        /// Copy console entries the buffer does not already hold into it. Entries are matched on type
        /// and message, the only fields both sides record identically. A log type never contains "|",
        /// so the first one always terminates it and the key is unambiguous.
        ///
        /// Matching counts occurrences rather than presence, because the console holds repeated
        /// messages by design. Treating a key as seen-or-not lets a row the buffer already has cancel
        /// out an identical row it missed, and that row is then never recovered.
        /// </summary>
        static bool Reseed()
        {
            var entries = new List<EditorConsoleEntries.Entry>();
            if (!EditorConsoleEntries.TryReadRecent(ConsoleLogBuffer.Capacity, entries))
                return false;

            var held = new Dictionary<string, int>();
            foreach (var entry in ConsoleLogCapture.Buffer.Query(-1, 0, ConsoleLogBuffer.SeverityLog).Entries)
            {
                var key = Key(entry.LogType, entry.Message);
                held.TryGetValue(key, out var count);
                held[key] = count + 1;
            }

            var now = DateTime.UtcNow;
            foreach (var entry in entries)
            {
                var key = Key(entry.Type.ToString(), entry.Message);

                // Consume one occurrence the buffer already holds; whatever the console has beyond
                // that is what capture missed.
                if (held.TryGetValue(key, out var remaining) && remaining > 0)
                {
                    held[key] = remaining - 1;
                    continue;
                }

                ConsoleLogCapture.Buffer.Add(entry.Type, entry.Message, entry.StackTrace, now, seeded: true);
            }

            return true;
        }

        static string Key(string logType, string message) => logType + "|" + message;
    }

    /// <summary>
    /// Reads the Editor console's own entry store through UnityEditor.LogEntries.
    ///
    /// This is the only way to see entries the package's log-callback capture never received.
    /// Editor compile errors are logged with a sticky flag, and the console's Clear skips sticky
    /// entries, so they survive the Clear button, Clear on Play, Clear on Build and Clear on
    /// Recompile — only the next completed compile removes them. They can therefore stand in the
    /// console long after the callback-fed buffer evicted them or was never subscribed.
    ///
    /// Reflection rather than a direct reference, because LogEntries is internal. Any failure
    /// disables this permanently for the session and callers report the console as unseeded.
    /// </summary>
    static class EditorConsoleEntries
    {
        // ConsoleMode bits from Unity's native EditorMonoConsole (mirrored by the internal
        // ConsoleWindow.ConsoleFlags): the three level toggles the console filter checks, and
        // Collapse, which folds identical entries into a single row.
        const int LevelFlags = (1 << 7) | (1 << 8) | (1 << 9);
        const int CollapseFlag = 1 << 0;

        // LogMessageFlags bits, grouped exactly as Unity's own LogModeToLogType groups them.
        const int ModeScriptingException = 1 << 17;
        const int ModeErrors = (1 << 0) | (1 << 4) | (1 << 8) | (1 << 11) | (1 << 6);
        const int ModeAsserts = (1 << 1) | (1 << 21);
        const int ModeWarnings = (1 << 9) | (1 << 12) | (1 << 7);

        static readonly MethodInfo k_GetCountsByType;
        static readonly MethodInfo k_StartGettingEntries;
        static readonly MethodInfo k_EndGettingEntries;
        static readonly MethodInfo k_GetEntryInternal;
        static readonly MethodInfo k_GetFilteringText;
        static readonly MethodInfo k_SetFilteringText;
        static readonly MethodInfo k_GetConsoleFlags;
        static readonly MethodInfo k_SetConsoleFlags;
        static readonly Type k_LogEntryType;
        static readonly FieldInfo k_MessageField;
        static readonly FieldInfo k_ModeField;
        static readonly FieldInfo k_CallstackStartField;

        static bool s_Disabled;

        /// <summary>An entry as it stands in the Editor console.</summary>
        internal struct Entry
        {
            public LogType Type;
            public string Message;
            public string StackTrace;
        }

        static EditorConsoleEntries()
        {
            // LogEntries and LogEntry are internal, and LogEntries' members are static and
            // non-public, so NonPublic is mandatory in the binding flags — GetMethod without it
            // silently returns null.
            const BindingFlags statics = BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Static;

            var logEntries = Type.GetType("UnityEditor.LogEntries,UnityEditor");
            k_LogEntryType = Type.GetType("UnityEditor.LogEntry,UnityEditor");
            if (logEntries == null || k_LogEntryType == null)
                return;

            k_GetCountsByType = logEntries.GetMethod("GetCountsByType", statics);
            k_StartGettingEntries = logEntries.GetMethod("StartGettingEntries", statics);
            k_EndGettingEntries = logEntries.GetMethod("EndGettingEntries", statics);
            k_GetEntryInternal = logEntries.GetMethod("GetEntryInternal", statics);
            k_GetFilteringText = logEntries.GetMethod("GetFilteringText", statics);
            k_SetFilteringText = logEntries.GetMethod("SetFilteringText", statics);

            var consoleFlags = logEntries.GetProperty("consoleFlags", statics);
            k_GetConsoleFlags = consoleFlags?.GetGetMethod(true);
            k_SetConsoleFlags = consoleFlags?.GetSetMethod(true);

            const BindingFlags fields = BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Instance;
            k_MessageField = k_LogEntryType.GetField("message", fields);
            k_ModeField = k_LogEntryType.GetField("mode", fields);
            k_CallstackStartField = k_LogEntryType.GetField("callstackTextStartUTF16", fields);
        }

        /// <summary>
        /// True when every member this reads was found and no read has failed yet.
        /// </summary>
        internal static bool Available =>
            !s_Disabled
            && k_StartGettingEntries != null && k_EndGettingEntries != null && k_GetEntryInternal != null
            && k_GetFilteringText != null && k_SetFilteringText != null
            && k_GetConsoleFlags != null && k_SetConsoleFlags != null
            && k_MessageField != null && k_ModeField != null && k_CallstackStartField != null;

        /// <summary>
        /// Read up to <paramref name="max"/> most recent console entries, oldest first. Returns
        /// false when the internal API is unavailable or the read failed, in which case
        /// <paramref name="into"/> is left untouched. Main thread only.
        /// </summary>
        /// <param name="max">Maximum number of entries to read, taken from the end of the console.</param>
        /// <param name="into">Receives the entries read.</param>
        /// <returns>True when entries were read.</returns>
        internal static bool TryReadRecent(int max, List<Entry> into)
        {
            if (!Available || max <= 0)
                return false;

            var flags = 0;
            var filter = string.Empty;
            var flagsMutated = false;
            var filterMutated = false;

            try
            {
                flags = (int)k_GetConsoleFlags.Invoke(null, null);
                filter = (string)k_GetFilteringText.Invoke(null, null) ?? string.Empty;

                // The console's row list is filtered by the level toggles and the search box, and
                // Collapse folds repeats into one row, so a user who has any of them set would
                // otherwise hide entries from this read. Open the view only when it is actually
                // restricted: in the default state this mutates nothing and avoids two native
                // re-filter passes.
                //
                // Collapse matters because the caller counts occurrences to work out what it missed;
                // a collapsed row reports one where the console holds several.
                //
                // Both must happen outside the StartGettingEntries bracket: setting them rebuilds
                // the filtered index, and reordering it under a live snapshot invalidates the row
                // indices the read below uses.
                var opened = (flags | LevelFlags) & ~CollapseFlag;
                if (opened != flags)
                {
                    k_SetConsoleFlags.Invoke(null, new object[] { opened });
                    flagsMutated = true;
                }

                if (!string.IsNullOrEmpty(filter))
                {
                    k_SetFilteringText.Invoke(null, new object[] { string.Empty });
                    filterMutated = true;
                }

                // StartGettingEntries returns the row count and pins it for the duration; GetCount
                // would report that same pinned value, so there is no reason to ask twice.
                var total = (int)k_StartGettingEntries.Invoke(null, null);
                try
                {
                    var entry = Activator.CreateInstance(k_LogEntryType);
                    var args = new object[2];
                    for (var row = Math.Max(0, total - max); row < total; row++)
                    {
                        args[0] = row;
                        args[1] = entry;
                        if (!(bool)k_GetEntryInternal.Invoke(null, args))
                            continue;

                        into.Add(ToEntry(entry));
                    }
                }
                finally
                {
                    // Leaving the snapshot open pins the console's row count for every other reader
                    // and makes Unity warn on the next StartGettingEntries.
                    k_EndGettingEntries.Invoke(null, null);
                }

                return true;
            }
            catch (Exception ex)
            {
                s_Disabled = true;
                Debug.LogWarning($"[Pipeline] Cannot read the Editor console store; console entries logged before capture started will not be reported: {ex.Message}");
                return false;
            }
            finally
            {
                if (filterMutated)
                    TryRestore(k_SetFilteringText, filter);
                if (flagsMutated)
                    TryRestore(k_SetConsoleFlags, flags);
            }
        }

        internal static void ReadCounts(out int errors, out int warnings, out int logs)
        {
            errors = warnings = logs = 0;
            if (k_GetCountsByType == null)
                return;

            var counts = new object[] { 0, 0, 0 };
            k_GetCountsByType.Invoke(null, counts);
            errors = (int)counts[0];
            warnings = (int)counts[1];
            logs = (int)counts[2];
        }

        static Entry ToEntry(object logEntry)
        {
            var message = (string)k_MessageField.GetValue(logEntry) ?? string.Empty;
            var mode = (int)k_ModeField.GetValue(logEntry);

            // A console entry stores its message and callstack in one string, split at this index.
            var split = Mathf.Clamp((int)k_CallstackStartField.GetValue(logEntry), 0, message.Length);

            return new Entry
            {
                Type = ToLogType(mode),
                Message = message.Substring(0, split),
                StackTrace = message.Substring(split)
            };
        }

        static LogType ToLogType(int mode)
        {
            if ((mode & ModeScriptingException) != 0)
                return LogType.Exception;
            if ((mode & ModeErrors) != 0)
                return LogType.Error;
            if ((mode & ModeAsserts) != 0)
                return LogType.Assert;
            if ((mode & ModeWarnings) != 0)
                return LogType.Warning;
            return LogType.Log;
        }

        static void TryRestore(MethodInfo setter, object value)
        {
            try
            {
                setter.Invoke(null, new[] { value });
            }
            catch (Exception ex)
            {
                Debug.LogWarning($"[Pipeline] Failed to restore Editor console filtering state: {ex.Message}");
            }
        }
    }
}
#endif
