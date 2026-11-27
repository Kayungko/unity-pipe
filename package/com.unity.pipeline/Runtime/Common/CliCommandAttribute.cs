using System;
using System.Collections.Generic;
using Newtonsoft.Json;

namespace Unity.Pipeline.Commands
{
    public enum CommandEffect
    {
        Unknown,
        ReadOnly,
        Stateful,
        Destructive
    }

    public enum CommandLogLevel
    {
        Debug,
        Info,
        Warning,
        Error
    }

    /// <summary>A log entry to return with a command's terminal result.</summary>
    public sealed class CommandLog
    {
        [JsonProperty("level")]
        public string Level { get; }
        [JsonProperty("message")]
        public string Message { get; }

        public CommandLog(CommandLogLevel level, string message)
        {
            if (level < CommandLogLevel.Debug || level > CommandLogLevel.Error)
                throw new ArgumentOutOfRangeException(nameof(level));
            Level = level.ToString().ToLowerInvariant();
            Message = message ?? throw new ArgumentNullException(nameof(message));
        }

        // The level was validated on the original entry.
        internal CommandLog(string level, string message)
        {
            Level = level;
            Message = message;
        }
    }

    /// <summary>
    /// Return directly or as a Task result to report a command's outcome with optional logs.
    /// Existing methods can continue returning their ordinary payload.
    /// </summary>
    public sealed class PipelineCommandResult
    {
        internal const int MaxBinaryBytes = 512 * 1024;
        internal const string FailureCode = "COMMAND_REPORTED_FAILURE";

        internal bool Success => Error == null;
        internal object Value { get; }
        internal string Error { get; }
        internal IReadOnlyList<CommandLog> Logs { get; }

        private PipelineCommandResult(object value, string error, CommandLog[] logs)
        {
            Value = value;
            Error = error;
            if (logs != null && logs.Length > 0)
                Logs = Array.AsReadOnly((CommandLog[])logs.Clone());
        }

        public static PipelineCommandResult Succeed(object value = null, params CommandLog[] logs) =>
            new PipelineCommandResult(value, null, logs);

        /// <summary>Return binary stdout with optional terminal logs.</summary>
        public static PipelineCommandResult SucceedBinary(byte[] stdout, params CommandLog[] logs)
        {
            if (stdout == null)
                throw new ArgumentNullException(nameof(stdout));
            if (stdout.Length > MaxBinaryBytes)
                return Fail($"Binary stdout exceeds the {MaxBinaryBytes}-byte limit.", null, logs);
            return Succeed(new { stdoutBase64 = Convert.ToBase64String(stdout) }, logs);
        }

        public static PipelineCommandResult Fail(string message, object data = null, params CommandLog[] logs)
        {
            if (string.IsNullOrWhiteSpace(message))
                throw new ArgumentException("Provide a message for the failed command.", nameof(message));
            return new PipelineCommandResult(data, message, logs);
        }
    }

    /// <summary>
    /// Attribute to mark static methods as CLI-accessible commands.
    /// Commands with this attribute can be executed remotely via Pipeline Server.
    /// Adapted from unity-tools [Tool] attribute for Pipeline organization requirements.
    /// </summary>
    [AttributeUsage(AttributeTargets.Method, AllowMultiple = false)]
    public class CliCommandAttribute : Attribute
    {
        /// <summary>
        /// Unique name of the command for CLI execution.
        /// Used in "unity request command_name" syntax.
        /// </summary>
        public string Name { get; }

        /// <summary>
        /// Human-readable description of what the command does.
        /// Used in CLI help text and command discovery.
        /// </summary>
        public string Description { get; }

        /// <summary>
        /// Whether this command requires Unity main thread execution.
        /// Default: true (most Unity APIs require main thread).
        /// </summary>
        public bool MainThreadRequired { get; set; } = true;

        /// <summary>
        /// Whether this command belongs to the runtime (Player) command surface only.
        /// Runtime-only commands are hidden from an Editor server's command listing
        /// (they remain executable, but are not advertised when connected to an Editor).
        /// Default: false (listed on the Editor server).
        /// </summary>
        public bool RuntimeOnly { get; set; } = false;

        public CommandEffect Effect { get; set; } = CommandEffect.Unknown;

        /// <summary>
        /// Optional hierarchical tags used to group and browse commands.
        /// Path-style: a '/' separates tag from subtag (e.g. "assets", "assets/import").
        /// Default: empty (untagged).
        /// </summary>
        public string[] Tags { get; set; } = Array.Empty<string>();

        /// <summary>
        /// Create a new CLI command attribute.
        /// </summary>
        /// <param name="name">Unique command name for CLI execution</param>
        /// <param name="description">Human-readable description</param>
        public CliCommandAttribute(string name, string description)
        {
            Name = name ?? throw new ArgumentNullException(nameof(name));
            Description = description ?? throw new ArgumentNullException(nameof(description));
        }
    }
}
