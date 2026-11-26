using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Linq;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
#if UNITY_EDITOR || (UNITY_STANDALONE && DEBUG)
using Microsoft.CodeAnalysis.CSharp;
using Microsoft.CodeAnalysis.CSharp.Syntax;
#endif
using Unity.Pipeline.Models;
using UnityEngine;

namespace Unity.Pipeline.Compilation
{
    static class EvalCodeCompiler
    {
        // Synchronous compiler fixtures use this entry point. Never block on an async snippet
        // from Unity's main thread.
        public static EvalResponse CompileAndExecuteOnMainThread(string code, int timeoutMs, Stopwatch stopwatch)
        {
            var result = CompileAndExecuteAsync(code, timeoutMs, false, stopwatch ?? Stopwatch.StartNew());
            if (!result.IsCompleted)
                throw new InvalidOperationException("Use asynchronous evaluation for code with await.");
            return result.GetAwaiter().GetResult();
        }

        public static async Task<EvalResponse> CompileAndExecuteAsync(string code, int timeoutMs, bool dryRun,
            Stopwatch stopwatch)
        {
#if UNITY_EDITOR || (UNITY_STANDALONE && DEBUG)
            var job = BasePipelineServer.CurrentJob;
            var id = "PipelineEval_" + Guid.NewGuid().ToString("N");
            var isAsync = false;
            using var compileBudget = new CancellationTokenSource(timeoutMs);
            try
            {
                var token = compileBudget.Token;
                var editor = Application.isEditor;
                var asyncHeader = Header(id, editor, true);
                isAsync = code.IndexOf("await", StringComparison.Ordinal) >= 0 && HasAwait(asyncHeader + code + Footer, token);
                var header = isAsync ? asyncHeader : Header(id, editor, false);

                var compilation = Compile(header, code, id, dryRun, token);
                // "await" was a legal identifier in the old synchronous wrapper.
                // Keep compilable legacy snippets even when async parsing sees a keyword.
                if (isAsync && !compilation.Success && compilation.Diagnostics.Any(d => d.Id == "CS4003"))
                {
                    var legacy = Compile(Header(id, editor, false), code, id, dryRun, token);
                    if (legacy.Success)
                    {
                        isAsync = false;
                        compilation = legacy;
                    }
                }

                if (job?.CancellationRequested == true)
                    throw new OperationCanceledException();
                if (token.IsCancellationRequested || stopwatch.ElapsedMilliseconds >= timeoutMs)
                    return TimedOut(stopwatch, timeoutMs);
                if (!compilation.Success)
                    return Failure("Compilation Failed", "Code compilation failed", stopwatch, dryRun || isAsync,
                        compilation.Diagnostics);
                if (dryRun)
                    return EvalResponse.EvalSuccess(null, executionTimeMs: stopwatch.ElapsedMilliseconds,
                        diagnostics: compilation.Diagnostics);

                var method = compilation.Assembly.GetType("PipelineEvaluation." + id)?.GetMethod("Execute");
                if (method == null)
                    return Failure("Method Not Found", "The compiled evaluation entry point could not be found.",
                        stopwatch, isAsync);
                if (!isAsync)
                    return EvalResponse.EvalSuccess(SerializeResult(method.Invoke(null, null)),
                        executionTimeMs: stopwatch.ElapsedMilliseconds);

                using var execution = new CancellationTokenSource();
                // Start the snippet and its result serialization on Unity's synchronization
                // context. The watchdog itself must stay responsive when Unity pauses that
                // context, such as in a background Editor with Play Mode paused.
                var task = SerializeWhenFinished((Task<object>)method.Invoke(null, new object[] { execution.Token }));
                while (!task.IsCompleted && job?.CancellationRequested != true &&
                    stopwatch.ElapsedMilliseconds < timeoutMs)
                {
                    var remaining = timeoutMs - stopwatch.ElapsedMilliseconds;
                    await Task.Delay((int)Math.Max(1, Math.Min(25, remaining))).ConfigureAwait(false);
                }
                if (job?.CancellationRequested == true || !task.IsCompleted)
                {
                    try { execution.Cancel(); }
                    catch (AggregateException ex) { UnityEngine.Debug.LogWarning(ex.Message); }
                    // Give cooperative code an opportunity to unwind on Unity's main thread.
                    if (await Task.WhenAny(task, Task.Delay(200)).ConfigureAwait(false) != task || task.IsFaulted)
                        _ = task.ContinueWith(fault => { _ = fault.Exception; }, CancellationToken.None,
                            TaskContinuationOptions.OnlyOnFaulted | TaskContinuationOptions.ExecuteSynchronously,
                            TaskScheduler.Default);
                    if (job?.CancellationRequested == true)
                        throw new OperationCanceledException();
                    return TimedOut(stopwatch, timeoutMs);
                }
                return EvalResponse.EvalSuccess(await task.ConfigureAwait(false), executionTimeMs: stopwatch.ElapsedMilliseconds);
            }
            catch (OperationCanceledException) when (job?.CancellationRequested == true)
            {
                throw;
            }
            catch (OperationCanceledException) when (compileBudget.IsCancellationRequested)
            {
                return TimedOut(stopwatch, timeoutMs);
            }
            catch (Exception ex)
            {
                var inner = (ex as TargetInvocationException)?.InnerException ?? ex;
                return Failure("Runtime Error", inner.Message, stopwatch, isAsync);
            }
#else
            return await Task.FromResult(EvalResponse.EvalFailure("Platform Not Supported",
                "Runtime code compilation only supported on Desktop development builds"));
#endif
        }

#if UNITY_EDITOR || (UNITY_STANDALONE && DEBUG)
        static async Task<object> SerializeWhenFinished(Task<object> task) => SerializeResult(await task);

