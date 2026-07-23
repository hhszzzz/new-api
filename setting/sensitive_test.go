package setting

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSensitiveWordsFromStringIgnoresBlankEntries(t *testing.T) {
	original := SensitiveWordsToString()
	t.Cleanup(func() { SensitiveWordsFromString(original) })

	SensitiveWordsFromString(" blocked \n\n\t\n another ")

	assert.Equal(t, []string{"blocked", "another"}, SensitiveWordsSnapshot())
	assert.Equal(t, "blocked\nanother", SensitiveWordsToString())
}

func TestSensitiveWordsSnapshotIsStableDuringUpdates(t *testing.T) {
	original := SensitiveWordsToString()
	t.Cleanup(func() { SensitiveWordsFromString(original) })
	SensitiveWordsFromString("first")

	snapshot := SensitiveWordsSnapshot()
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		SensitiveWordsFromString("second")
	}()
	waitGroup.Wait()

	require.Len(t, snapshot, 1)
	assert.Equal(t, "first", snapshot[0])
	assert.Equal(t, []string{"second"}, SensitiveWordsSnapshot())
}
