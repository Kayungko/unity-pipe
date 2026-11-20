using System;
using Newtonsoft.Json;

namespace Unity.Pipeline.Models
{
    /// <summary>
    /// Response model for the <c>console</c> command: a page of captured console entries plus
    /// the cursor needed to fetch the next page.
    /// </summary>
    [Serializable]
    public class ConsoleLogResponse
    {
        /// <summary>
        /// The matching entries, oldest first, after applying the level filter, <c>since</c> cursor,
        /// and <c>tail</c> limit.
        /// </summary>
        [JsonProperty("entries")]
        public ConsoleLogEntry[] Entries { get; set; }

        /// <summary>
        /// The highest <see cref="ConsoleLogEntry.Seq"/> currently held in the buffer (regardless of
        /// the level filter). A "--follow" client passes this back as <c>since</c> on the next poll
        /// so it resumes exactly where it left off, even if the only new entries were filtered out.
        /// Only meaningful paired with <see cref="Session"/>: sequence numbers restart from zero
        /// with the capture session, so a cursor from an earlier session names different entries.
        /// </summary>
        [JsonProperty("cursor")]
        public long Cursor { get; set; }

        /// <summary>
        /// Identifies the capture session <see cref="Cursor"/> belongs to. A "--follow" client
        /// passes this back as <c>since-session</c> alongside <c>since</c>; a cursor from a
        /// different session is refused rather than silently misapplied.
        /// </summary>
        [JsonProperty("session")]
        public string Session { get; set; }

        /// <summary>
        /// Number of entries returned in <see cref="Entries"/>.
        /// </summary>
        [JsonProperty("returned")]
        public int Returned { get; set; }

        /// <summary>
        /// True when the requested <c>since</c> cursor pointed at entries that have already been
        /// evicted from the bounded buffer, meaning the consumer missed some entries. Lets a
        /// "--follow" client warn about a gap instead of silently skipping output. Also true
        /// whenever <see cref="Reset"/> is.
        /// </summary>
        [JsonProperty("dropped")]
        public bool Dropped { get; set; }

        /// <summary>
        /// True when the requested cursor could not be honored — it came from another session, or
        /// it was past the current counter — so the cursor was ignored and the tail returned
        /// instead. A "--follow" client should adopt <see cref="Cursor"/> and <see cref="Session"/>
        /// and treat the returned entries as a fresh start.
        /// </summary>
        [JsonProperty("reset")]
        public bool Reset { get; set; }

        /// <summary>
        /// How many entries of each severity the buffer currently holds, before the level filter
        /// and <c>tail</c> limit. Compare with <see cref="ConsoleGroundTruth.ConsoleErrors"/> and
        /// friends to see whether the buffer is missing entries the Editor console still shows.
        /// </summary>
        [JsonProperty("counts")]
        public ConsoleLevelCounts Counts { get; set; }

        /// <summary>
        /// Editor ground truth about the console at the time it was last sampled. Null in a player
        /// build, and before the first sample after an Editor load.
        /// </summary>
        [JsonProperty("groundTruth")]
        public ConsoleGroundTruth GroundTruth { get; set; }
    }

    /// <summary>
    /// Entry counts per severity retained in the buffer, before the level filter and <c>tail</c>
    /// limit are applied. Compare with the Editor console's own totals in
    /// <see cref="ConsoleGroundTruth"/> to see whether the buffer is missing entries the console
    /// still shows.
    /// </summary>
    [Serializable]
    public class ConsoleLevelCounts
    {
        /// <summary>
        /// Retained entries at error severity. Unity's Error, Exception and Assert log types all
        /// count here.
        /// </summary>
        [JsonProperty("error")]
        public int Error { get; set; }

        /// <summary>Retained entries at warning severity.</summary>
        [JsonProperty("warn")]
        public int Warn { get; set; }

        /// <summary>Retained entries at informational severity.</summary>
        [JsonProperty("log")]
        public int Log { get; set; }
    }

    /// <summary>
    /// Editor ground truth about the console, sampled on the main thread and served alongside
    /// buffered entries so a client can tell whether the buffer agrees with the Editor console
    /// without a second round trip. Null when served from a player build, which has no Editor
    /// console.
    /// </summary>
    [Serializable]
    public class ConsoleGroundTruth
    {
        /// <summary>When the sample was taken (UTC).</summary>
        [JsonProperty("sampledUtc")]
        public DateTime SampledUtc { get; set; }

        /// <summary>
        /// How long ago the sample was taken. Sampling runs on the main thread, so it stops while
        /// the Editor is blocked compiling or importing; a large age means the rest of this object
        /// describes an older state of the console.
        /// </summary>
        [JsonProperty("ageMs")]
        public long AgeMs { get; set; }

        /// <summary>
        /// True while the project has editor compile errors. Read from
        /// <c>EditorUtility.scriptCompilationFailed</c>, which is native state independent of the
        /// log callback and of any domain reload, so it stays true for as long as the errors do.
        /// </summary>
        [JsonProperty("compilationFailed")]
        public bool CompilationFailed { get; set; }

        /// <summary>True while script compilation is in progress.</summary>
        [JsonProperty("compiling")]
        public bool Compiling { get; set; }

        /// <summary>
        /// Errors the Editor console holds. This is a console-lifetime total, so comparing it with
        /// <see cref="ConsoleLevelCounts.Error"/> shows whether the buffer is missing entries the
        /// console still displays.
        /// </summary>
        [JsonProperty("consoleErrors")]
        public int ConsoleErrors { get; set; }

        /// <summary>Warnings the Editor console holds.</summary>
        [JsonProperty("consoleWarnings")]
        public int ConsoleWarnings { get; set; }

        /// <summary>Informational entries the Editor console holds.</summary>
        [JsonProperty("consoleLogs")]
        public int ConsoleLogs { get; set; }

        /// <summary>
        /// True once the buffer has been backfilled from the Editor console's own store. False
        /// means that backfill is unavailable on this Editor version, which is a benign explanation
        /// for the buffer holding fewer entries than <see cref="ConsoleErrors"/> and friends report.
        /// </summary>
        [JsonProperty("seeded")]
        public bool Seeded { get; set; }
    }
}
