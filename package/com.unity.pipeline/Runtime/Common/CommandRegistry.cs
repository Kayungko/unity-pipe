using System;
using System.Collections.Generic;
using System.Linq;
using System.Reflection;
using System.Threading;
using Unity.Pipeline.HotReload;
using UnityEngine;
#if UNITY_6000_3_OR_NEWER
using UnityEngine.Assemblies;
#endif
#if UNITY_6000_5_OR_NEWER
using Unity.Scripting.LifecycleManagement;
#endif

namespace Unity.Pipeline.Commands
{
    /// <summary>
    /// Registry for discovering and managing CLI commands via pluggable discovery mechanism.
    /// Scans for methods marked with [CliCommand] attribute across all assemblies.
    /// Based on unity-tools ToolRegistry patterns adapted for Pipeline requirements.
    /// </summary>
#if UNITY_6000_5_OR_NEWER
    [NoAutoStaticsCleanup]
#endif
    public static class CommandRegistry
    {
        private static IReadOnlyList<CommandInfo> m_CachedCommands;
        private static volatile IReadOnlyList<CommandInfo> m_Catalog;
        private static ICommandDiscovery m_Discovery;
        private static readonly object m_DiscoveryLock = new object();
        private static readonly Dictionary<string, CommandInfo> m_Registered =
            new Dictionary<string, CommandInfo>(StringComparer.Ordinal);

        /// <summary>
        /// Register a project-owned method on the current domain's command catalog.
        /// Dispose the returned handle when the owner goes away.
        /// </summary>
        public static IDisposable RegisterCommand(string name, string description, Delegate handler,
            bool mainThreadRequired = true, bool runtimeOnly = false,
            CommandEffect effect = CommandEffect.Unknown, params string[] tags)
        {
            if (string.IsNullOrWhiteSpace(name) || name.Any(ch => char.IsWhiteSpace(ch) || char.IsControl(ch)))
                throw new ArgumentException("Command names must be nonempty single tokens.", nameof(name));
            if (string.IsNullOrWhiteSpace(description))
                throw new ArgumentException("A command description is required.", nameof(description));
            if (handler == null)
                throw new ArgumentNullException(nameof(handler));
            if (tags != null && tags.Any(tag => string.IsNullOrWhiteSpace(tag) ||
                tag.Split('/').Any(string.IsNullOrWhiteSpace)))
                throw new ArgumentException("Tags must be nonempty slash-separated paths.", nameof(tags));
            var method = handler.Method;
            if (handler.GetInvocationList().Length != 1 || method.IsStatic != (handler.Target == null) ||
                method.ReturnType.IsByRef || method.GetParameters().Any(p => p.ParameterType.IsByRef))
                throw new ArgumentException("Use a single static or bound instance handler without ref or out parameters.",
                    nameof(handler));

            var parameters = DiscoverParameters(method).ToArray();
            var keys = new HashSet<string>(StringComparer.OrdinalIgnoreCase) { "token" };
            if (parameters.Any(p => string.IsNullOrWhiteSpace(p.Name) || !keys.Add(p.Name)))
                throw new ArgumentException("Command parameter names must be unique, nonempty, and cannot be 'token'.",
                    nameof(handler));

            var command = new CommandInfo(name, description, mainThreadRequired, method, parameters,
                runtimeOnly, tags?.Distinct(StringComparer.Ordinal).ToArray(), method.DeclaringType?.Assembly.GetName().Name)
            {
                Effect = effect,
                InvocationTarget = handler.Target,
                IsDynamic = true
            };
            lock (m_DiscoveryLock)
            {
                if (DiscoverCommands().Any(existing => existing.Name == name))
                    throw new ArgumentException($"A command named '{name}' is already registered.", nameof(name));
                m_Registered.Add(name, command);
                m_Catalog = null;
            }
            return new Registration(command);
        }

        /// <summary>Remove a dynamically registered command. Attribute commands remain in the catalog.</summary>
        public static bool UnregisterCommand(string name) => RemoveRegistration(name, null);

        static bool RemoveRegistration(string name, CommandInfo expected)
        {
            if (string.IsNullOrEmpty(name))
                return false;
            lock (m_DiscoveryLock)
            {
                if (!m_Registered.TryGetValue(name, out var command) || (expected != null && !ReferenceEquals(command, expected)))
                    return false;
                m_Registered.Remove(name);
                m_Catalog = null;
                return true;
            }
        }

