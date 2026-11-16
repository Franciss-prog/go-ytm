package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// DefaultSlots and MaxSlots bound the Google multi-login slots probed
// (X-Goog-AuthUser 0..n-1), as in ytm-player.
const (
	DefaultSlots = 10
	MaxSlots     = 30
)

// ClientFactory builds a YouTube Music client for one set of credentials.
type ClientFactory func(ytmusic.Credentials) ytmusic.Client

// Probed is an account found in one slot.
type Probed struct {
	Slot int
	Info ytmusic.AccountInfo
}

// Label renders "Name · @handle · slot N".
func (p Probed) Label() string {
	s := p.Info.Name
	if p.Info.Handle != "" {
		s += " · " + p.Info.Handle
	}
	return fmt.Sprintf("%s · slot %d", s, p.Slot)
}

// ProbeSlots asks YouTube Music who is signed in at each slot 0..slots-1,
// four at a time. Slots that answer "not signed in" are skipped, and an
// account seen in several slots is listed once, at its lowest slot. When
// every slot failed for another reason (e.g. no network), that error is
// returned instead of an empty list.
func ProbeSlots(ctx context.Context, newClient ClientFactory, base ytmusic.Credentials, slots int) ([]Probed, error) {
	slots = min(max(slots, 1), MaxSlots)
	type result struct {
		p   Probed
		err error
	}
	results := make([]result, slots)
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for slot := range slots {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[slot].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			c := base
			c.AuthUser = slot
			info, err := newClient(c).Account(ctx)
			results[slot] = result{Probed{Slot: slot, Info: info}, err}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var found []Probed
	var hardErr error
	seen := map[string]bool{}
	for _, r := range results {
		if r.err != nil {
			if !errors.Is(r.err, ytmusic.ErrUnauthenticated) && hardErr == nil {
				hardErr = r.err
			}
			continue
		}
		key := r.p.Info.ChannelID
		if key == "" {
			key = r.p.Info.Name + "\x00" + r.p.Info.Handle
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		found = append(found, r.p)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Slot < found[j].Slot })
	if len(found) == 0 && hardErr != nil {
		return nil, hardErr
	}
	return found, nil
}

// Renewer re-reads the browser session recorded at setup and returns fresh
// credentials for the same account (silent renewal, spec 03). It refuses
// when the account has no recorded channel id, the source was a manual
// paste, or the account is no longer signed in.
type Renewer struct {
	Store     Store
	Extract   func(ctx context.Context, source string) ([]Cookie, error)
	NewClient ClientFactory
	Slots     int
	Now       func() time.Time
}

// ErrRenewalUnavailable means the session cannot be renewed silently.
var ErrRenewalUnavailable = errors.New("automatic session renewal unavailable; run `ytm setup`")

// Renew implements ytmusic.Options.Renew.
func (r Renewer) Renew(ctx context.Context) (ytmusic.Credentials, error) {
	sess, err := r.Store.Load()
	if err != nil {
		return ytmusic.Credentials{}, err
	}
	acct := sess.Account
	if acct.ChannelID == "" || acct.Source == "" || acct.Source == "manual" {
		return ytmusic.Credentials{}, ErrRenewalUnavailable
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	jar, err := r.Extract(ctx, acct.Source)
	if err != nil {
		return ytmusic.Credentials{}, fmt.Errorf("renew session: %w", err)
	}
	if !HasSession(jar, now()) {
		return ytmusic.Credentials{}, fmt.Errorf("renew session from %s: %w", acct.Source, ErrNoSession)
	}
	base := ytmusic.Credentials{Cookie: CookieHeader(jar, now()), UserAgent: sess.Credentials.UserAgent, AuthUser: acct.Slot}

	// The recorded slot first; browsers can reorder accounts, so fall back
	// to a unique match in any other slot.
	match := func(p Probed) bool { return p.Info.ChannelID == acct.ChannelID }
	chosen := -1
	if info, err := r.NewClient(base).Account(ctx); err == nil && match(Probed{Info: info}) {
		chosen = acct.Slot
	} else {
		probed, err := ProbeSlots(ctx, r.NewClient, base, max(r.Slots, DefaultSlots))
		if err != nil {
			return ytmusic.Credentials{}, fmt.Errorf("renew session: %w", err)
		}
		for _, p := range probed {
			if match(p) {
				if chosen >= 0 {
					return ytmusic.Credentials{}, fmt.Errorf("renew session: account found in several slots: %w", ErrRenewalUnavailable)
				}
				chosen = p.Slot
			}
		}
	}
	if chosen < 0 {
		return ytmusic.Credentials{}, fmt.Errorf("renew session: %s is no longer signed in to %s: %w", acct.Name, acct.Source, ErrRenewalUnavailable)
	}
	base.AuthUser = chosen
	acct.SavedAt = now()
	if err := r.Store.Save(Session{Credentials: base, Account: acct, StreamJar: StreamJar(jar)}); err != nil {
		return ytmusic.Credentials{}, err
	}
	return base, nil
}
