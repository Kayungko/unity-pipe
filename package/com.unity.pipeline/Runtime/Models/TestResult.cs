using System;

namespace Unity.Pipeline
{
    /// <summary>
    /// Individual test result information
    /// </summary>
    [Serializable]
    class TestResult
    {
        /// <summary>Fully-qualified test name.</summary>
        public string FullName { get; set; }

        [Newtonsoft.Json.JsonProperty(NullValueHandling = Newtonsoft.Json.NullValueHandling.Ignore)]
        public string Assembly { get; set; }

        [Newtonsoft.Json.JsonProperty(NullValueHandling = Newtonsoft.Json.NullValueHandling.Ignore)]
        public string Mode { get; set; }
        /// <summary>Outcome: Passed, Failed, Skipped, or Inconclusive.</summary>
        public string Status { get; set; } // Passed, Failed, Skipped, Inconclusive
        /// <summary>How long the test took, in seconds.</summary>
        public double Duration { get; set; }
        /// <summary>Failure message, if any.</summary>
        public string Message { get; set; }
        /// <summary>Failure stack trace, if any.</summary>
        public string StackTrace { get; set; }
    }
}