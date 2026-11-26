#if UNITY_EDITOR
using System;
using System.Collections.Generic;
using System.Globalization;
using System.Reflection;
using Newtonsoft.Json.Linq;
using Unity.Pipeline.Editor.Authoring;
using Unity.Pipeline.Models;
using UnityEditor;
using UnityEngine;
using Object = UnityEngine.Object;

namespace Unity.Pipeline.Editor.Commands.Scripts
{
    /// <summary>
    /// Bridges agent-supplied JSON values and Unity's <see cref="SerializedProperty"/> model for the
    /// set_serialized_field / get_serialized_fields commands (CLI-195).
    ///
    /// Supported property kinds: integers, floats, bools, strings, enums, Vector2/3/4, Color,
    /// Quaternion, Rect, Bounds, and object references. Object references accept an <see cref="ObjectRef"/>
    /// handle (resolved via <see cref="ObjectResolver"/>) — assets by GUID/fileId or path, and scene
    /// objects by instanceId/hierarchyPath. Arrays are addressed element-by-element via a path like
    /// "myArray.Array.data[2]" (Unity's native SerializedProperty path syntax) — see the commands.
    /// </summary>
    static class SerializedPropertyConverter
    {
        /// <summary>
        /// Assign a JSON value to a serialized property, converting by the property's type. Throws
        /// <see cref="ArgumentException"/> with a clear message when the value can't be coerced or the
        /// property type isn't supported. The caller is responsible for ApplyModifiedProperties.
        /// </summary>
        public static void SetValue(SerializedProperty prop, JToken value, bool useEnumValue = false)
        {
            // Whole-array assignment: an array/list field (Generic + isArray) set from a JSON array —
            // resize, then recurse per element (handles primitive and object-reference arrays alike).
            // Element-by-element addressing via "field.Array.data[i]" / "field.Array.size" still works.
            if (prop.isArray && prop.propertyType != SerializedPropertyType.String)
            {
                if (!(value is JArray array))
                    throw new ArgumentException(
                        $"Field '{prop.propertyPath}' is an array; provide a JSON array of elements.");

                prop.arraySize = array.Count;
                for (int i = 0; i < array.Count; i++)
                    SetValue(prop.GetArrayElementAtIndex(i), array[i], useEnumValue);
                return;
            }

            switch (prop.propertyType)
            {
                case SerializedPropertyType.Integer:
                    prop.longValue = value.ToObject<long>();
                    break;
                case SerializedPropertyType.Boolean:
                    prop.boolValue = value.ToObject<bool>();
                    break;
                case SerializedPropertyType.Float:
                    prop.doubleValue = value.ToObject<double>();
                    break;
                case SerializedPropertyType.String:
                    prop.stringValue = value.ToObject<string>();
                    break;
                case SerializedPropertyType.Enum:
                    SetEnum(prop, value, useEnumValue);
                    break;
                case SerializedPropertyType.Vector2:
                    prop.vector2Value = ToVector2(value);
                    break;
                case SerializedPropertyType.Vector3:
                    prop.vector3Value = ToVector3(value);
                    break;
                case SerializedPropertyType.Vector4:
                    prop.vector4Value = ToVector4(value);
                    break;
                case SerializedPropertyType.Quaternion:
                    var v4 = ToVector4(value);
                    prop.quaternionValue = new Quaternion(v4.x, v4.y, v4.z, v4.w);
                    break;
                case SerializedPropertyType.Color:
                    prop.colorValue = ToColor(value);
                    break;
                case SerializedPropertyType.Rect:
                    prop.rectValue = ToRect(value);
                    break;
                case SerializedPropertyType.Bounds:
                    prop.boundsValue = ToBounds(value);
                    break;
                case SerializedPropertyType.ObjectReference:
                    prop.objectReferenceValue = ResolveObjectReference(value);
                    break;
                case SerializedPropertyType.ArraySize:
                    // Resizing an array via "<field>.Array.size" — the property is an int under the hood.
                    prop.intValue = value.ToObject<int>();
                    break;
                case SerializedPropertyType.Generic:
                    if (!(value is JObject members))
                        throw new ArgumentException($"Field '{prop.propertyPath}' requires a JSON object.");
                    foreach (var member in members.Properties())
                    {
                        var child = prop.FindPropertyRelative(member.Name);
                        if (child == null)
                            throw new ArgumentException($"Field '{prop.propertyPath}' has no member '{member.Name}'.");
                        SetValue(child, member.Value, useEnumValue);
                    }
                    break;
                default:
                    throw new ArgumentException(
                        $"Field '{prop.propertyPath}' has unsupported type '{prop.propertyType}'. " +
                        "Supported: int, float, bool, string, enum, Vector2/3/4, Quaternion, Color, Rect, Bounds, object reference, array size.");
            }
        }

