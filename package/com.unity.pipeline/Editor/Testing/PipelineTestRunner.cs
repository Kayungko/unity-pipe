#if UNITY_EDITOR
using System;
using System.Collections;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
using UnityEditor;
using UnityEditor.TestTools.TestRunner.Api;
using UnityEngine;
using Newtonsoft.Json;
#if UNITY_6000_5_OR_NEWER
using Unity.Scripting.LifecycleManagement;
#endif

namespace Unity.Pipeline.Editor.Testing
{
    /// <summary>
    /// Core test execution engine supporting both synchronous and asynchronous execution modes.
    /// Adapted from unity-tools with domain reload handling and dual mode support.
    /// </summary>
#if UNITY_6000_5_OR_NEWER
    [NoAutoStaticsCleanup]
#endif
    static class PipelineTestRunner
    {
        private const string TestRequestFile = "Temp/pipeline_test_request.json";
        private const string TestStatusFile = "Temp/pipeline_test_status.json";
        private static readonly object s_TestStatusLock = new object();

        private static TestRunnerApi m_ActiveApi;
        private static TestResultCollector m_ActiveCollector;
        private static TestRequest s_AsyncRun;
        private static string s_DrainingRunId;

        // Test Framework 1.1.33 has no public cancellation API. Look up only the job
        // returned by Execute, so stopping a Pipeline run cannot stop someone else's tests.
        private static readonly MethodInfo CancelNativeRun = typeof(TestRunnerApi).GetMethod(
            "CancelTestRun", BindingFlags.Public | BindingFlags.Static, null, new[] { typeof(string) }, null);
        private static readonly Type LegacyJobHolder = typeof(TestRunnerApi).Assembly.GetType(
            "UnityEditor.TestTools.TestRunner.TestRun.TestJobDataHolder");
        private static readonly Type LegacyPlayController = typeof(UnityEngine.TestTools.UnityTestAttribute).Assembly.GetType(
            "UnityEngine.TestTools.TestRunner.PlaymodeTestsController");

        /// <summary>
        /// Execute tests with the given parameters
        /// </summary>
        /// <param name="mode">"editor"/"editmode", "playmode"/"play", or "all".</param>
        /// <param name="filter">Filter value to match against, per <paramref name="filterType"/>.</param>
        /// <param name="filterType">"testname", "assembly", or "category".</param>
        /// <param name="includeExplicit">Whether to include tests/fixtures marked [Explicit].</param>
        /// <param name="asyncMode">Run asynchronously, returning immediately with a status path to poll.</param>
        /// <param name="timeoutSeconds">Timeout for synchronous execution.</param>
        /// <returns>The test execution response.</returns>
        public static async Task<TestExecutionResponse> ExecuteTestsAsync(
            string mode,
            string filter,
            string filterType,
            bool includeExplicit,
            bool asyncMode,
            int timeoutSeconds)
        {
            try
            {
                var parsedMode = ParseTestMode(mode);
                if (!parsedMode.IsValid)
                {
                    return CreateErrorResponse($"Invalid mode '{mode}'. Use 'editor', 'playmode', or 'all'");
                }

                // Validate filter type
                var normalizedFilterType = filterType?.ToLower() ?? "testname";
                if (!string.IsNullOrEmpty(filter) &&
                    normalizedFilterType != "testname" && normalizedFilterType != "assembly" && normalizedFilterType != "category")
                {
                    return CreateErrorResponse($"Invalid filterType '{filterType}'. Valid options: testName, assembly, category");
                }

                if (timeoutSeconds <= 0)
                    return CreateErrorResponse("Test timeout must be greater than zero.");
                if (s_AsyncRun != null || m_ActiveCollector != null || FindLegacyJob(s_DrainingRunId) != null)
                    return CreateErrorResponse("A Pipeline test run is still active. Poll test_status or cancel it before starting another.");

                if (parsedMode.IsAll || asyncMode)
                    return StartAsyncRun(parsedMode, filter, normalizedFilterType, includeExplicit, timeoutSeconds);
                if (parsedMode.TestMode == TestMode.PlayMode)
                {
                    // PlayMode entry triggers a domain reload that drops the in-flight HTTP request,
                    // so a synchronous playmode run can never return results over one call. Fail fast
                    // with guidance instead of hanging until the connection resets.
                    return CreateErrorResponse(
                        "PlayMode tests cannot run synchronously over HTTP: entering play mode triggers a " +
                        "domain reload that drops the request. Re-run asynchronously (run_tests --mode " +
                        "playmode --async_tests) and poll the test_status command for results.");
                }
                else
                {
                    return await ExecuteSyncMode(parsedMode.TestMode, filter, normalizedFilterType, includeExplicit, timeoutSeconds);
                }
            }
            catch (Exception ex)
            {
                Debug.LogError($"[PipelineTestRunner] Error: {ex.Message}");
                return CreateErrorResponse($"Failed to run tests: {ex.Message}");
            }
        }

