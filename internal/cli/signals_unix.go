//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// shutdownSignals end the player cleanly (spec 01). SIGINT only arrives
// from outside: in raw mode Ctrl+C is a key press.
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