        /// <summary>
        /// Read a serialized property into a plain object suitable for JSON serialization. Object
        /// references are described via <see cref="ObjectResolver.Describe"/> so the agent gets a
        /// re-usable handle; arrays are returned as a length plus per-element values.
        /// </summary>
        public static object GetValue(SerializedProperty prop)
        {
            // Array/list fields are returned as a JSON array of their elements so they round-trip with
            // SetValue. (String also reports isArray, hence the exclusion.)
            if (prop.isArray && prop.propertyType != SerializedPropertyType.String)
            {
                var elements = new JArray();
                for (int i = 0; i < prop.arraySize; i++)
                {
                    var v = GetValue(prop.GetArrayElementAtIndex(i));
                    elements.Add(v == null ? JValue.CreateNull() : JToken.FromObject(v));
                }

                return elements;
            }

            switch (prop.propertyType)
            {
                case SerializedPropertyType.Integer:
                    return prop.longValue;
                case SerializedPropertyType.Boolean:
                    return prop.boolValue;
                case SerializedPropertyType.Float:
                    return prop.doubleValue;
                case SerializedPropertyType.String:
                    return prop.stringValue;
                case SerializedPropertyType.Enum:
                    return prop.enumValueIndex >= 0 && prop.enumValueIndex < prop.enumNames.Length
                        ? prop.enumNames[prop.enumValueIndex]
                        : (object)prop.intValue;
                case SerializedPropertyType.Vector2:
                    return new { x = prop.vector2Value.x, y = prop.vector2Value.y };
                case SerializedPropertyType.Vector3:
                    return new { x = prop.vector3Value.x, y = prop.vector3Value.y, z = prop.vector3Value.z };
                case SerializedPropertyType.Vector4:
                    return new { x = prop.vector4Value.x, y = prop.vector4Value.y, z = prop.vector4Value.z, w = prop.vector4Value.w };
                case SerializedPropertyType.Quaternion:
                    return new { x = prop.quaternionValue.x, y = prop.quaternionValue.y, z = prop.quaternionValue.z, w = prop.quaternionValue.w };
                case SerializedPropertyType.Color:
                    return new { r = prop.colorValue.r, g = prop.colorValue.g, b = prop.colorValue.b, a = prop.colorValue.a };
                case SerializedPropertyType.Rect:
                    return new { x = prop.rectValue.x, y = prop.rectValue.y, width = prop.rectValue.width, height = prop.rectValue.height };
                case SerializedPropertyType.Bounds:
                    var b = prop.boundsValue;
                    return new { center = new { x = b.center.x, y = b.center.y, z = b.center.z }, size = new { x = b.size.x, y = b.size.y, z = b.size.z } };
                case SerializedPropertyType.ObjectReference:
                    return ObjectResolver.Describe(prop.objectReferenceValue);
                case SerializedPropertyType.ArraySize:
                    return prop.intValue;
                case SerializedPropertyType.Generic:
                case SerializedPropertyType.ManagedReference:
                    if (prop.propertyType == SerializedPropertyType.ManagedReference && prop.managedReferenceValue == null)
                        return null;
                    var fields = new JObject();
                    var cursor = prop.Copy();
                    var end = cursor.GetEndProperty();
                    var traverseChildren = true;
                    while (cursor.NextVisible(traverseChildren) && !SerializedProperty.EqualContents(cursor, end))
                    {
                        var childValue = GetValue(cursor);
                        fields[cursor.name] = childValue == null ? JValue.CreateNull() : JToken.FromObject(childValue);
                        traverseChildren = false;
                    }
                    return fields;
                default:
                    // Unknown/unsupported types are reported by kind rather than failing a whole read.
                    return new { unsupported = prop.propertyType.ToString() };
            }
        }

