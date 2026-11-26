#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using Newtonsoft.Json.Linq;
using Unity.Pipeline.Commands;
using Unity.Pipeline.Editor.Authoring;
using Unity.Pipeline.Editor.Commands.GameObjects;
using Unity.Pipeline.Models;
using UnityEditor;
using UnityEngine;
using Object = UnityEngine.Object;

namespace Unity.Pipeline.Editor.Commands.Scripts
{
    /// <summary>
    /// Read and write serialized ([SerializeField] / public) fields on a component or asset through
    /// Unity's <see cref="SerializedObject"/> model (CLI-195).
    ///
    /// Why SerializedObject and not plain reflection: it honours Unity's serialization rules (private
    /// [SerializeField] fields, property drawers, prefab overrides), marks the object dirty correctly,
    /// and integrates with Undo. Writes are wrapped in an <see cref="AuthoringUndoScope"/> so an
    /// agent's change reverts as one step.
    ///
    /// Field addressing uses Unity's native SerializedProperty path syntax, so array elements are
    /// reachable directly: "myArray.Array.data[2]" sets the third element; "myArray.Array.size" sets
    /// the array length. Nested fields use dotted paths ("settings.speed").
    /// </summary>
    static class SerializedFieldCommands
    {
        [CliCommand("set_serialized_field",
            "Set a serialized field on a component/asset. Supports primitives, enums, Vector/Color/Rect/Bounds, " +
            "object references (value = an ObjectRef: asset by guid/fileId/path or scene object by instanceId/hierarchyPath), " +
            "and array elements via 'name.Array.data[i]' (or 'name.Array.size' to resize).",
            Tags = new[] { "scripts", "gameobjects/components" })]
        public static AuthoringResult SetSerializedField(
            [CliArg("target", "Reference to the component or asset to modify (globalId/path/guid/instanceId/hierarchyPath). May be a GameObject when 'component' is given.", Required = true)] ObjectRef target,
            [CliArg("field", "SerializedProperty path, e.g. 'speed', 'settings.speed', or 'waypoints.Array.data[0]'.", Required = true)] string field,
            [CliArg("value", "JSON value to assign. For object references pass an ObjectRef object (or null to clear). For enums pass the value name.", Required = true, ArbitraryKeys = true)] JToken value,
            [CliArg("component", "Component type name on the target GameObject (e.g. 'Rigidbody'). Use when 'target' is a GameObject; omit when 'target' is already a component handle.")] string component = null,
            [CliArg("enum_mode", "Numeric enum interpretation: index (default) or underlying value. Applies to nested fields.")] string enumMode = "index")
        {
            if (target == null || target.IsEmpty)
                throw new ArgumentException("set_serialized_field 'target' is required.");
            if (string.IsNullOrWhiteSpace(field))
                throw new ArgumentException("set_serialized_field 'field' is required.");

            var obj = ResolveSerializable(target, component);
            if (obj is Component && EditorUtility.IsPersistent(obj)
                && AssetDatabase.GetAssetPath(obj).EndsWith(".prefab", StringComparison.OrdinalIgnoreCase))
                throw new ArgumentException("Use set_prefab_serialized_field for a component in a prefab asset.");

            using (var so = new SerializedObject(obj))
            {
                var prop = so.FindProperty(field);
                if (prop == null)
                    throw new ArgumentException(
                        $"Field '{field}' was not found on '{obj.GetType().Name}'. " +
                        "Use a SerializedProperty path (e.g. 'speed', 'settings.speed', 'items.Array.data[0]').");

                SerializedPropertyConverter.SetValue(prop, value, ParseEnumMode(enumMode));
                using (new AuthoringUndoScope($"Set {field}"))
                {
                    Undo.RegisterCompleteObjectUndo(obj, $"Set {field}");
                    so.ApplyModifiedProperties();
                    EditorUtility.SetDirty(obj);
                }
            }

            var result = ObjectResolver.Describe(obj) ?? new AuthoringResult { Type = obj.GetType().Name };
            return result;
        }