        /// <summary>
        /// Synchronous execution - blocks until completion
        /// </summary>
        private static async Task<TestExecutionResponse> ExecuteSyncMode(
            TestMode testMode,
            string filter,
            string filterType,
            bool includeExplicit,
            int timeoutSeconds)
        {
            var startTime = DateTime.UtcNow;
            var api = ScriptableObject.CreateInstance<TestRunnerApi>();
            var collector = new TestResultCollector();

            m_ActiveApi = api;
            m_ActiveCollector = collector;

            // Set up timeout cancellation
            using (var cts = new CancellationTokenSource(TimeSpan.FromSeconds(timeoutSeconds)))
            {
                cts.Token.Register(() =>
                {
                    Debug.Log("[PipelineTestRunner] Test execution timed out, cancelling...");
                    InvalidatePreviousRun();
                });

                try
                {
                    // For PlayMode tests, we need to handle domain reloads
                    if (testMode == TestMode.PlayMode)
                    {
                        // Save request for domain reload recovery
                        var request = new TestRequest
                        {
                            filter = filter,
                            filterType = filterType,
                            mode = testMode.ToString(),
                            includeExplicit = includeExplicit,
                            isSync = true
                        };
                        File.WriteAllText(TestRequestFile, JsonConvert.SerializeObject(request));
                    }

                    // Execute tests and wait for completion
                    var completionTask = ExecuteTestsInternal(api, collector, testMode, filter, filterType, includeExplicit);
                    var resultAdaptor = await completionTask;

                    // Build response
                    var duration = (DateTime.UtcNow - startTime).TotalSeconds;
                    var response = new TestExecutionResponse
                    {
                        Success = true,
                        Command = "run_tests",
                        Duration = Math.Round(duration, 2),
                        Mode = testMode.ToString(),
                        FilterApplied = string.IsNullOrEmpty(filter) ? null : $"{filterType}: {filter}",
                        ExecutionTimeMs = (int)(duration * 1000),
                        Summary = resultAdaptor != null ? new TestSummary
                        {
                            Total = resultAdaptor.PassCount + resultAdaptor.FailCount + resultAdaptor.SkipCount + resultAdaptor.InconclusiveCount,
                            Passed = resultAdaptor.PassCount,
                            Failed = resultAdaptor.FailCount,
                            Skipped = resultAdaptor.SkipCount,
                            Inconclusive = resultAdaptor.InconclusiveCount
                        } : new TestSummary(),
                        Results = collector.Results,
                        ExecutedAt = DateTime.UtcNow
                    };

                    // Clean up PlayMode request file
                    CleanupRequestFile();

                    return response;
                }
                catch (OperationCanceledException)
                {
                    return CreateErrorResponse("Test execution timed out");
                }
                catch (Exception ex)
                {
                    Debug.LogError($"[PipelineTestRunner] Sync execution error: {ex.Message}");
                    CleanupRequestFile();
                    return CreateErrorResponse($"Test execution failed: {ex.Message}");
                }
                finally
                {
                    UnregisterCollector(api, collector);
                }
            }
        }

