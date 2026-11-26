using System;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Console;
using Unity.Pipeline.Models;

namespace Unity.Pipeline.Runtime.Commands
{
    /// <summary>
    /// Returns captured Unity console output. Backs the CLI's
    /// <c>console [--tail N] [--level log|warn|error] [--follow]</c>.
    ///
    /// Lives in the runtime assembly, so it is available from both the Editor and player builds —
    /// console capture is driven by <see cref="ConsoleLogCapture"/>, which runs in both contexts.
    ///
    /// The package has no streaming transport (the HTTP server handles one request at a time), so
    /// <c>--follow</c> is realized client-side: the CLI polls this command, prints new entries, and
    /// passes the returned <see cref="ConsoleLogResponse.Cursor"/> back as <c>since</c> on the next
    /// call. The flag mapping is:
    ///   --tail N             -> tail
    ///   --level X            -> level   (minimum severity threshold)
    ///   --follow             -> repeated calls with since = previous cursor
    ///
    /// Reads only the in-memory buffer (no Unity main-thread APIs), so it is marked
    /// <c>MainThreadRequired = false</c> for fast polling.
    /// </summary>
    static class ConsoleCommand
    {
        /// <summary>Default number of entries returned when <c>tail</c> is not supplied.</summary>
        public const int DefaultTail = 100;

        [CliCommand("console", "Get captured Unity console output (Editor or Player; supports tail, level filtering, and follow via a cursor)", MainThreadRequired = false, Effect = CommandEffect.ReadOnly, Tags = new[] { "observability/console" })]
        public static ConsoleLogResponse GetConsole(
            [CliArg("tail", "Maximum number of most-recent entries to return")] int tail = DefaultTail,
            [CliArg("level", "Minimum severity to include: log | warn | error")] string level = ConsoleLogBuffer.LevelLog,
            [CliArg("since", "Cursor: only return entries newer than this seq. Use the 'cursor' from a previous response to follow.")] long since = -1,
            [CliArg("since-session", "Session from the previous response. A stale cursor returns a fresh tail and reset=true.")] string since_session = null)
        {
            // Guard against nonsensical tail values; <= 0 would otherwise mean "unlimited" in the
            // buffer, which is not what a caller passing e.g. --tail 0 expects.
            if (tail <= 0)
                tail = DefaultTail;

            var minSeverity = ConsoleLogBuffer.SeverityFromLevelName(level);
            var response = ConsoleLogCapture.Buffer.Query(since, tail, minSeverity, since_session);
            response.GroundTruth = ReadGroundTruth();
            return response;
        }

        [CliCommand("console_status", "Read retained severity counts, cursor and the most recent Editor console and compiler sample.", MainThreadRequired = false, Effect = CommandEffect.ReadOnly, Tags = new[] { "observability/console" })]
        public static ConsoleLogResponse GetStatus()
        {
            var response = ConsoleLogCapture.Buffer.Stats();
            response.GroundTruth = ReadGroundTruth();
            return response;
        }

        /// <summary>
        /// A copy of the last ground-truth sample with its age filled in. Copied rather than
        /// returned directly so the published snapshot stays immutable and shared between callers.
        /// </summary>
        static ConsoleGroundTruth ReadGroundTruth()
        {
            var sample = ConsoleLogCapture.GroundTruth;
            if (sample == null)
                return null;

            var ageMs = (long)(DateTime.UtcNow - sample.SampledUtc).TotalMilliseconds;
            return new ConsoleGroundTruth
            {
                SampledUtc = sample.SampledUtc,
                AgeMs = ageMs < 0 ? 0 : ageMs,
                CompilationFailed = sample.CompilationFailed,
                Compiling = sample.Compiling,
                ConsoleErrors = sample.ConsoleErrors,
                ConsoleWarnings = sample.ConsoleWarnings,
                ConsoleLogs = sample.ConsoleLogs,
                Seeded = sample.Seeded
            };
        }
    }
}