        [CliCommand("set_prefab_serialized_field", "Edit and save a field on a Prefab component in isolated Prefab contents.", Tags = new[] { "scripts", "prefabs" })]
        public static AuthoringResult SetPrefabSerializedField(
            [CliArg("target", "Prefab asset path and child, e.g. Assets/UI.prefab:Root/Child.", Required = true)] string target,
            [CliArg("component", "Component type on the Prefab object.", Required = true)] string component,
            [CliArg("field", "Unity SerializedProperty path.", Required = true)] string field,
            [CliArg("value", "JSON value to write.", Required = true, ArbitraryKeys = true)] JToken value,
            [CliArg("enum_mode", "Numeric enum interpretation: index (default) or underlying value. Applies to nested fields.")] string enumMode = "index")
        {
            var useEnumValue = ParseEnumMode(enumMode);
            if (!ObjectResolver.TrySplitPrefabPath(target, out var path, out var childPath))
                throw new ArgumentException("Use a path such as 'Assets/UI.prefab:Root/Child' for target.");
            var assetPath = ProjectPaths.Resolve(path, out var error);
            if (assetPath == null)
                throw new ArgumentException(error);
            if (AssetDatabase.LoadAssetAtPath<GameObject>(assetPath) == null)
                throw new ArgumentException($"No Prefab at '{assetPath}'.");

            var root = PrefabUtility.LoadPrefabContents(assetPath);
            try
            {
                var node = ObjectResolver.FindPrefabChild(root, childPath, out error);
                if (node == null)
                    throw new ArgumentException(error);
                var obj = ResolveSerializable(new ObjectRef { InstanceId = PipelineUtils.GetObjectId(node) }, component);
                using (var data = new SerializedObject(obj))
                {
                    var property = data.FindProperty(field);
                    if (property == null)
                        throw new ArgumentException($"Field '{field}' was not found on '{obj.GetType().Name}'.");
                    SerializedPropertyConverter.SetValue(property, value, useEnumValue);
                    data.ApplyModifiedPropertiesWithoutUndo();
                }
                PrefabUtility.SaveAsPrefabAsset(root, assetPath, out var saved);
                if (!saved)
                    throw new InvalidOperationException($"Failed to save '{assetPath}'.");
            }
            finally
            {
                PrefabUtility.UnloadPrefabContents(root);
            }

            var persistent = ResolveSerializable(new ObjectRef { Path = $"{assetPath}:{childPath}" }, component);
            return ObjectResolver.Describe(persistent);
        }

        private static bool ParseEnumMode(string mode)
        {
            if (mode == "index") return false;
            if (mode == "value") return true;
            throw new ArgumentException("enum_mode must be 'index' or 'value'.", nameof(mode));
        }

        [CliCommand("get_serialized_fields",
            "Read serialized fields of a component/asset. Returns each top-level field's name, type and value " +
            "(object references are returned as re-usable handles). Pass 'field' to read a single SerializedProperty path. " +
            "Pass format='value' to return just the value(s) — a token-efficient read for scalars.",
            Tags = new[] { "scripts", "gameobjects/components" })]
        public static object GetSerializedFields(
            [CliArg("target", "Reference to the component or asset to read (globalId/path/guid/instanceId/hierarchyPath). May be a GameObject when 'component' is given.", Required = true)] ObjectRef target,
            [CliArg("field", "Optional single SerializedProperty path to read (e.g. 'speed' or 'items.Array.data[0]'). Omit to read all top-level fields.")] string field = null,
            [CliArg("component", "Component type name on the target GameObject (e.g. 'Rigidbody'). Use when 'target' is a GameObject; omit when 'target' is already a component handle.")] string component = null,
            [CliArg("format", "Output shape. Omit (default) for a full descriptor per field {name,path,propertyType,isArray,arrayLength,value}; 'value' returns just the value(s): the raw value for a single 'field', or a {name: value} map for all fields.")] string format = null)
        {
            if (target == null || target.IsEmpty)
                throw new ArgumentException("get_serialized_fields 'target' is required.");

            // Value projection (AUTHAPI-21): drop the per-field descriptor and return only the value(s)
            // so a scalar read costs bytes, not an envelope. Anything other than 'value' is the default
            // descriptor form, so existing callers are unaffected.
            var valueOnly = string.Equals(format, "value", StringComparison.OrdinalIgnoreCase);

            var obj = ResolveSerializable(target, component);
            var so = new SerializedObject(obj);

            if (!string.IsNullOrWhiteSpace(field))
            {
                var prop = so.FindProperty(field);
                if (prop == null)
                    throw new ArgumentException($"Field '{field}' was not found on '{obj.GetType().Name}'.");

                if (valueOnly)
                    return SerializedPropertyConverter.GetValue(prop);

                return new
                {
                    type = obj.GetType().Name,
                    fields = new[] { DescribeProperty(prop) }
                };
            }

            // Each mode gets only its own container (the other stays null).
            var fields = valueOnly ? null : new List<object>();
            var values = valueOnly ? new Dictionary<string, object>() : null;
            var iterator = so.GetIterator();
            // enterChildren=true on the first MoveNext to step into the top level; then false to stay
            // at the top level and skip nested children (callers drill in via an explicit 'field').
            var enterChildren = true;
            while (iterator.NextVisible(enterChildren))
            {
                enterChildren = false;

                // m_Script is Unity's hidden back-reference to the MonoScript; not a user field.
                if (iterator.propertyPath == "m_Script")
                    continue;

                if (valueOnly)
                {
                    // Degrade per-field rather than failing the whole map read (mirrors
                    // get_component_properties' per-property guard in ComponentCommands).
                    try { values[iterator.name] = SerializedPropertyConverter.GetValue(iterator); }
                    catch (Exception ex) { values[iterator.name] = $"<error:{ex.Message}>"; }
                }
                else
                {
                    fields.Add(DescribeProperty(iterator));
                }
            }

            return valueOnly ? (object)values : new { type = obj.GetType().Name, fields };
        }