        /// <summary>
        /// Internal test execution logic
        /// </summary>
        private static async Task<ITestResultAdaptor> ExecuteTestsInternal(
            TestRunnerApi api,
            TestResultCollector collector,
            TestMode testMode,
            string filter,
            string filterType,
            bool includeExplicit)
        {
            var tcs = new TaskCompletionSource<ITestResultAdaptor>();

            // Retrieve test list and filter
            api.RetrieveTestList(testMode, (ITestAdaptor rootTest) =>
            {
                try
                {
                    var testNames = CollectTestNames(rootTest, filter, filterType, includeExplicit);

                    if (testNames.Count == 0)
                    {
                        Debug.Log("[PipelineTestRunner] No tests to run after filtering");
                        var emptyResult = CreateEmptyTestResult();
                        tcs.SetResult(emptyResult);
                        return;
                    }

                    Debug.Log($"[PipelineTestRunner] Running {testNames.Count} tests (explicit tests {(includeExplicit ? "included" : "excluded")})");

                    var testFilter = new Filter
                    {
                        testMode = testMode,
                        testNames = testNames.ToArray()
                    };

                    // Set up completion handling
                    collector.OnRunFinished = () => tcs.SetResult(collector.RootResult);

                    api.RegisterCallbacks(collector);
                    api.Execute(new ExecutionSettings(testFilter));
                }
                catch (Exception ex)
                {
                    Debug.LogError($"[PipelineTestRunner] Error during test execution: {ex.Message}");
                    tcs.SetException(ex);
                }
            });

            return await tcs.Task;
        }

        private static TestExecutionResponse StartAsyncRun(ParsedTestMode mode, string filter, string filterType,
            bool includeExplicit, int timeoutSeconds)
        {
            var now = DateTime.UtcNow;
            var request = new TestRequest
            {
                runId = Guid.NewGuid().ToString("N"),
                all = mode.IsAll,
                mode = (mode.IsAll ? TestMode.EditMode : mode.TestMode).ToString(),
                filter = filter,
                filterType = filterType,
                includeExplicit = includeExplicit,
                startedAtUtc = now,
                deadlineUtc = now.AddSeconds(timeoutSeconds)
            };
            SaveRequest(request);
            s_AsyncRun = request;
            WriteRunStatus(request, "running");
            EditorApplication.update += PumpAsyncRun;
            return new TestExecutionResponse
            {
                Success = true,
                Command = "run_tests",
                Result = "running",
                RunId = request.runId,
                StatusPath = TestStatusFile,
                Mode = request.all ? "All" : request.mode,
                FilterApplied = string.IsNullOrEmpty(filter) ? null : $"{filterType}: {filter}",
                Message = "Tests queued. Poll test_status for results.",
                ExecutedAt = now
            };
        }

        private static void PumpAsyncRun()
        {
            var request = s_AsyncRun;
            if (request == null)
                return;
            try
            {
                if (request.stopStatus != null)
                {
                    DrainStoppedRun(request);
                    return;
                }
                if (DateTime.UtcNow >= request.deadlineUtc)
                {
                    StopAsyncRun(request, "timed_out", "Test run timed out.");
                    return;
                }
                if (m_ActiveCollector != null)
                    return;
                if (FindLegacyJob(request.previousNativeRunId) != null)
                    return;
                StartAsyncPhase(request);
            }
            catch (Exception ex)
            {
                FinishAsyncRun(request, "error", ex.Message);
            }
        }

        private static void StartAsyncPhase(TestRequest request)
        {
            var testMode = (TestMode)Enum.Parse(typeof(TestMode), request.mode);
            var api = ScriptableObject.CreateInstance<TestRunnerApi>();
            var collector = AttachCollector(request, api);
            api.RetrieveTestList(testMode, rootTest =>
            {
                if (!ReferenceEquals(s_AsyncRun, request) || !ReferenceEquals(m_ActiveCollector, collector))
                    return;
                try
                {
                    var names = CollectTestNames(rootTest, request.filter, request.filterType, request.includeExplicit);
                    if (names.Count == 0)
                    {
                        CompletePhase(request, new TestPhase(), api, collector);
                        return;
                    }
                    api.RegisterCallbacks(collector);
                    request.nativeRunId = api.Execute(new ExecutionSettings(new Filter
                    {
                        testMode = testMode,
                        testNames = names.ToArray()
                    }));
                    SaveRequest(request);
                }
                catch (Exception ex)
                {
                    FinishAsyncRun(request, "error", ex.Message);
                }
            });
        }

