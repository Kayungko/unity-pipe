using System;
using System.IO;
using System.Threading;
using System.Threading.Tasks;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Compilation;
using Unity.Pipeline.Models;
using Unity.Pipeline.Telemetry;

namespace Unity.Pipeline.Runtime.Commands
{
    static class CodeEvalCommand
    {
        const int MaxTimeoutMs = 86_400_000;

        [CliCommand("eval", "Compile and evaluate a C# snippet, including await expressions.", MainThreadRequired = true, Tags = new[] { "scripts/eval" })]
        public static Task<EvalResponse> EvaluateCode(
            [CliArg("code", "C# code to evaluate", Required = true)] string code,
            [CliArg("timeout", "Timeout in milliseconds")] int timeout = 5000,
            [CliArg("dry_run", "Compile and report diagnostics without loading or executing the snippet.")] bool dryRun = false)
        {
            if (string.IsNullOrWhiteSpace(code))
                return Task.FromResult(Reject("eval", code, "Code parameter is required and cannot be empty", dryRun));
            return EvaluateSource(code, timeout, "eval", dryRun);
        }

        [CliCommand("eval_file", "Evaluate a C# snippet read from an existing .cs file.", MainThreadRequired = true, Tags = new[] { "scripts/eval" })]
        public static Task<EvalResponse> EvaluateFile(
            [CliArg("file", "Path to a .cs file to evaluate", Required = true)] string file,
            [CliArg("timeout", "Timeout in milliseconds")] int timeout = 5000,
            [CliArg("dry_run", "Compile and report diagnostics without loading or executing the snippet.")] bool dryRun = false)
        {
            if (string.IsNullOrWhiteSpace(file))
                return Task.FromResult(Reject("eval_file", null, "File parameter is required and cannot be empty", dryRun));
            if (!file.EndsWith(".cs", StringComparison.OrdinalIgnoreCase))
                return Task.FromResult(Reject("eval_file", null, "File must have a .cs extension", dryRun));
            if (!File.Exists(file))
                return Task.FromResult(Reject("eval_file", null, $"File not found: {file}", dryRun));

            string code;
            try { code = File.ReadAllText(file); }
            catch (Exception ex)
            {
                return Task.FromResult(Reject("eval_file", null, $"Failed to read file: {ex.Message}", dryRun));
            }

            if (string.IsNullOrWhiteSpace(code))
                return Task.FromResult(Reject("eval_file", code, $"File is empty: {file}", dryRun));
            return EvaluateSource(code, timeout, "eval_file", dryRun);
        }

        static EvalResponse Reject(string command, string code, string details, bool dryRun)
        {
            var response = EvalResponse.EvalFailure("Bad Request", details);
            if (dryRun)
                response.Status = "failed";
            Record(command, code, response);
            return response;
        }

        static async Task<EvalResponse> EvaluateSource(string code, int timeout, string command, bool dryRun)
        {
            if (timeout <= 0 || timeout > MaxTimeoutMs)
                return Reject(command, code, $"Timeout must be between 1ms and {MaxTimeoutMs}ms", dryRun);

            var stopwatch = System.Diagnostics.Stopwatch.StartNew();
            var context = SynchronizationContext.Current;
            var threadId = Thread.CurrentThread.ManagedThreadId;
            var response = await EvalCodeCompiler.CompileAndExecuteAsync(code, timeout, dryRun, stopwatch).ConfigureAwait(false);
            if (Thread.CurrentThread.ManagedThreadId == threadId)
                Record(command, code, response);
            else
            {
                try { context?.Post(_ => Record(command, code, response), null); }
                catch { /* A stale telemetry context must not alter the command outcome. */ }
            }
            return response;
        }

        // Local telemetry snapshots its settings on Unity's main thread. A telemetry error
        // must remain separate from the actual compile or execution result.
        static void Record(string command, string code, EvalResponse response)
        {
            try
            {
                EvalUsageTelemetry.RecordInBackground(command, code ?? string.Empty, response.Success,
                    response.Error, response.ExecutionTimeMs ?? 0L);
            }
            catch { /* Telemetry is best-effort. */ }
        }
    }
}