        /// <summary>
        /// Resolve a handle to an object that a SerializedObject can wrap: a Component or an asset.
        ///
        /// Two addressing forms (CLI-225):
        ///   * The handle already points at a Component (or asset) → use it directly. An optional
        ///     <paramref name="component"/> type name is validated against it as a guard.
        ///   * The handle points at a GameObject AND <paramref name="component"/> is given → look up
        ///     the matching component(s) on that GameObject. Exactly one match is used; MULTIPLE
        ///     same-type components are an error that lists each instanceId so the agent can re-address
        ///     by instanceId; zero matches is a clear error.
        ///
        /// A GameObject with no <paramref name="component"/> is rejected (a GameObject's "fields" are
        /// its components), with a hint to pass --component or a specific component handle.
        ///
        /// This mirrors <see cref="ComponentCommands.ResolveComponent"/>'s GO-path+type pattern, but
        /// deliberately errors on MULTIPLE matches (that helper returns the first) so a set/get never
        /// silently targets the wrong instance.
        /// </summary>
        private static Object ResolveSerializable(ObjectRef target, string component = null)
        {
            if (!ObjectResolver.TryResolve(target, out var obj, out var error))
                throw new ArgumentException($"Could not resolve target: {error}");

            // The handle already points at a specific Component: honour it directly. A 'component' here
            // is only a constraint to validate against, never a reason to re-fetch (which could pick a
            // different same-type instance and ignore the handle).
            if (obj is Component directComponent)
            {
                if (!string.IsNullOrWhiteSpace(component))
                {
                    var requestedType = TypeResolver.ResolveComponentType(component);
                    if (requestedType == null)
                        throw new ArgumentException($"Could not resolve component type '{component}'.");
                    if (!requestedType.IsInstanceOfType(directComponent))
                        throw new ArgumentException(
                            $"Target resolves to a {directComponent.GetType().Name}, which is not a '{requestedType.Name}'.");
                }

                return directComponent;
            }

            if (obj is GameObject go)
            {
                if (string.IsNullOrWhiteSpace(component))
                    throw new ArgumentException(
                        $"Target '{target}' is a GameObject. Pass --component <TypeName> to pick a component on it, " +
                        "or target a specific component directly (use its instanceId/globalId), " +
                        "to read or set serialized fields.");

                var componentType = TypeResolver.ResolveComponentType(component);
                if (componentType == null)
                    throw new ArgumentException($"Could not resolve component type '{component}'.");

                var matches = go.GetComponents(componentType).Where(c => c != null).ToArray();
                if (matches.Length == 0)
                    throw new ArgumentException(
                        $"GameObject '{go.name}' has no component of type '{componentType.Name}'.");

                if (matches.Length > 1)
                {
                    var sb = new StringBuilder();
                    sb.Append($"GameObject '{go.name}' has {matches.Length} components of type '{componentType.Name}'. ")
                      .Append("Re-target a specific one by its instanceId: ");
                    for (int i = 0; i < matches.Length; i++)
                    {
                        if (i > 0) sb.Append(", ");
                        sb.Append($"instanceId {PipelineUtils.GetObjectId(matches[i])}");
                    }
                    sb.Append('.');
                    throw new ArgumentException(sb.ToString());
                }

                return matches[0];
            }

            // Not a Component, not a GameObject — an asset (e.g. ScriptableObject) handled directly.
            // A 'component' filter is meaningless for an asset; reject it rather than silently ignore it
            // (a supplied component here signals a misrouted or misspelled request).
            if (!string.IsNullOrWhiteSpace(component))
                throw new ArgumentException(
                    $"Target '{target}' resolved to a {obj.GetType().Name} (an asset), which has no components. " +
                    "Omit --component when targeting an asset directly.");

            return obj;
        }

        private static object DescribeProperty(SerializedProperty prop)
        {
            return new
            {
                name = prop.name,
                path = prop.propertyPath,
                propertyType = prop.propertyType.ToString(),
                isArray = prop.isArray && prop.propertyType != SerializedPropertyType.String,
                arrayLength = (prop.isArray && prop.propertyType != SerializedPropertyType.String) ? prop.arraySize : (int?)null,
                value = SerializedPropertyConverter.GetValue(prop)
            };
        }
    }
}
#endif