        private static TestResultCollector AttachCollector(TestRequest request, TestRunnerApi api)
        {
            var collector = new TestResultCollector();
            m_ActiveApi = api;
            m_ActiveCollector = collector;
            collector.OnRunFinished = () =>
            {
                if (!ReferenceEquals(s_AsyncRun, request) || !ReferenceEquals(m_ActiveCollector, collector))
                    return;
                try
                {
                    var root = collector.RootResult;
                    CompletePhase(request, new TestPhase
                    {
                        duration = root.Duration,
                        passed = root.PassCount,
                        failed = root.FailCount,
                        skipped = root.SkipCount,
                        inconclusive = root.InconclusiveCount,
                        results = collector.Results.ToArray()
                    }, api, collector);
                }
                catch (Exception ex)
                {
                    FinishAsyncRun(request, "error", ex.Message);
                }
            };
            return collector;
        }

        private static void CompletePhase(TestRequest request, TestPhase phase, TestRunnerApi api, TestResultCollector collector)
        {
            UnregisterCollector(api, collector);
            if (request.mode == nameof(TestMode.EditMode))
                request.editMode = phase;
            else
                request.playMode = phase;

            if (request.all && request.mode == nameof(TestMode.EditMode))
            {
                // Commit the EditMode result before Unity can reload for PlayMode.
                request.mode = nameof(TestMode.PlayMode);
                request.previousNativeRunId = request.nativeRunId;
                request.nativeRunId = null;
                SaveRequest(request);
                WriteRunStatus(request, "running");
                return;
            }
            FinishAsyncRun(request, "completed");
        }

        [InitializeOnLoad]
        public static class TestReloader
        {
            static TestReloader()
            {
                EditorApplication.delayCall += CheckForPendingTests;
            }
        }

        public static void CheckForPendingTests()
        {
            if (s_AsyncRun != null || !File.Exists(TestRequestFile))
                return;
            try
            {
                var request = JsonConvert.DeserializeObject<TestRequest>(File.ReadAllText(TestRequestFile));
                if (request == null || request.isSync || string.IsNullOrEmpty(request.runId))
                {
                    CleanupRequestFile();
                    return;
                }
                s_AsyncRun = request;
                EditorApplication.update += PumpAsyncRun;
                if (request.stopStatus != null || string.IsNullOrEmpty(request.nativeRunId))
                    return;

                // The Test Framework resumes its own PlayMode job. Attach to that
                // job's result callbacks, never call Execute a second time.
                if (LegacyJobHolder != null && FindLegacyJob(request.nativeRunId) == null &&
                    !(request.mode == nameof(TestMode.PlayMode) && EditorApplication.isPlayingOrWillChangePlaymode))
                {
                    FinishAsyncRun(request, "interrupted", "The Unity test job did not survive the domain reload.");
                    return;
                }
                var api = ScriptableObject.CreateInstance<TestRunnerApi>();
                api.RegisterCallbacks(AttachCollector(request, api));
            }
            catch (Exception ex)
            {
                if (s_AsyncRun != null)
                    FinishAsyncRun(s_AsyncRun, "error", ex.Message);
                else
                {
                    WriteStatusFile(new { status = "error", message = ex.Message });
                    CleanupRequestFile();
                }
            }
        }

        private static object FindLegacyJob(string nativeRunId)
        {
            if (string.IsNullOrEmpty(nativeRunId) || LegacyJobHolder == null)
                return null;
            var holder = LegacyJobHolder.BaseType.GetProperty("instance", BindingFlags.Static | BindingFlags.Public)?.GetValue(null);
            var jobs = LegacyJobHolder.GetField("TestRuns")?.GetValue(holder) as IEnumerable;
            if (jobs != null)
            {
                foreach (var job in jobs)
                {
                    if (string.Equals(job?.GetType().GetField("guid")?.GetValue(job) as string, nativeRunId, StringComparison.Ordinal))
                        return job;
                }
            }
            return null;
        }

        private static void StopAsyncRun(TestRequest request, string status, string message)
        {
            request.stopStatus = status;
            SaveRequest(request);
            WriteRunStatus(request, status, message);
            InvalidatePreviousRun();
            DrainStoppedRun(request);
        }

