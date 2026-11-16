package ytmusic

import (
	"context"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// limiter is a token bucket: rate tokens per second, up to burst. In-house
// (≈40 lines) instead of golang.org/x/time/rate so it runs on clock.Clock
// and tests stay deterministic.
type limiter struct {
	clk   clock.Clock
	rate  float64
	burst float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newLimiter(clk clock.Clock, perSecond float64, burst int) *limiter {
	return &limiter{clk: clk, rate: perSecond, burst: float64(burst), tokens: float64(burst), last: clk.Now()}
}

// wait blocks until a token is available or ctx is done.
func (l *limiter) wait(ctx context.Context) error {
	if l == nil || l.rate <= 0 {
		return ctx.Err()
	}
	for {
		l.mu.Lock()
		now := l.clk.Now()
		if el := now.Sub(l.last); el > 0 {
			l.tokens = min(l.burst, l.tokens+el.Seconds()*l.rate)
		}
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		need := time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
		l.mu.Unlock()

		t := l.clk.NewTimer(max(need, time.Millisecond))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C():
		}
	}
}