        internal static bool IsRegistered(CommandInfo command)
        {
            lock (m_DiscoveryLock)
                return m_Registered.TryGetValue(command.Name, out var current) && ReferenceEquals(current, command);
        }

        sealed class Registration : IDisposable
        {
            CommandInfo m_Command;

            public Registration(CommandInfo command) => m_Command = command;

            public void Dispose()
            {
                var command = Interlocked.Exchange(ref m_Command, null);
                if (command != null)
                    RemoveRegistration(command.Name, command);
            }
        }

        /// <summary>
        /// Set the command discovery mechanism.
        /// Editor assembly provides TypeCache-based discovery, Runtime uses reflection fallback.
        /// </summary>
        /// <param name="discovery">The discovery mechanism to use.</param>
        internal static void SetDiscovery(ICommandDiscovery discovery)
        {
            lock (m_DiscoveryLock)
            {
                m_Discovery = discovery;
                ClearCache();
            }
        }

        /// <summary>
        /// Discover all CLI commands marked with [CliCommand] attribute.
        /// Uses injected discovery mechanism (TypeCache in Editor, reflection in Runtime).
        /// Results are cached until domain reload.
        /// </summary>
        /// <returns>All discovered commands.</returns>
        internal static IEnumerable<CommandInfo> DiscoverCommands()
        {
            var catalog = m_Catalog;
            if (catalog != null)
            {
                return catalog;
            }

            lock (m_DiscoveryLock)
            {
                if (m_CachedCommands == null)
                {
                    var discovered = DiscoverCommandsInternal().ToList();

                    // Also discover hot reload methods when commands are discovered
                    DiscoverHotReloadMethods();

                    m_CachedCommands = discovered;
                }
                if (m_Catalog == null)
                    m_Catalog = m_CachedCommands.Concat(m_Registered.Values).ToArray();
                return m_Catalog;
            }
        }

        /// <summary>
        /// Clear the command cache. Called automatically on domain reload.
        /// Can be called manually for testing or dynamic command registration.
        /// </summary>
        internal static void ClearCache()
        {
            lock (m_DiscoveryLock)
            {
                m_CachedCommands = null;
                m_Catalog = null;
            }
        }

        /// <summary>
        /// Internal implementation of command discovery using injected discovery mechanism.
        /// </summary>
        private static IEnumerable<CommandInfo> DiscoverCommandsInternal()
        {
            var commands = new List<CommandInfo>();

            try
            {
                // Use injected discovery mechanism if available, otherwise fallback to reflection
                IEnumerable<MethodInfo> methods;

                if (m_Discovery != null)
                {
                    methods = m_Discovery.GetMethodsWithAttribute<CliCommandAttribute>();
                }
                else
                {
                    // Fallback: Use reflection to scan loaded assemblies
                    methods = GetMethodsWithAttributeViaReflection<CliCommandAttribute>();
                }

                foreach (var method in methods)
                {
                    try
                    {
                        var commandInfo = CreateCommandInfo(method);
                        if (commandInfo != null)
                        {
                            commands.Add(commandInfo);
                        }
                    }
                    catch (Exception ex)
                    {
                        Debug.LogWarning($"Failed to register command from method {method.DeclaringType?.Name}.{method.Name}: {ex.Message}");
                    }
                }
            }
            catch (Exception ex)
            {
                Debug.LogError($"Failed to discover commands: {ex.Message}");
            }

            return commands;
        }

        /// <summary>
        /// Fallback command discovery using reflection when TypeCache is not available.
        /// </summary>
        private static IEnumerable<MethodInfo> GetMethodsWithAttributeViaReflection<T>() where T : Attribute
        {
            var methods = new List<MethodInfo>();

            try
            {
                var assemblies = PipelineUtils.GetLoadedAssemblies();

                foreach (var assembly in assemblies)
                {
                    // Skip system assemblies for performance
                    if (IsSystemAssembly(assembly))
                        continue;

                    try
                    {
                        foreach (var type in assembly.GetTypes())
                        {
                            foreach (var method in type.GetMethods(BindingFlags.Static | BindingFlags.Public | BindingFlags.NonPublic))
                            {
                                if (method.GetCustomAttribute<T>() != null)
                                {
                                    methods.Add(method);
                                }
                            }
                        }
                    }
                    catch (ReflectionTypeLoadException)
                    {
                        // Skip assemblies that can't be loaded
                        continue;
                    }
                }
            }
            catch (Exception ex)
            {
                Debug.LogWarning($"Reflection-based command discovery failed: {ex.Message}");
            }

            return methods;
        }