        private static void DrainStoppedRun(TestRequest request)
        {
            if (string.IsNullOrEmpty(request.nativeRunId))
            {
                ReleaseAsyncRun(request);
                return;
            }
            if (CancelNativeRun != null)
            {
                CancelNativeRun.Invoke(null, new object[] { request.nativeRunId });
                ReleaseAsyncRun(request);
                return;
            }

            var job = FindLegacyJob(request.nativeRunId);
            if (request.mode == nameof(TestMode.PlayMode) && EditorApplication.isPlaying)
            {
                // The legacy job can leave the registry on reload while its
                // controller is still running tests in the bootstrap scene.
                var controller = LegacyPlayController?.GetMethod("GetController", BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Static)
                    ?.Invoke(null, null) as MonoBehaviour;
                if (controller != null)
                {
                    controller.StopAllCoroutines();
                    EditorApplication.isPlaying = false;
                    return;
                }
            }
            if (job == null)
            {
                ReleaseAsyncRun(request);
                return;
            }
            var runner = job.GetType().GetField("editModeRunner")?.GetValue(job) as UnityEngine.Object;
            if (runner != null && Equals(runner.GetType().GetField("RunFinished")?.GetValue(runner), false))
                runner.GetType().GetMethod("OnRunCancel")?.Invoke(runner, null);
        }

        private static void FinishAsyncRun(TestRequest request, string status, string message = null)
        {
            WriteRunStatus(request, status, message);
            InvalidatePreviousRun();
            ReleaseAsyncRun(request);
            RestoreLiveServerIfDisrupted();
        }

        private static void ReleaseAsyncRun(TestRequest request)
        {
            s_DrainingRunId = request.nativeRunId ?? request.previousNativeRunId;
            s_AsyncRun = null;
            EditorApplication.update -= PumpAsyncRun;
            CleanupRequestFile();
        }

        private static void SaveRequest(TestRequest request)
        {
            File.WriteAllText(TestRequestFile, JsonConvert.SerializeObject(request));
        }

        private static void WriteRunStatus(TestRequest request, string status, string message = null)
        {
            var phases = new[] { request.editMode, request.playMode }.Where(phase => phase != null).ToArray();
            WriteStatusFile(new
            {
                status,
                runId = request.runId,
                mode = request.all ? "All" : request.mode,
                phase = request.mode,
                filter = string.IsNullOrEmpty(request.filter) ? null : $"{request.filterType}: {request.filter}",
                startedAtUtc = request.startedAtUtc,
                duration = Math.Round(phases.Sum(phase => phase.duration), 2),
                message,
                summary = new
                {
                    total = phases.Sum(phase => phase.total),
                    passed = phases.Sum(phase => phase.passed),
                    failed = phases.Sum(phase => phase.failed),
                    skipped = phases.Sum(phase => phase.skipped),
                    inconclusive = phases.Sum(phase => phase.inconclusive)
                },
                phases = new { editMode = request.editMode?.Summary, playMode = request.playMode?.Summary },
                results = phases.SelectMany(phase => phase.results).ToArray()
            });
        }


        #region Utility Methods

        private struct ParsedTestMode
        {
            public bool IsValid { get; set; }
            public bool IsAll { get; set; }
            public TestMode TestMode { get; set; }

            public static ParsedTestMode Invalid => new ParsedTestMode { IsValid = false };
            public static ParsedTestMode All => new ParsedTestMode { IsValid = true, IsAll = true };
            public static ParsedTestMode Single(TestMode mode) => new ParsedTestMode { IsValid = true, IsAll = false, TestMode = mode };
        }

        private static ParsedTestMode ParseTestMode(string mode)
        {
            switch (mode?.ToLower())
            {
                case "editor":
                case "editmode":
                    return ParsedTestMode.Single(TestMode.EditMode);
                case "playmode":
                case "play":
                    return ParsedTestMode.Single(TestMode.PlayMode);
                case "all":
                    return ParsedTestMode.All;
                default:
                    return ParsedTestMode.Invalid;
            }
        }

        private static List<string> CollectTestNames(ITestAdaptor test, string filter, string filterType, bool includeExplicit)
        {
            var testNames = new List<string>();
            CollectTestNamesRecursive(test, testNames, filter, filterType, includeExplicit);
            return testNames;
        }

