package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidTopupGroupRatioJSONPreservesCurrentRatios(t *testing.T) {
	original := TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateTopupGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateTopupGroupRatioByJSONString(`{"before":1.5}`))
	require.Error(t, UpdateTopupGroupRatioByJSONString(`{`))

	assert.JSONEq(t, `{"before":1.5}`, TopupGroupRatio2JSONString())
}
