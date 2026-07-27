package common

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomEventRenderSupportsNonStringData(t *testing.T) {
	recorder := httptest.NewRecorder()

	err := (CustomEvent{Data: 42}).Render(recorder)

	require.NoError(t, err)
	assert.Equal(t, "42", recorder.Body.String())
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
}