        private static void CollectTestNamesRecursive(ITestAdaptor test, List<string> testNames, string filter, string filterType, bool includeExplicit)
        {
            if (test == null) return;

            // Skip [Explicit] tests AND [Explicit] fixtures (the whole subtree) unless includeExplicit.
            // Checked at EVERY node — including suite/fixture nodes — because a class-level [Explicit]
            // is not reliably propagated to [UnityTest] leaf RunState by Unity's adaptor; honoring the
            // declaring type's attribute here is what makes class-level [Explicit] actually exclude.
            if (!includeExplicit && IsExplicit(test))
                return;

            // Only collect leaf tests (actual test methods, not fixtures/suites)
            if (!test.HasChildren && test.IsSuite == false)
            {
                // Apply user filter
                if (!ShouldIncludeTest(test, filter, filterType))
                    return;

                testNames.Add(test.FullName);
            }

            // Recurse into children
            if (test.Children != null)
            {
                foreach (var child in test.Children)
                {
                    CollectTestNamesRecursive(child, testNames, filter, filterType, includeExplicit);
                }
            }
        }

        /// <summary>
        /// True if the test, its method, or its declaring fixture type is marked [Explicit].
        /// Class-level [Explicit] must be honored here because Unity does not always propagate it
        /// to leaf RunState (notably for [UnityTest]).
        /// </summary>
        private static bool IsExplicit(ITestAdaptor test)
        {
            if (test.RunState == RunState.Explicit)
                return true;

            var method = test.Method?.MethodInfo;
            if (method != null && method.GetCustomAttributes(true).Any(a => a.GetType().Name == "ExplicitAttribute"))
                return true;

            var type = test.TypeInfo?.Type;
            if (type != null && type.GetCustomAttributes(true).Any(a => a.GetType().Name == "ExplicitAttribute"))
                return true;

            return false;
        }

        private static bool ShouldIncludeTest(ITestAdaptor test, string filter, string filterType)
        {
            if (string.IsNullOrEmpty(filter))
                return true;

            switch (filterType)
            {
                case "assembly":
                    var assemblyName = test.TypeInfo?.Assembly?.GetName()?.Name;
                    return assemblyName != null &&
                           assemblyName.IndexOf(filter, StringComparison.OrdinalIgnoreCase) >= 0;

                case "testname":
                    return test.FullName != null &&
                           test.FullName.IndexOf(filter, StringComparison.OrdinalIgnoreCase) >= 0;

                case "category":
                    return test.Categories != null &&
                           test.Categories.Any(c => c.Equals(filter, StringComparison.OrdinalIgnoreCase));

                default:
                    return true;
            }
        }

        private static void InvalidatePreviousRun()
        {
            if (m_ActiveCollector != null)
            {
                m_ActiveCollector.Cancel();
                UnregisterCollector(m_ActiveApi, m_ActiveCollector);
                Debug.Log("[PipelineTestRunner] Invalidated previous test run");
            }
        }

        /// <summary>
        /// Unregisters a collector from Unity's TestRunnerApi callback registry (process-wide, not
        /// per-instance) and clears the active-run fields if they still point at it.
        /// </summary>
        private static void UnregisterCollector(TestRunnerApi api, TestResultCollector collector)
        {
            if (api != null && collector != null)
            {
                try { api.UnregisterCallbacks(collector); }
                catch (Exception ex)
                {
                    Debug.LogWarning($"[PipelineTestRunner] Could not unregister collector: {ex.Message}");
                }
            }

            if (ReferenceEquals(m_ActiveCollector, collector))
            {
                m_ActiveApi = null;
                m_ActiveCollector = null;
            }
        }

        private static TestExecutionResponse CreateErrorResponse(string message)
        {
            return new TestExecutionResponse
            {
                Success = false,
                Command = "run_tests",
                Error = message,
                Results = new List<TestResult>(),
                Summary = new TestSummary(),
                ExecutedAt = DateTime.UtcNow
            };
        }

        private static ITestResultAdaptor CreateEmptyTestResult()
        {
            // This is a bit of a hack since ITestResultAdaptor is an interface
            // In practice, this case should be rare (no tests matching filter)
            return null;
        }

