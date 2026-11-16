//go:build live

package ytmusic

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live smoke tests against music.youtube.com (signed out). Not run in CI:
//
//	YTM_LIVE=1 go test -tags live -run Live ./internal/ytmusic
func TestLiveSearch(t *testing.T) {
	if os.Getenv("YTM_LIVE") != "1" {
		t.Skip("set YTM_LIVE=1")
	}
	a := New(Options{})
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	all, err := a.Search(ctx, "daft punk", FilterAll, 20)
	if err != nil || len(all.Items) == 0 {
		t.Fatalf("unfiltered: %d items, %v", len(all.Items), err)
	}
	for _, f := range Filters {
		res, err := a.Search(ctx, "daft punk", f, 30)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if len(res.Items) == 0 {
			t.Errorf("%s: no results", f)
		}
		for i, r := range res.Items {
			if r.Title() == "" {
				t.Errorf("%s[%d]: empty title", f, i)
			}
		}
	}
	sugg, err := a.SearchSuggestions(ctx, "daft p")
	if err != nil || len(sugg) == 0 {
		t.Errorf("suggestions: %v %v", sugg, err)
	}
}
