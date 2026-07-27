package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidPayMethodsJSONPreservesCurrentMethods(t *testing.T) {
	original := PayMethods2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdatePayMethodsByJsonString(original))
	})

	require.NoError(t, UpdatePayMethodsByJsonString(`[{"type":"before"}]`))
	require.Error(t, UpdatePayMethodsByJsonString(`{`))

	assert.JSONEq(t, `[{"type":"before"}]`, PayMethods2JsonString())
}
