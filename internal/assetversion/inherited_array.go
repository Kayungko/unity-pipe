package assetversion

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var arrayPropertyIndex = regexp.MustCompile(`\.Array\.data\[([0-9]+)\]`)

const maxPrefabArrayElements = 100000

type prefabArray struct {
	length int
	zero   any
}

func arrayZero(sample any) any {
	switch sample.(type) {
	case Reference:
		return Reference{FileID: "0"}
	case json.Number:
		return json.Number("0")
	case bool:
		return false
	case string:
		return ""
	}
	return nil
}

func captureArrays(lengths map[string]int, fields map[string]any) map[string]*prefabArray {
	if len(lengths) == 0 {
		return nil
	}
	arrays := make(map[string]*prefabArray, len(lengths))
	for path, length := range lengths {
		arrays[path] = &prefabArray{length: length, zero: arrayZero(fields[path+"/0"])}
	}
	return arrays
}

// arrayIndex returns the first index within a flattened YAML sequence.
func arrayIndex(array, member string) (int, bool) {
	tail, ok := strings.CutPrefix(member, array+"/")
	if !ok {
		return 0, false
	}
	part, _, _ := strings.Cut(tail, "/")
	index, err := strconv.Atoi(part)
	return index, err == nil && index >= 0
}

func (r *inheritedRecord) resizeArray(path string, length int) bool {
	array := r.arrays[path]
	if array == nil || length < 0 || length > maxPrefabArrayElements {
		return false
	}
	if length < array.length {
		for field := range r.fields {
			if index, member := arrayIndex(path, field); member && index >= length {
				delete(r.fields, field)
			}
		}
		for child := range r.arrays {
			if index, member := arrayIndex(path, child); member && index >= length {
				delete(r.arrays, child)
			}
		}
	}
	if length == 0 {
		r.fields[path] = []any{}
		r.unknown = slices.DeleteFunc(r.unknown, func(key string) bool {
			return key == path || strings.HasPrefix(key, path+"/")
		})
	} else {
		delete(r.fields, path)
	}
	array.length = length
	return true
}

// A grown array can omit serialized overrides for newly inserted default values.
// Leave compound values uncertain unless Unity serialized enough information.
func (r *inheritedRecord) fillArray(path string) bool {
	array := r.arrays[path]
	if array == nil || array.length == 0 {
		return true
	}
	materialized := make(map[int]bool)
	for key := range r.fields {
		if index, member := arrayIndex(path, key); member {
			materialized[index] = true
		}
	}
	for i := 0; i < array.length; i++ {
		if materialized[i] {
			continue
		}
		if array.zero == nil {
			return false
		}
		r.fields[path+"/"+strconv.Itoa(i)] = array.zero
	}
	return true
}

func (r *inheritedRecord) arrayForElement(key string) *prefabArray {
	// Use the immediate parent path, including when the sequence is nested in a struct.
	split := strings.LastIndex(key, "/")
	if split <= 0 {
		return nil
	}
	index, err := strconv.Atoi(key[split+1:])
	array := r.arrays[key[:split]]
	if array == nil || err != nil || index < 0 || index >= array.length {
		return nil
	}
	return array
}

func (r *inheritedRecord) outsideArray(key string) bool {
	for path, array := range r.arrays {
		if index, member := arrayIndex(path, key); member && index >= array.length {
			return true
		}
	}
	return false
}