        static CompilationResult Compile(string header, string code, string id, bool dryRun, CancellationToken token) =>
            RoslynCompilationService.Compile(new CompilationRequest
            {
                SourceCode = header + code + Footer,
                AssemblyName = id,
                LineNumberOffset = header.Count(ch => ch == '\n'),
                SkipLoad = dryRun,
                CancellationToken = token
            });

        static EvalResponse TimedOut(Stopwatch stopwatch, int budget) =>
            new EvalResponse
            {
                Command = "eval",
                Error = "Timeout",
                ErrorDetails = $"Evaluation exceeded its {budget} ms budget.",
                ExecutionTimeMs = stopwatch.ElapsedMilliseconds,
                Status = "timed_out",
                ExecutedAt = DateTime.UtcNow
            };

        static EvalResponse Failure(string error, string details, Stopwatch stopwatch, bool reportExitCode,
            List<DiagnosticInfo> diagnostics = null)
        {
            var result = EvalResponse.EvalFailure(error, details, stopwatch.ElapsedMilliseconds, diagnostics);
            if (reportExitCode)
                result.Status = "failed";
            return result;
        }

        static bool HasAwait(string source, CancellationToken cancellationToken)
        {
            var body = CSharpSyntaxTree.ParseText(source, RoslynCompilationService.ProjectParseOptions(),
                cancellationToken: cancellationToken).GetRoot(cancellationToken)
                .DescendantNodes().OfType<MethodDeclarationSyntax>().First().Body;
            return body.DescendantNodes(node => !(node is LocalFunctionStatementSyntax)
                    && !(node is AnonymousFunctionExpressionSyntax))
                .Any(node => node is AwaitExpressionSyntax expression && !expression.Expression.IsMissing ||
                    node is CommonForEachStatementSyntax each && each.AwaitKeyword.RawKind != 0 ||
                    node is UsingStatementSyntax statement && statement.AwaitKeyword.RawKind != 0 ||
                    node is LocalDeclarationStatementSyntax declaration && declaration.AwaitKeyword.RawKind != 0);
        }

        static string Header(string id, bool editor, bool isAsync)
        {
            var editorUsing = editor ? "using UnityEditor;\n" : "";
            var taskUsing = isAsync ? "using System.Threading.Tasks;\n" : "";
            var signature = isAsync
                ? "async global::System.Threading.Tasks.Task<object> Execute(global::System.Threading.CancellationToken cancellationToken)"
                : "object Execute()";
            return "using System;\nusing System.Collections.Generic;\nusing System.Linq;\nusing UnityEngine;\n" +
                editorUsing + taskUsing +
                $"namespace PipelineEvaluation\n{{\n    public static class {id}\n    {{\n" +
                $"        public static {signature}\n        {{\n";
        }

        const string Footer = "\n#pragma warning disable CS0162\n            return null;\n" +
            "#pragma warning restore CS0162\n        }\n    }\n}\n";

        private static object SerializeResult(object value)
        {
            if (value == null) return null;

            // Handle primitive types directly
            if (value is string || value is bool || value is int || value is long || value is float || value is double)
                return value;

            // Handle Unity objects
            if (value is UnityEngine.Object unityObj)
            {
                try
                {
                    return Newtonsoft.Json.JsonConvert.DeserializeObject(UnityEngine.JsonUtility.ToJson(unityObj));
                }
                catch
                {
                    return value.ToString();
                }
            }

            // Handle other objects via JSON serialization
            try
            {
                return Newtonsoft.Json.JsonConvert.DeserializeObject(
                    Newtonsoft.Json.JsonConvert.SerializeObject(value));
            }
            catch
            {
                return value.ToString();
            }
        }
#endif
    }
}
