package stream

import (
	"context"
	"os"
	"testing"
)

func BenchmarkParseInfo(b *testing.B) {
	raw, err := os.ReadFile("testdata/info_opus.json")
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseInfo(opusID, raw, fixed); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCacheHit(b *testing.B) {
	c, _, _ := newCache(b, nil)
	ctx := context.Background()
	if _, err := c.Resolve(ctx, opusID, High); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Resolve(ctx, opusID, High); err != nil {
			b.Fatal(err)
		}
	}
}
