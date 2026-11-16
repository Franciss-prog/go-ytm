// Package ytmusic is a small InnerTube client for YouTube Music: the
// endpoints ytm-go needs, normalized into Track/AlbumRef/… values.
//
// Request shapes, constants and parser paths are ported from ytmusicapi
// (MIT, https://github.com/sigma67/ytmusicapi, pinned at commit
// 86fcd17ee3ff0d19bc807558d4e1a637df395143, 2026-11-12); see NOTICE.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// Client is the YouTube Music API used by the rest of the app. It grows
// per milestone (docs/specs/03-ytmusic-api.md); M2 covers search and the
// account lookup used by `ytm setup`.
type Client interface {
	// Search returns up to limit results. Filtered searches follow
	// continuation pages until limit is reached.
	Search(ctx context.Context, q string, f SearchFilter, limit int) (SearchResults, error)
	// SearchSuggestions returns query completions for q.
	SearchSuggestions(ctx context.Context, q string) ([]string, error)
	// Account returns the signed-in account, or ErrUnauthenticated.
	Account(ctx context.Context) (AccountInfo, error)
}

// Sentinel errors. Match with errors.Is.
var (
	// ErrUnauthenticated means the request needs a valid signed-in session.
	ErrUnauthenticated = errors.New("ytmusic: not signed in")
	// ErrRateLimited means YouTube Music answered 429; see HTTPError.RetryAfter.
	ErrRateLimited = errors.New("ytmusic: rate limited")
)