        private static void SetEnum(SerializedProperty prop, JToken value, bool useEnumValue)
        {
            if (value.Type == JTokenType.String)
            {
                var name = value.ToObject<string>();
                var idx = FindEnumName(prop, name);
                if (idx >= 0)
                {
                    prop.enumValueIndex = idx;
                    return;
                }
                if (name.IndexOfAny(new[] { '|', ',' }) >= 0)
                {
                    SetFlags(prop, name);
                    return;
                }
                throw InvalidEnum(prop, name);
            }
            if (useEnumValue)
            {
                SetEnumNumber(prop, value);
                return;
            }
            // Keep the original numeric index semantics when enum_mode is omitted.
            var index = value.ToObject<int>();
            if (index < 0 || index >= prop.enumNames.Length)
                throw new ArgumentException(
                    $"Enum index {index} is out of range for '{prop.propertyPath}' (valid 0..{prop.enumNames.Length - 1}). " +
                    $"Valid values: {string.Join(", ", prop.enumNames)}.");
            prop.enumValueIndex = index;
        }

        private static void SetEnumNumber(SerializedProperty prop, JToken value)
        {
            if (value.Type != JTokenType.Integer)
                throw InvalidEnum(prop, value);
            try
            {
                var type = FindManagedEnum(prop);
                if (type == null)
                {
                    // A built-in property can have no corresponding managed field.
                    prop.intValue = value.ToObject<int>();
                    if (prop.enumValueIndex < 0)
                        throw InvalidEnum(prop, value);
                    return;
                }

                var number = value.ToObject<long>();
                var underlying = Convert.ChangeType(number, Enum.GetUnderlyingType(type), CultureInfo.InvariantCulture);
                if (type.IsDefined(typeof(FlagsAttribute), false))
                {
                    long defined = 0;
                    foreach (var constant in Enum.GetValues(type))
                        defined |= Convert.ToInt64(constant, CultureInfo.InvariantCulture);
                    if ((number & ~defined) != 0)
                        throw InvalidEnum(prop, value);
                }
                else if (!Enum.IsDefined(type, underlying))
                {
                    throw InvalidEnum(prop, value);
                }
                prop.longValue = number;
            }
            catch (OverflowException)
            {
                throw InvalidEnum(prop, value);
            }
        }

        private static void SetFlags(SerializedProperty prop, string text)
        {
            var type = FindManagedEnum(prop);
            if (type == null || !type.IsDefined(typeof(FlagsAttribute), false))
                throw InvalidEnum(prop, text);
            long mask = 0;
            foreach (var name in text.Split('|', ','))
            {
                var index = FindEnumName(prop, name.Trim());
                if (index < 0)
                    throw InvalidEnum(prop, text);
                mask |= Convert.ToInt64(Enum.Parse(type, prop.enumNames[index]), CultureInfo.InvariantCulture);
            }
            prop.longValue = mask;
        }

        private static Type FindManagedEnum(SerializedProperty prop)
        {
            var type = prop.serializedObject.targetObject.GetType();
            var path = prop.propertyPath.Split('.');
            for (var i = 0; i < path.Length && type != null; i++)
            {
                if (path[i] == "Array" && i + 1 < path.Length && path[i + 1].StartsWith("data[", StringComparison.Ordinal))
                {
                    if (type.IsArray)
                        type = type.GetElementType();
                    else
                        type = type.IsGenericType && type.GetGenericTypeDefinition() == typeof(List<>) ? type.GetGenericArguments()[0] : null;
                    i++;
                    continue;
                }
                FieldInfo field = null;
                for (var declaring = type; declaring != null && field == null; declaring = declaring.BaseType)
                    field = declaring.GetField(path[i], BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.DeclaredOnly);
                type = field?.FieldType;
            }
            return type?.IsEnum == true ? type : null;
        }

