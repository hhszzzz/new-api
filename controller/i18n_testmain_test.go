package controller

import (
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
)

// TestMain initializes the i18n bundle once for the whole controller test
// package. Handlers render user-facing messages through common.TranslateMessage,
// which i18n.Init() replaces with the real translator; without this the default
// stub returns the raw message key and message-content assertions fail.
func TestMain(m *testing.M) {
	if err := i18n.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "i18n init:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
