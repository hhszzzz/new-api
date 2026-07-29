package helper

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedStreamReaderDeliversStreamEndingExactlyAtLimit(t *testing.T) {
	payload := strings.Repeat("a", 1024)
	reader := &boundedStreamReader{reader: strings.NewReader(payload), remaining: int64(len(payload)) + 1}

	replayed, err := io.ReadAll(reader)

	require.NoError(t, err)
	assert.Equal(t, payload, string(replayed))
}

func TestBoundedStreamReaderFailsInsteadOfTruncatingPastLimit(t *testing.T) {
	payload := strings.Repeat("a", 1024)
	reader := &boundedStreamReader{reader: strings.NewReader(payload), remaining: 17}

	replayed, err := io.ReadAll(reader)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBufferedStreamTooLarge))
	assert.Len(t, replayed, 17)
}
