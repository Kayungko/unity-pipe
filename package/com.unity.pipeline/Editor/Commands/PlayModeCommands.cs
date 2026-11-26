#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.Linq;
using System.Reflection;
using Unity.Pipeline.Commands;
using UnityEditor;
using UnityEditor.SceneManagement;
using UnityEngine;
using UnityEngine.SceneManagement;

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

    static class EditorQuitCommands
    {
        const double ReplyDelaySeconds = 0.75;
        static readonly PropertyInfo StageUnsavedChanges = typeof(PrefabStage).GetProperty(
            "hasUnsavedChanges", BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic);
        static readonly MethodInfo ClearSceneDirtiness = typeof(EditorSceneManager).GetMethod(
            "ClearSceneDirtiness", BindingFlags.Static | BindingFlags.Public | BindingFlags.NonPublic, null, new[] { typeof(Scene) }, null);
        static bool s_ExitPending;

        [CliCommand("editor_quit", "Quit the Editor. Dirty scenes or an open dirty Prefab require an explicit save or discard choice.",
            Effect = CommandEffect.Destructive, Tags = new[] { "editor" })]
        public static object Quit(
            [CliArg("save", "Save dirty open scenes and the open Prefab before exiting.")] bool save = false,
            [CliArg("discard", "Exit without saving changes.")] bool discard = false,
            [CliArg("exit_code", "Process exit code for the Editor.")] int exitCode = 0)
        {
            if (save && discard)
                throw new ArgumentException("Choose save or discard.");
            if (s_ExitPending)
                throw new InvalidOperationException("An Editor exit is already scheduled.");
            if (!discard && InPlayMode())
                throw new InvalidOperationException("Stop Play Mode before quitting, or pass discard=true.");

            var dirtyStage = GetDirtyStage();
            var scenes = GetDirtyScenes();
            var unsaved = scenes.Select(DescribeScene).ToList();
            if (dirtyStage != null)
                unsaved.Add(dirtyStage.assetPath);
            if (unsaved.Count > 0 && !save && !discard)
                throw new InvalidOperationException($"Unsaved changes in {string.Join(", ", unsaved)}. Pass save=true or discard=true.");
            if (discard && scenes.Count != 0 && ClearSceneDirtiness == null)
                throw new NotSupportedException("This Editor cannot discard dirty scenes through the command.");

            if (save)
            {
                if (scenes.Any(scene => string.IsNullOrEmpty(scene.path)))
                    throw new InvalidOperationException("A dirty untitled scene needs a path. Save it explicitly before quitting.");
                foreach (var scene in scenes)
                {
                    if (!EditorSceneManager.SaveScene(scene))
                        throw new InvalidOperationException($"Could not save '{scene.path}'. The Editor is still open.");
                }
                if (dirtyStage != null)
                {
                    PrefabUtility.SaveAsPrefabAsset(dirtyStage.prefabContentsRoot, dirtyStage.assetPath, out var saved);
                    if (!saved)
                        throw new InvalidOperationException($"Could not save '{dirtyStage.assetPath}'. The Editor is still open.");
                    dirtyStage.ClearDirtiness();
                }
            }

            s_ExitPending = true;
            var exitAt = EditorApplication.timeSinceStartup + ReplyDelaySeconds;
            EditorApplication.update += ExitAfterReply;
            return new
            {
                quitting = true,
                exitCode,
                saved = save ? unsaved.ToArray() : Array.Empty<string>(),
                discarded = discard ? unsaved.ToArray() : Array.Empty<string>()
            };

            void ExitAfterReply()
            {
                if (EditorApplication.timeSinceStartup < exitAt)
                    return;
                EditorApplication.update -= ExitAfterReply;
                s_ExitPending = false;
                if (!discard && (InPlayMode() || GetDirtyStage() != null || GetDirtyScenes().Count != 0))
                {
                    Debug.LogWarning("Editor exit cancelled because the Play Mode state or unsaved contents changed after the reply.");
                    return;
                }
                if (discard)
                {
                    // Unity 2022 can still display a save prompt during Exit.
                    // Clear the accepted dirty state only after replying, without writing assets.
                    var currentScenes = GetDirtyScenes();
                    if (currentScenes.Count != 0 && ClearSceneDirtiness == null)
                    {
                        Debug.LogWarning("Editor exit cancelled because dirty scenes cannot be discarded through the command.");
                        return;
                    }
                    GetDirtyStage()?.ClearDirtiness();
                    foreach (var scene in currentScenes)
                        ClearSceneDirtiness.Invoke(null, new object[] { scene });
                }
                EditorApplication.Exit(exitCode);
            }
        }

        static bool InPlayMode() => EditorApplication.isPlaying || EditorApplication.isPlayingOrWillChangePlaymode;

        static PrefabStage GetDirtyStage()
        {
            var stage = PrefabStageUtility.GetCurrentPrefabStage();
            if (stage == null)
                return null;
            var dirty = StageUnsavedChanges != null ? (bool)StageUnsavedChanges.GetValue(stage) : stage.scene.isDirty;
            return dirty ? stage : null;
        }

        static List<Scene> GetDirtyScenes()
        {
            var scenes = new List<Scene>();
            for (var i = 0; i < SceneManager.sceneCount; i++)
            {
                var scene = SceneManager.GetSceneAt(i);
                if (scene.isLoaded && scene.isDirty && !EditorSceneManager.IsPreviewScene(scene))
                    scenes.Add(scene);
            }
            return scenes;
        }

        static string DescribeScene(Scene scene) => string.IsNullOrEmpty(scene.path) ? $"Untitled scene '{scene.name}'" : scene.path;
    }
}
#endif
