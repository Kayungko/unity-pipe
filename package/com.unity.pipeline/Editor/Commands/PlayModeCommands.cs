#if UNITY_EDITOR
using System;
using Unity.Pipeline.Commands;
using UnityEditor;
using UnityEngine;

namespace Unity.Pipeline.Editor.Commands
{
    /// <summary>
    /// Commands for controlling Unity Editor play mode state.
    /// Enables automation workflows to control Editor play/stop/pause from CLI tools.
    /// </summary>
    [InitializeOnLoad]
    static class PlayModeCommands
    {
        private const string RestoreBackground = "Unity.Pipeline.PlayMode.RestoreBackground";
        private const string OriginalBackground = "Unity.Pipeline.PlayMode.OriginalBackground";

        static PlayModeCommands()
        {
            EditorApplication.playModeStateChanged += OnPlayModeChanged;
            if (SessionState.GetBool(RestoreBackground, false))
                EditorApplication.update += MaintainBackground;
        }

        [CliCommand("editor_play", "Enter Unity Editor play mode", Tags = new[] { "editor/playmode" })]
        public static string EnterPlayMode(
            [CliArg("run_in_background", "Keep the game running while the Editor is unfocused for this Play Mode session.", DefaultValue = true)] bool runInBackground = true)
        {
            if (EditorUtility.scriptCompilationFailed || EditorApplication.isCompiling)
                throw new InvalidOperationException("Cannot enter Play Mode while scripts have compiler errors or are compiling.");
            if (EditorApplication.isPlaying)
                return "Already in play mode";
            if (EditorApplication.isPlayingOrWillChangePlaymode)
                return "Play mode transition already requested";

            if (runInBackground)
            {
                SessionState.SetBool(OriginalBackground, Application.runInBackground);
                SessionState.SetBool(RestoreBackground, true);
                Application.runInBackground = true;
                EditorApplication.update += MaintainBackground;
            }

            try
            {
                EditorApplication.isPlaying = true;
                return "Entered play mode";
            }
            catch
            {
                RestoreOriginalBackground();
                throw;
            }
        }

        private static void OnPlayModeChanged(PlayModeStateChange state)
        {
            if (state == PlayModeStateChange.EnteredPlayMode && SessionState.GetBool(RestoreBackground, false))
                Application.runInBackground = true;
            if (state == PlayModeStateChange.EnteredEditMode)
                RestoreOriginalBackground();
        }

        private static void MaintainBackground()
        {
            if (EditorApplication.isPlaying)
                return;
            if (!EditorApplication.isPlayingOrWillChangePlaymode)
                RestoreOriginalBackground();
        }

        private static void RestoreOriginalBackground()
        {
            EditorApplication.update -= MaintainBackground;
            if (!SessionState.GetBool(RestoreBackground, false))
                return;

            Application.runInBackground = SessionState.GetBool(OriginalBackground, false);
            SessionState.EraseBool(RestoreBackground);
            SessionState.EraseBool(OriginalBackground);
        }

        [CliCommand("editor_stop", "Exit Unity Editor play mode", Tags = new[] { "editor/playmode" })]
        public static string ExitPlayMode()
        {
            if (!EditorApplication.isPlaying)
            {
                Debug.LogWarning("Editor is already in edit mode");
                return "Already in edit mode";
            }

            EditorApplication.isPlaying = false;
            return "Exited play mode";
        }

        [CliCommand("editor_pause", "Toggle pause state of Unity Editor play mode", Tags = new[] { "editor/playmode" })]
        public static string PausePlayMode()
        {
            if (!EditorApplication.isPlaying)
            {
                Debug.LogWarning("Cannot pause - Editor is not in play mode");
                return "Cannot pause - not in play mode";
            }

            EditorApplication.isPaused = !EditorApplication.isPaused;
            string state = EditorApplication.isPaused ? "paused" : "unpaused";
            return $"Play mode {state}";
        }
    }
}
#endif
