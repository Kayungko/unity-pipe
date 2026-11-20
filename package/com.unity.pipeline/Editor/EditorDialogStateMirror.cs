#if UNITY_6000_7_OR_NEWER
using System;
using System.Linq;
using UnityEditor;

namespace Unity.Pipeline.Editor
{
    /// <summary>
    /// Mirrors UnityEditor.EditorDialogEvents into the live pipeline server's DialogStateTracker,
    /// so GET /api/dialog, dialogsDuringExecution, and the dialog busy-gate all observe modal
    /// dialogs with no per-command instrumentation (see the trunk-side design doc). Ambient
    /// dialogs are a live-editor-wide concept, not per-test-server — targets the live server
    /// explicitly, the same way EditorProgressMirror does, so a test server's DialogStateTracker
    /// never receives them.
    /// </summary>
    [InitializeOnLoad]
    internal static class EditorDialogStateMirror
    {
        static EditorDialogStateMirror()
        {
            EditorDialogEvents.dialogWillShow -= OnDialogWillShow;
            EditorDialogEvents.dialogWillShow += OnDialogWillShow;
            EditorDialogEvents.dialogDismissed -= OnDialogDismissed;
            EditorDialogEvents.dialogDismissed += OnDialogDismissed;
        }

        static void OnDialogWillShow(DialogEventInfo info)
        {
            var server = PipelineServerStartup.Server;
            if (server == null)
                return;

            // Still on the main thread here — dialogWillShow fires before the dialog actually
            // blocks it — so this is the last chance to read live Editor state for the dialog
            // busy-gate's editor_status fallback (see SnapshotStatusForDialogGate's doc).
            server.SnapshotStatusForDialogGate();

            server.Dialogs.OnShown(info.Id, info.Source.ToString(), info.Title, info.Message,
                info.ButtonLabels?.ToArray(), info.Level?.ToString(), info.OpenedAtUtc);
        }

        static void OnDialogDismissed(DialogEventInfo info)
        {
            var server = PipelineServerStartup.Server;
            if (server == null)
                return;

            server.Dialogs.OnDismissed(info.Id, DateTime.UtcNow);
        }
    }
}
#endif

#if UNITY_EDITOR_WIN && !UNITY_6000_7_OR_NEWER
namespace Unity.Pipeline.Editor
{
    using System;
    using System.Collections.Generic;
    using System.Runtime.InteropServices;
    using System.Text;

    /// <summary>Reads owned, visible Windows modal dialogs without dispatching to the Editor thread.</summary>
    internal sealed class WindowsEditorDialogMirror
    {
        const uint OwnerWindow = 4;
        readonly uint m_ProcessId = GetCurrentProcessId();
        readonly object m_Gate = new object();
        readonly HashSet<int> m_Reported = new HashSet<int>();

        internal void Refresh(DialogStateTracker tracker)
        {
            lock (m_Gate)
            {
                var seen = new HashSet<int>();
                var now = DateTime.UtcNow;
                var caption = new StringBuilder(512);
                var className = new StringBuilder(128);
                if (!EnumWindows((window, state) =>
                {
                    GetWindowThreadProcessId(window, out var pid);
                    if (pid != m_ProcessId || !IsWindowVisible(window) || !IsWindowEnabled(window))
                        return true;
                    className.Clear();
                    GetClassName(window, className, className.Capacity);
                    if (!string.Equals(className.ToString(), "#32770", StringComparison.Ordinal))
                        return true;

                    var owner = GetWindow(window, OwnerWindow);
                    if (owner == IntPtr.Zero || IsWindowEnabled(owner))
                        return true;
                    GetWindowThreadProcessId(owner, out var ownerPid);
                    if (ownerPid != m_ProcessId)
                        return true;

                    caption.Clear();
                    SendMessageTimeout(window, 0x000D, (UIntPtr)(uint)caption.Capacity, caption, 0x0002, 50, out _);
                    if (caption.Length == 0)
                        return true;
                    var id = unchecked((int)window.ToInt64());
                    seen.Add(id);
                    if (m_Reported.Add(id))
                        tracker.OnShown(id, "windows", caption.ToString(), null, Array.Empty<string>(), null, now);
                    return true;
                }, IntPtr.Zero))
                    return;

                foreach (var id in m_Reported)
                    if (!seen.Contains(id))
                        tracker.OnDismissed(id, now);
                m_Reported.IntersectWith(seen);
            }
        }

        delegate bool WindowVisitor(IntPtr window, IntPtr state);
        [DllImport("kernel32.dll")] static extern uint GetCurrentProcessId();
        [DllImport("user32.dll")] static extern bool EnumWindows(WindowVisitor visitor, IntPtr state);
        [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr window);
        [DllImport("user32.dll")] static extern bool IsWindowEnabled(IntPtr window);
        [DllImport("user32.dll")] static extern IntPtr GetWindow(IntPtr window, uint command);
        [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
        [DllImport("user32.dll", CharSet = CharSet.Unicode)]
        static extern int GetClassName(IntPtr window, StringBuilder className, int length);
        [DllImport("user32.dll", CharSet = CharSet.Unicode)]
        static extern IntPtr SendMessageTimeout(IntPtr window, uint message, UIntPtr capacity, StringBuilder caption,
            uint flags, uint timeoutMs, out UIntPtr result);
    }
}
#endif
