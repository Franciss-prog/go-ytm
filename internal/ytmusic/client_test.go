package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

var t0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "responses", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type recorded struct {
	path   string
	query  string
	header http.Header
	body   map[string]any
}

// server serves handler and records every request.
func server(t *testing.T, handler func(w http.ResponseWriter, r recorded, n int)) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone()}
		_ = json.Unmarshal(raw, &rec.body)
		mu.Lock()
		reqs = append(reqs, rec)
		n := len(reqs)
		mu.Unlock()
		handler(w, rec, n)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recorded {
		mu.Lock()
		defer mu.Unlock()
		return append([]recorded(nil), reqs...)
	}
}

func newTestAPI(t *testing.T, srv *httptest.Server, o Options) *API {
	t.Helper()
	o.BaseURL = srv.URL + "/youtubei/v1/"
	o.HTTP = srv.Client()
	if o.Clock == nil {
		o.Clock = clock.NewFake(t0)
	}
	if o.RatePerSec == 0 {
		o.RatePerSec = 1000
		o.Burst = 1000
	}
	a := New(o)
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestSAPISIDHashVector(t *testing.T) {
	// sha1("1700000000 sapisid-value https://music.youtube.com"), computed
	// independently with Python hashlib.
	got := SAPISIDHash("sapisid-value", Origin, time.Unix(1700000000, 0))
	want := "SAPISIDHASH 1700000000_11055f89efabedf355d8a9cb9845ef0187ec5fdc"
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestCredentialsSAPISID(t *testing.T) {
	cases := map[string]string{
		"a=1; __Secure-3PAPISID=three; SAPISID=plain": "three",
		"SAPISID=plain; b=2":                          "plain",
		`__Secure-3PAPISID="quoted"`:                  "quoted",
	}
	for cookie, want := range cases {
		got, err := Credentials{Cookie: cookie}.SAPISID()
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", cookie, got, err)
		}
	}
	if _, err := (Credentials{Cookie: "a=1"}).SAPISID(); !errors.Is(err, ErrNoSAPISID) {
		t.Errorf("err = %v", err)
	}
}

func TestAnonymousRequestShape(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		_, _ = w.Write(fixtureBytes(t, "suggestions"))
	})
	a := newTestAPI(t, srv, Options{Region: "us", Language: "en"})
	got, err := a.SearchSuggestions(context.Background(), "daft p")
	if err != nil || len(got) == 0 {
		t.Fatalf("got %v, %v", got, err)
	}
	r := reqs()[0]
	if r.path != "/youtubei/v1/music/get_search_suggestions" || r.query != "alt=json&prettyPrint=false" {
		t.Errorf("url = %s?%s", r.path, r.query)
	}
	for k, want := range map[string]string{"Origin": Origin, "X-Origin": Origin, "Cookie": "SOCS=CAI", "Content-Type": "application/json"} {
		if got := r.header.Get(k); got != want {
			t.Errorf("%s = %q want %q", k, got, want)
		}
	}
	for _, k := range []string{"Authorization", "X-Goog-Authuser"} {
		if r.header.Get(k) != "" {
			t.Errorf("anonymous request sent %s", k)
		}
	}
	client := r.body["context"].(map[string]any)["client"].(map[string]any)
	if client["clientName"] != "WEB_REMIX" || client["clientVersion"] != "1.20261117.01.00" || client["gl"] != "US" || client["hl"] != "en" {
		t.Errorf("client context = %v", client)
	}
	if r.body["input"] != "daft p" {
		t.Errorf("body = %v", r.body)
	}
}

