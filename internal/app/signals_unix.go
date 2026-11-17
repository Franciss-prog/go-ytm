//go:build !windows

package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// reloadOnSignal calls reload on every SIGUSR1 (spec 08: manual theme
// reload) until ctx ends or the returned stop is called.
func reloadOnSignal(ctx context.Context, reload func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				reload()
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		cancel()
		<-done
	}
}