        private static int FindEnumName(SerializedProperty prop, string name)
        {
            var index = Array.IndexOf(prop.enumNames, name);
            return index >= 0 ? index : Array.IndexOf(prop.enumDisplayNames, name);
        }

        private static ArgumentException InvalidEnum(SerializedProperty prop, object value) =>
            new ArgumentException($"Enum '{value}' is invalid for '{prop.propertyPath}'. Valid names: {string.Join(", ", prop.enumNames)}.");

        private static Object ResolveObjectReference(JToken value)
        {
            // An explicit null clears the reference; anything else MUST resolve — never silently no-op
            // (an unresolved handle used to be dropped, so the command reported success while assigning
            // nothing).
            if (value == null || value.Type == JTokenType.Null)
                return null;

            var handle = value.ToObject<ObjectRef>();
            if (handle == null || handle.IsEmpty)
                throw new ArgumentException(
                    $"'{value}' is not a recognized object reference handle. " +
                    "Provide a path, guid, globalId, or instanceId (or null to clear).");

            if (!ObjectResolver.TryResolve(handle, out var obj, out var error))
                throw new ArgumentException($"Could not resolve object reference: {error}");

            return obj;
        }

        private static float[] Components(JToken value, string[] names, bool defaultAlpha = false)
        {
            var values = new float[names.Length];
            if (defaultAlpha)
                values[3] = 1;
            if (value is JObject obj)
            {
                // Preserve the named-object defaults used by existing callers.
                for (var i = 0; i < names.Length; i++)
                {
                    var member = obj[names[i]];
                    if (member != null)
                        values[i] = member.ToObject<float>();
                }
                return values;
            }

            var array = value as JArray;
            var csv = value?.Type == JTokenType.String ? value.Value<string>().Split(',') : null;
            var count = array?.Count ?? csv?.Length ?? 0;
            if (count != names.Length && !(defaultAlpha && count == 3))
                throw new ArgumentException($"Provide {string.Join(", ", names)} as an object, numeric array, or comma-separated numbers.");

            for (var i = 0; i < count; i++)
            {
                float number;
                if (array != null)
                {
                    if (array[i].Type != JTokenType.Integer && array[i].Type != JTokenType.Float)
                        throw new ArgumentException($"'{names[i]}' must be a finite number.");
                    number = array[i].Value<float>();
                }
                else if (!float.TryParse(csv[i], NumberStyles.Float, CultureInfo.InvariantCulture, out number))
                {
                    throw new ArgumentException($"'{names[i]}' must be a finite number.");
                }
                if (float.IsNaN(number) || float.IsInfinity(number))
                    throw new ArgumentException($"'{names[i]}' must be a finite number.");
                values[i] = number;
            }
            return values;
        }

        private static Vector2 ToVector2(JToken value)
        {
            var v = Components(value, new[] { "x", "y" });
            return new Vector2(v[0], v[1]);
        }

        private static Vector3 ToVector3(JToken value)
        {
            var v = Components(value, new[] { "x", "y", "z" });
            return new Vector3(v[0], v[1], v[2]);
        }

        private static Vector4 ToVector4(JToken value)
        {
            var v = Components(value, new[] { "x", "y", "z", "w" });
            return new Vector4(v[0], v[1], v[2], v[3]);
        }

        private static Color ToColor(JToken value)
        {
            var v = Components(value, new[] { "r", "g", "b", "a" }, defaultAlpha: true);
            return new Color(v[0], v[1], v[2], v[3]);
        }

        private static Rect ToRect(JToken value)
        {
            var v = Components(value, new[] { "x", "y", "width", "height" });
            return new Rect(v[0], v[1], v[2], v[3]);
        }

        private static Bounds ToBounds(JToken t)
        {
            if (!(t is JObject obj))
                throw new ArgumentException(
                    $"Expected a JSON object with 'center' and 'size' components but received a " +
                    $"'{t?.Type.ToString() ?? "null"}'.");

            var center = obj["center"] != null ? ToVector3(obj["center"]) : Vector3.zero;
            var size = obj["size"] != null ? ToVector3(obj["size"]) : Vector3.zero;
            return new Bounds(center, size);
        }
    }
}
#endif
