package common

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}

func TestWalkJsonStringsPreservesObjectAndArrayOrder(t *testing.T) {
	type visitedString struct {
		path  string
		value string
	}
	var visited []visitedString
	err := WalkJsonStrings(strings.NewReader(`{"second":"two","items":[{"first":"one"},"loose"],"last":"three"}`), func(path []string, value string) error {
		visited = append(visited, visitedString{path: strings.Join(path, "."), value: value})
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []visitedString{
		{path: "second", value: "two"},
		{path: "items.first", value: "one"},
		{path: "items", value: "loose"},
		{path: "last", value: "three"},
	}, visited)
}

func TestWalkJsonStringsRejectsTrailingJSON(t *testing.T) {
	err := WalkJsonStrings(strings.NewReader(`{"value":"one"} {"value":"two"}`), func([]string, string) error { return nil })
	require.Error(t, err)
}