        /// <summary>
        /// B2 self-heal: after a test run, if a test left the live editor server disrupted (its
        /// listener died, or its discovery descriptor was deleted/clobbered), restart it so the
        /// dogfood loop and subsequent commands keep working instead of wedging the session. No-op
        /// when the server is healthy. Runs at run completion — the watchdog is disabled during the
        /// run (PipelineWatchdogTestGuard), so this is what revives the live server afterwards.
        /// </summary>
        private static void RestoreLiveServerIfDisrupted()
        {
            try
            {
                var server = PipelineServerStartup.Server;
                var root = Path.GetDirectoryName(Application.dataPath);
                var descriptor = Unity.Pipeline.Models.InstanceDescriptor.ReadFromProjectRoot(root);

                var disrupted = server == null
                                || !server.IsRunning
                                || descriptor == null
                                || descriptor.Port != server.Port;

                if (disrupted)
                {
                    Debug.Log("[PipelineTestRunner] Live server was disrupted during the test run — restarting it.");
                    PipelineServerStartup.RestartServer();
                }
            }
            catch (Exception ex)
            {
                Debug.LogWarning($"[PipelineTestRunner] Live server restore check failed: {ex.Message}");
            }
        }

        private static void WriteStatusFile(object data)
        {
            try
            {
                var json = JsonConvert.SerializeObject(data, Formatting.Indented);
                lock (s_TestStatusLock)
                    File.WriteAllText(TestStatusFile, json);
            }
            catch (Exception ex)
            {
                Debug.LogError($"[PipelineTestRunner] Failed to write status file: {ex.Message}");
            }
        }

        private static string ReadStatusFile()
        {
            lock (s_TestStatusLock)
                return File.Exists(TestStatusFile) ? File.ReadAllText(TestStatusFile) : null;
        }

        private static void CleanupRequestFile()
        {
            try { if (File.Exists(TestRequestFile)) File.Delete(TestRequestFile); } catch { }
        }

        /// <summary>
        /// Get current test status (for async mode polling)
        /// </summary>
        /// <returns>The status file's JSON contents, "running" if a request is pending, or null if there's no run.</returns>
        public static string GetTestStatus()
        {
            var status = ReadStatusFile();
            if (status != null)
                return status;
            if (File.Exists(TestRequestFile))
                return JsonConvert.SerializeObject(new { status = "running" });
            return null;
        }

        /// <summary>
        /// Cancel running tests
        /// </summary>
        /// <returns>A status payload confirming cancellation (or that there was nothing to cancel).</returns>
        public static object CancelTests()
        {
            if (s_AsyncRun != null)
            {
                if (s_AsyncRun.stopStatus != null)
                    return new { status = s_AsyncRun.stopStatus, runId = s_AsyncRun.runId };
                var request = s_AsyncRun;
                StopAsyncRun(request, "cancelled", "Test run cancelled.");
                return new { status = "cancelled", runId = request.runId, message = "Test run cancelled." };
            }
            if (m_ActiveCollector == null)
                return new { status = "no_tests", message = "No test run in progress." };

            InvalidatePreviousRun();
            WriteStatusFile(new { status = "cancelled", message = "Test run cancelled." });
            CleanupRequestFile();
            return new { status = "cancelled", message = "Test run cancelled." };
        }

        #endregion

        [Serializable]
        private class TestRequest
        {
            public string filter;
            public string filterType;
            public string mode;
            public bool includeExplicit;
            public bool isSync;
            public string runId;
            public bool all;
            public string nativeRunId;
            public string previousNativeRunId;
            public string stopStatus;
            public DateTime startedAtUtc;
            public DateTime deadlineUtc;
            public TestPhase editMode;
            public TestPhase playMode;
        }

        [Serializable]
        private class TestPhase
        {
            public double duration;
            public int passed;
            public int failed;
            public int skipped;
            public int inconclusive;
            public TestResult[] results = Array.Empty<TestResult>();

            [JsonIgnore] public int total => passed + failed + skipped + inconclusive;
            [JsonIgnore] public object Summary => new { total, passed, failed, skipped, inconclusive };
        }
    }
}
#endif