// HTTPError is a non-2xx InnerTube response.
type HTTPError struct {
	Endpoint   string
	Status     int
	Message    string        // error.message from the JSON body, if any
	RetryAfter time.Duration // from Retry-After on 429
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("ytmusic: %s: HTTP %d", e.Endpoint, e.Status)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Is maps status codes onto the package sentinels.
func (e *HTTPError) Is(target error) bool {
	switch target {
	case ErrUnauthenticated:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	}
	return false
}

// DefaultBaseURL is the InnerTube API root for YouTube Music.
const DefaultBaseURL = Origin + "/youtubei/v1/"

// webAPIKey is the public key the YouTube Music web client sends; ytmusicapi
// adds it to browser-authenticated requests (constants.py YTM_PARAMS_KEY).
const webAPIKey = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"

// Options configure an API client. Zero values pick the defaults noted.
type Options struct {
	HTTP        *http.Client  // default: a client with no overall timeout (per-call Timeout applies)
	BaseURL     string        // default DefaultBaseURL
	Credentials *Credentials  // nil: anonymous
	BrandID     string        // onBehalfOfUser for brand accounts
	Language    string        // hl, default "en"
	Region      string        // gl; "" or "ZZ" omit it
	Timeout     time.Duration // per call, default 15 s
	Retries     int           // retries for idempotent reads on 5xx/network errors; <0 disables, 0 means 2
	RatePerSec  float64       // token bucket rate, default 5
	Burst       int           // token bucket burst, default 10
	Clock       clock.Clock   // default clock.Real
	Logger      *slog.Logger  // default: discard
	// Renew, if set, is called once when a request fails with
	// ErrUnauthenticated; on success the request is retried with the new
	// credentials (silent session renewal, spec 03).
	Renew func(ctx context.Context) (Credentials, error)
}

// API is the HTTP implementation of Client. Close it to cancel shared
// in-flight requests.
type API struct {
	o     Options
	lim   *limiter
	group singleflight.Group

	ctx    context.Context // parent of shared (singleflight) calls
	cancel context.CancelFunc

	mu      sync.RWMutex
	creds   *Credentials
	renewMu sync.Mutex
}

var _ Client = (*API)(nil)

// New returns an API client.
func New(o Options) *API {
	if o.HTTP == nil {
		o.HTTP = &http.Client{}
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if o.Language == "" {
		o.Language = "en"
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.Retries == 0 {
		o.Retries = 2
	}
	if o.RatePerSec == 0 {
		o.RatePerSec = 5
	}
	if o.Burst <= 0 {
		o.Burst = 10
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &API{o: o, lim: newLimiter(o.Clock, o.RatePerSec, o.Burst), ctx: ctx, cancel: cancel}
	if o.Credentials != nil {
		c := *o.Credentials
		a.creds = &c
	}
	return a
}

// Close cancels shared in-flight requests. The client must not be used after.
func (a *API) Close() error {
	a.cancel()
	return nil
}

// Authenticated reports whether the client has credentials.
func (a *API) Authenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.creds != nil
}

// SetCredentials replaces the credentials (nil: anonymous).
func (a *API) SetCredentials(c *Credentials) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c == nil {
		a.creds = nil
		return
	}
	cc := *c
	a.creds = &cc
}

func (a *API) credentials() *Credentials {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.creds
}

// Search implements Client.
func (a *API) Search(ctx context.Context, q string, f SearchFilter, limit int) (SearchResults, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return SearchResults{}, errors.New("ytmusic: empty search query")
	}
	if limit <= 0 {
		limit = 20
	}
	body := map[string]any{"query": q}
	if p := searchParams(f); p != "" {
		body["params"] = p
	}
	var p parser
	resp, err := a.read(ctx, "search", body, "")
	if err != nil {
		return SearchResults{}, err
	}
	res := p.parseSearch(resp, f)
	// Only filtered searches have continuations (ytmusicapi mixins/search.py).
	for f != FilterAll && res.Continuation != "" && len(res.Items) < limit {
		tok := res.Continuation
		resp, err := a.read(ctx, "search", body, "&ctoken="+tok+"&continuation="+tok+"&type=next")
		if err != nil {
			return SearchResults{}, err
		}
		page := p.parseSearchContinuation(resp, f)
		if len(page.Items) == 0 {
			res.Continuation = ""
			break
		}
		res.Items = append(res.Items, page.Items...)
		res.Continuation = page.Continuation
	}
	if len(res.Items) > limit {
		res.Items = res.Items[:limit]
	}
	a.logShapes("search", p)
	return res, nil
}

// SearchSuggestions implements Client.
func (a *API) SearchSuggestions(ctx context.Context, q string) ([]string, error) {
	resp, err := a.read(ctx, "music/get_search_suggestions", map[string]any{"input": q}, "")
	if err != nil {
		return nil, err
	}
	return parseSuggestions(resp), nil
}

// Account implements Client.
func (a *API) Account(ctx context.Context) (AccountInfo, error) {
	if a.credentials() == nil {
		return AccountInfo{}, ErrUnauthenticated
	}
	var info AccountInfo
	err := a.withRenew(ctx, func() error {
		resp, err := a.call(ctx, "account/account_menu", map[string]any{}, "", true)
		if err != nil {
			return err
		}
		var ok bool
		if info, ok = parseAccount(resp); !ok {
			return fmt.Errorf("account menu has no signed-in account: %w", ErrUnauthenticated)
		}
		return nil
	})
	return info, err
}

func (a *API) logShapes(op string, p parser) {
	if len(p.unknown) > 0 {
		a.o.Logger.Debug("ytmusic: unknown response shapes", "op", op, "count", len(p.unknown), "shapes", p.unknown)
	}
}

// read is an idempotent call with silent renewal.
func (a *API) read(ctx context.Context, endpoint string, body map[string]any, extraQuery string) (node, error) {
	var out node
	err := a.withRenew(ctx, func() error {
		var err error
		out, err = a.call(ctx, endpoint, body, extraQuery, true)
		return err
	})
	return out, err
}

// withRenew runs fn; on ErrUnauthenticated with credentials and a Renew
// hook, it renews once and runs fn again.
func (a *API) withRenew(ctx context.Context, fn func() error) error {
	err := fn()
	if err == nil || a.o.Renew == nil || a.credentials() == nil || !errors.Is(err, ErrUnauthenticated) {
		return err
	}
	before := a.credentials()
	a.renewMu.Lock()
	// Another caller may have renewed while we waited.
	if a.credentials() == before {
		c, rerr := a.o.Renew(ctx)
		if rerr != nil {
			a.renewMu.Unlock()
			a.o.Logger.Info("ytmusic: session renewal failed", "err", rerr)
			return errors.Join(err, rerr)
		}
		a.SetCredentials(&c)
		a.o.Logger.Info("ytmusic: session renewed")
	}
	a.renewMu.Unlock()
	return fn()
}

// call sends one InnerTube request. Identical idempotent requests in flight
// share one HTTP round trip.
func (a *API) call(ctx context.Context, endpoint string, body map[string]any, extraQuery string, idempotent bool) (node, error) {
	creds := a.credentials()
	payload, err := a.payload(body)
	if err != nil {
		return node{}, err
	}
	if !idempotent {
		return a.attempts(ctx, endpoint, payload, extraQuery, creds, false)
	}
	key := fmt.Sprintf("%s?%s|%p|%s", endpoint, extraQuery, creds, payload)
	ch := a.group.DoChan(key, func() (any, error) {
		// Shared work is bounded by the client's lifetime and the call
		// timeout, not by whichever caller arrived first.
		return a.attempts(a.ctx, endpoint, payload, extraQuery, creds, true)
	})
	select {
	case <-ctx.Done():
		return node{}, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return node{}, r.Err
		}
		return r.Val.(node), nil
	}
}

