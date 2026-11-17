//go:build windows

package app

import "context"

// reloadOnSignal is a no-op on Windows (no SIGUSR1); use `ytm theme reload`.
func reloadOnSignal(context.Context, func()) (stop func()) { return func() {} }