        /// <summary>
        /// Check if assembly is a system assembly that should be skipped during discovery.
        /// </summary>
        private static bool IsSystemAssembly(Assembly assembly)
        {
            var name = assembly.GetName().Name;
            return name.StartsWith("System.") ||
                   name.StartsWith("Microsoft.") ||
                   name.StartsWith("mscorlib") ||
                   name.StartsWith("netstandard") ||
                   name.Equals("UnityEngine") ||
                   name.Equals("UnityEditor");
        }

        /// <summary>
        /// Create CommandInfo from a method with CliCommand attribute.
        /// </summary>
        private static CommandInfo CreateCommandInfo(MethodInfo method)
        {
            var commandAttr = method.GetCustomAttribute<CliCommandAttribute>();
            if (commandAttr == null)
                return null;

            // Validate method is static (required for CLI commands).
            // Any static method may be registered regardless of accessibility
            // (public, internal, or private) — invocation goes through MethodInfo.Invoke.
            if (!method.IsStatic)
            {
                Debug.LogWarning($"Command method {method.DeclaringType?.Name}.{method.Name} must be static");
                return null;
            }

            // Discover parameters
            var parameters = DiscoverParameters(method).ToList();

            return new CommandInfo(
                commandAttr.Name,
                commandAttr.Description,
                commandAttr.MainThreadRequired,
                method,
                parameters,
                commandAttr.RuntimeOnly,
                commandAttr.Tags,
                method.DeclaringType?.Assembly.GetName().Name)
            {
                Effect = commandAttr.Effect
            };
        }

        /// <summary>
        /// Discover parameter information from method parameters.
        /// </summary>
        private static IEnumerable<CommandParameterInfo> DiscoverParameters(MethodInfo method)
        {
            foreach (var param in method.GetParameters())
            {
                var argAttr = param.GetCustomAttribute<CliArgAttribute>();

                // Parameters without CliArg attribute get default metadata
                var name = argAttr?.Name ?? param.Name;
                var description = argAttr?.Description ?? $"Parameter: {param.Name}";
                var required = argAttr?.Required ?? !param.HasDefaultValue;
                var defaultValue = param.HasDefaultValue ? param.DefaultValue : argAttr?.DefaultValue;
                var arbitraryKeys = argAttr?.ArbitraryKeys ?? false;

                yield return new CommandParameterInfo(name, description, required, param.ParameterType, defaultValue, arbitraryKeys);
            }
        }

        /// <summary>
        /// Discover and register methods marked with [HotReloadWithOverrides] attribute.
        /// Called during command discovery to populate the hot reload registry.
        /// </summary>
        private static void DiscoverHotReloadMethods()
        {
            try
            {
                // Use injected discovery mechanism if available, otherwise fallback to reflection
                IEnumerable<MethodInfo> methods;

                if (m_Discovery != null)
                {
                    methods = m_Discovery.GetMethodsWithAttribute<HotReloadWithOverridesAttribute>();
                }
                else
                {
                    // Fallback: Use reflection to scan loaded assemblies
                    methods = GetMethodsWithAttributeViaReflection<HotReloadWithOverridesAttribute>();
                }

                foreach (var method in methods)
                {
                    try
                    {
                        var hotReloadAttr = method.GetCustomAttribute<HotReloadWithOverridesAttribute>();
                        if (hotReloadAttr != null)
                        {
                            HotReloadRegistry.RegisterReloadableMethod(method, hotReloadAttr);
                        }
                    }
                    catch (Exception ex)
                    {
                        Debug.LogWarning($"Failed to register hot reload method {method.DeclaringType?.Name}.{method.Name}: {ex.Message}");
                    }
                }
            }
            catch (Exception ex)
            {
                Debug.LogError($"Failed to discover hot reload methods: {ex.Message}");
            }
        }
    }
}
