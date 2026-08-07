package common

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingEventWriter struct {
	header http.Header
}

func (w *failingEventWriter) Header() http.Header {
	return w.header
}

func (w *failingEventWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *failingEventWriter) WriteHeader(int) {}

func TestCustomEventRenderSupportsNonStringData(t *testing.T) {
	recorder := httptest.NewRecorder()

	err := (CustomEvent{Data: 42}).Render(recorder)

	require.NoError(t, err)
	assert.Equal(t, "42", recorder.Body.String())
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
}

func TestCustomEventRenderPropagatesWriteFailure(t *testing.T) {
	writer := &failingEventWriter{header: make(http.Header)}

	err := (CustomEvent{Data: "data: terminal"}).Render(writer)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}
