package common

import (
	"reflect"

	"github.com/QuantumNous/new-api/common"
)

// MergeNativeRequestBody applies only changes made by structured processing to
// the original wire object. Unknown native fields survive at every unchanged
// level, including content parts and tool definitions.
func MergeNativeRequestBody(original, before, after []byte) ([]byte, error) {
	var wire, old, new any
	if err := common.Unmarshal(original, &wire); err != nil {
		return nil, err
	}
	if err := common.Unmarshal(before, &old); err != nil {
		return nil, err
	}
	if err := common.Unmarshal(after, &new); err != nil {
		return nil, err
	}
	if reflect.DeepEqual(old, new) {
		return original, nil
	}
	return common.Marshal(mergeNativeValue(wire, old, new))
}

func mergeNativeValue(wire, before, after any) any {
	if reflect.DeepEqual(before, after) {
		return wire
	}
	oldObject, oldOK := before.(map[string]any)
	newObject, newOK := after.(map[string]any)
	wireObject, wireOK := wire.(map[string]any)
	if oldOK && newOK && wireOK {
		for key, oldValue := range oldObject {
			if newValue, ok := newObject[key]; ok {
				if !reflect.DeepEqual(oldValue, newValue) {
					wireObject[key] = mergeNativeValue(wireObject[key], oldValue, newValue)
				}
			} else {
				delete(wireObject, key)
			}
		}
		for key, newValue := range newObject {
			if _, existed := oldObject[key]; !existed {
				wireObject[key] = newValue
			}
		}
		return wireObject
	}
	oldArray, oldOK := before.([]any)
	newArray, newOK := after.([]any)
	wireArray, wireOK := wire.([]any)
	if oldOK && newOK && wireOK && len(wireArray) == len(oldArray) {
		if len(newArray) == len(oldArray) {
			for i := range oldArray {
				wireArray[i] = mergeNativeValue(wireArray[i], oldArray[i], newArray[i])
			}
			return wireArray
		}
		if added := len(newArray) - len(oldArray); added > 0 {
			if reflect.DeepEqual(newArray[added:], oldArray) {
				return append(append([]any{}, newArray[:added]...), wireArray...)
			}
			if reflect.DeepEqual(newArray[:len(oldArray)], oldArray) {
				return append(wireArray, newArray[len(oldArray):]...)
			}
		}
	}
	return after
}