func (a *API) payload(body map[string]any) ([]byte, error) {
	client := map[string]any{
		"clientName":    "WEB_REMIX",
		"clientVersion": "1." + a.o.Clock.Now().UTC().Format("20060102") + ".01.00",
		"hl":            a.o.Language,
	}
	if r := strings.ToUpper(a.o.Region); r != "" && r != "ZZ" {
		client["gl"] = r
	}
	user := map[string]any{}
	if a.o.BrandID != "" {
		user["onBehalfOfUser"] = a.o.BrandID
	}
	full := make(map[string]any, len(body)+1)
	for k, v := range body {
		full[k] = v
	}
	full["context"] = map[string]any{"client": client, "user": user}
	return json.Marshal(full)
}

func (a *API) attempts(ctx context.Context, endpoint string, payload []byte, extraQuery string, creds *Credentials, retry bool) (node, error) {
	var last error
	for try := 0; ; try++ {
		if err := a.lim.wait(ctx); err != nil {
			return node{}, err
		}
		n, err := a.once(ctx, endpoint, payload, extraQuery, creds)
		if err == nil {
			return n, nil
		}
		last = err
		if !retry || a.o.Retries < 0 || try >= a.o.Retries || !retryable(ctx, err) {
			return node{}, last
		}
		d := backoff(try)
		a.o.Logger.Debug("ytmusic: retrying", "endpoint", endpoint, "try", try+1, "in", d, "err", err)
		t := a.o.Clock.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return node{}, errors.Join(last, ctx.Err())
		case <-t.C():
		}
	}
}

// backoff is 250 ms · 2^try with ±50 % jitter.
func backoff(try int) time.Duration {
	base := 250 * time.Millisecond << try
	return base/2 + rand.N(base) //nolint:gosec // jitter, not security
}

func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	var ue *url.Error // transport failures from http.Client.Do
	return errors.As(err, &ue)
}

const maxResponseBytes = 32 << 20

func (a *API) once(ctx context.Context, endpoint string, payload []byte, extraQuery string, creds *Credentials) (node, error) {
	ctx, cancel := context.WithTimeout(ctx, a.o.Timeout)
	defer cancel()

	u := a.o.BaseURL + endpoint + "?alt=json&prettyPrint=false"
	if creds != nil {
		u += "&key=" + webAPIKey
	}
	u += extraQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return node{}, fmt.Errorf("ytmusic: %s: %w", endpoint, err)
	}
	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "*/*")
	h.Set("Origin", Origin)
	h.Set("X-Origin", Origin)
	h.Set("User-Agent", DefaultUserAgent)
	// SOCS=CAI skips the EU cookie-consent interstitial (ytmusicapi ytmusic.py).
	cookie := "SOCS=CAI"
	if creds != nil {
		sapisid, err := creds.SAPISID()
		if err != nil {
			return node{}, fmt.Errorf("ytmusic: %w: %w", ErrUnauthenticated, err)
		}
		if creds.UserAgent != "" {
			h.Set("User-Agent", creds.UserAgent)
		}
		cookie = creds.Cookie
		if _, ok := CookieValue(cookie, "SOCS"); !ok {
			cookie += "; SOCS=CAI"
		}
		h.Set("Authorization", SAPISIDHash(sapisid, Origin, a.o.Clock.Now()))
		h.Set("X-Goog-AuthUser", strconv.Itoa(creds.AuthUser))
	}
	h.Set("Cookie", cookie)

	start := a.o.Clock.Now()
	resp, err := a.o.HTTP.Do(req)
	if err != nil {
		return node{}, fmt.Errorf("ytmusic: %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	a.o.Logger.Debug("ytmusic: request", "endpoint", endpoint, "status", resp.StatusCode,
		"bytes", len(raw), "took", a.o.Clock.Now().Sub(start), "auth", creds != nil)
	if err != nil {
		return node{}, fmt.Errorf("ytmusic: %s: read body: %w", endpoint, err)
	}
	if resp.StatusCode >= 400 {
		he := &HTTPError{Endpoint: endpoint, Status: resp.StatusCode}
		var eb struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &eb) == nil {
			he.Message = eb.Error.Message
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			he.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), a.o.Clock.Now())
		}
		return node{}, he
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return node{}, fmt.Errorf("ytmusic: %s: decode response: %w", endpoint, err)
	}
	return node{v}, nil
}

func parseRetryAfter(s string, now time.Time) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(s); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}