func TestAuthenticatedHeaders(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		_, _ = w.Write(fixtureBytes(t, "account_synthetic"))
	})
	clk := clock.NewFake(time.Unix(1700000000, 0))
	a := newTestAPI(t, srv, Options{
		Clock:       clk,
		Credentials: &Credentials{Cookie: "__Secure-3PAPISID=sapisid-value; X=1", AuthUser: 3, UserAgent: "UA/1"},
		BrandID:     "1234567890",
		Region:      "ZZ",
	})
	info, err := a.Account(context.Background())
	if err != nil || info.Name != "Jane Doe" {
		t.Fatalf("got %+v, %v", info, err)
	}
	r := reqs()[0]
	want := map[string]string{
		"Authorization":   "SAPISIDHASH 1700000000_11055f89efabedf355d8a9cb9845ef0187ec5fdc",
		"X-Goog-Authuser": "3",
		"User-Agent":      "UA/1",
		"Cookie":          "__Secure-3PAPISID=sapisid-value; X=1; SOCS=CAI",
	}
	for k, v := range want {
		if got := r.header.Get(k); got != v {
			t.Errorf("%s = %q want %q", k, got, v)
		}
	}
	if !strings.Contains(r.query, "key="+webAPIKey) {
		t.Errorf("query = %s", r.query)
	}
	ctx := r.body["context"].(map[string]any)
	if _, ok := ctx["client"].(map[string]any)["gl"]; ok {
		t.Error("region ZZ must omit gl")
	}
	if ctx["user"].(map[string]any)["onBehalfOfUser"] != "1234567890" {
		t.Errorf("user context = %v", ctx["user"])
	}
}

func TestAccountSignedOut(t *testing.T) {
	srv, _ := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		_, _ = w.Write(fixtureBytes(t, "account_unauth"))
	})
	a := newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "SAPISID=x"}})
	if _, err := a.Account(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("err = %v", err)
	}
	anon := newTestAPI(t, srv, Options{})
	if _, err := anon.Account(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("anonymous err = %v", err)
	}
	noSAPISID := newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "a=b"}})
	if _, err := noSAPISID.Account(context.Background()); !errors.Is(err, ErrUnauthenticated) || !errors.Is(err, ErrNoSAPISID) {
		t.Errorf("no-sapisid err = %v", err)
	}
}

func TestSearchFollowsContinuationsToLimit(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, _ int) {
		if strings.Contains(r.query, "ctoken=") {
			_, _ = w.Write(fixtureBytes(t, "search_songs_cont"))
			return
		}
		_, _ = w.Write(fixtureBytes(t, "search_songs"))
	})
	a := newTestAPI(t, srv, Options{})
	res, err := a.Search(context.Background(), " daft punk ", FilterSongs, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 30 || res.Continuation == "" {
		t.Errorf("items=%d continuation=%q", len(res.Items), res.Continuation)
	}
	rs := reqs()
	if len(rs) != 2 {
		t.Fatalf("requests = %d", len(rs))
	}
	if rs[0].body["query"] != "daft punk" || rs[0].body["params"] != searchParams(FilterSongs) {
		t.Errorf("first body = %v", rs[0].body)
	}
	if !strings.Contains(rs[1].query, "&type=next") || rs[1].body["params"] != searchParams(FilterSongs) {
		t.Errorf("continuation request = %s %v", rs[1].query, rs[1].body)
	}

	// Unfiltered searches never page.
	res, err = a.Search(context.Background(), "daft punk", FilterAll, 5)
	if err != nil || len(res.Items) != 5 {
		t.Fatalf("unfiltered: %d, %v", len(res.Items), err)
	}
	if len(reqs()) != 3 {
		t.Errorf("unfiltered search paged: %d requests", len(reqs()))
	}
	if _, err := a.Search(context.Background(), "  ", FilterAll, 5); err == nil {
		t.Error("empty query accepted")
	}
}

func TestRetriesServerErrors(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, n int) {
		if n <= 2 {
			http.Error(w, `{"error":{"message":"backend"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(fixtureBytes(t, "suggestions"))
	})
	clk := clock.NewFake(t0)
	a := newTestAPI(t, srv, Options{Clock: clk})
	done := make(chan error, 1)
	go func() {
		_, err := a.SearchSuggestions(context.Background(), "x")
		done <- err
	}()
	for range 2 {
		if !clk.BlockUntil(1, nil) {
			t.Fatal("no backoff timer")
		}
		clk.Advance(5 * time.Second)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(reqs()) != 3 {
		t.Errorf("requests = %d, want 3", len(reqs()))
	}
}

func TestNoRetryOnClientErrors(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	})
	a := newTestAPI(t, srv, Options{})
	_, err := a.SearchSuggestions(context.Background(), "x")
	var he *HTTPError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &he) || he.RetryAfter != 30*time.Second || he.Message != "slow down" {
		t.Fatalf("err = %v (%+v)", err, he)
	}
	if len(reqs()) != 1 {
		t.Errorf("429 was retried: %d requests", len(reqs()))
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Duration{
		"":                              0,
		"12":                            12 * time.Second,
		"Thu, 01 Jan 2026 00:01:00 GMT": time.Minute,
		"garbage":                       0,
	} {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("%q: %v want %v", in, got, want)
		}
	}
}

func TestSilentRenewal(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, _ int) {
		if !strings.Contains(r.header.Get("Cookie"), "SAPISID=fresh") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(fixtureBytes(t, "suggestions"))
	})
	var renewals atomic.Int32
	a := newTestAPI(t, srv, Options{
		Credentials: &Credentials{Cookie: "SAPISID=stale"},
		Renew: func(context.Context) (Credentials, error) {
			renewals.Add(1)
			return Credentials{Cookie: "SAPISID=fresh"}, nil
		},
	})
	if _, err := a.SearchSuggestions(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if renewals.Load() != 1 || len(reqs()) != 2 {
		t.Errorf("renewals=%d requests=%d", renewals.Load(), len(reqs()))
	}

	// A failing renewal surfaces ErrUnauthenticated and is not retried in a loop.
	b := newTestAPI(t, srv, Options{
		Credentials: &Credentials{Cookie: "SAPISID=stale"},
		Renew: func(context.Context) (Credentials, error) {
			renewals.Add(1)
			return Credentials{}, errors.New("browser locked")
		},
	})
	if _, err := b.SearchSuggestions(context.Background(), "x"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("err = %v", err)
	}
	if renewals.Load() != 2 {
		t.Errorf("renewals = %d", renewals.Load())
	}
}

func TestSingleflightSharesIdenticalReads(t *testing.T) {
	release := make(chan struct{})
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		<-release
		_, _ = w.Write(fixtureBytes(t, "suggestions"))
	})
	a := newTestAPI(t, srv, Options{})
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() {
			_, err := a.SearchSuggestions(context.Background(), "same")
			errs <- err
		})
	}
	// Let the five callers pile onto the one in-flight request.
	deadline := time.Now().Add(2 * time.Second)
	for len(reqs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := len(reqs()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

func TestCallerCancellation(t *testing.T) {
	release := make(chan struct{})
	srv, _ := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		<-release
		_, _ = w.Write([]byte(`{}`))
	})
	defer close(release)
	a := newTestAPI(t, srv, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.SearchSuggestions(ctx, "x")
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestDecodeErrorNotRetried(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, _ int) {
		_, _ = w.Write([]byte(`<html>`))
	})
	a := newTestAPI(t, srv, Options{})
	if _, err := a.SearchSuggestions(context.Background(), "x"); err == nil {
		t.Fatal("bad JSON accepted")
	}
	if len(reqs()) != 1 {
		t.Errorf("requests = %d", len(reqs()))
	}
}

func TestLimiter(t *testing.T) {
	clk := clock.NewFake(t0)
	l := newLimiter(clk, 5, 2)
	ctx := context.Background()
	for range 2 {
		if err := l.wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- l.wait(ctx) }()
	if !clk.BlockUntil(1, nil) {
		t.Fatal("third wait did not block")
	}
	clk.Advance(200 * time.Millisecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	cctx, cancel := context.WithCancel(ctx)
	go func() { done <- l.wait(cctx) }()
	clk.BlockUntil(1, nil)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}
