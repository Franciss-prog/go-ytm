// Package logging configures slog: structured JSON to a rotating log file,
// plus human-readable output on stderr in --debug mode (charm log).
// Secrets are redacted before anything is written.
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	charmlog "charm.land/log/v2"
)

// LevelFromConfig maps ytm-player style level names to slog levels.
func LevelFromConfig(name string) slog.Level {
	switch strings.ToUpper(name) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "ERROR", "CRITICAL":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

// Options configure Setup.
type Options struct {
	File        string     // log file path; empty disables file logging
	Level       slog.Level // file level
	MaxBytes    int64
	BackupCount int
	Debug       bool      // also log DEBUG+ to Stderr, human-readable
	Stderr      io.Writer // destination for --debug output
}

// Setup builds the logger. The returned closer flushes and closes the file.
// A failure to open the log file is not fatal: logging falls back to
// discarding file output and the error is returned alongside a usable logger.
func Setup(o Options) (*slog.Logger, io.Closer, error) {
	var handlers []slog.Handler
	var closer io.Closer = nopCloser{}
	var setupErr error

	level := o.Level
	if o.Debug {
		level = slog.LevelDebug
	}

	if o.File != "" {
		rw, err := NewRotatingWriter(o.File, o.MaxBytes, o.BackupCount)
		if err != nil {
			setupErr = fmt.Errorf("open log file: %w", err)
		} else {
			closer = rw
			handlers = append(handlers, slog.NewJSONHandler(rw, &slog.HandlerOptions{
				Level:       level,
				ReplaceAttr: Redact,
			}))
		}
	}
	if o.Debug && o.Stderr != nil {
		cl := charmlog.NewWithOptions(o.Stderr, charmlog.Options{
			Level:           charmlog.DebugLevel,
			ReportTimestamp: true,
			TimeFormat:      time.TimeOnly,
		})
		handlers = append(handlers, redactingHandler{cl})
	}
	return slog.New(fanout(handlers)), closer, setupErr
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// secretKeys are attribute keys whose values are always redacted.
var secretKeys = []string{
	"cookie", "authorization", "sapisid", "token", "secret", "password",
	"session_key", "api_key", "apikey", "auth",
}

// IsSecretKey reports whether an attribute key names a secret.
func IsSecretKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range secretKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Redact is a slog ReplaceAttr hook that masks secret values.
func Redact(_ []string, a slog.Attr) slog.Attr {
	if IsSecretKey(a.Key) && a.Value.Kind() != slog.KindGroup {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

// redactingHandler applies Redact to a handler that has no ReplaceAttr hook.
type redactingHandler struct{ slog.Handler }

func (h redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})
	return h.Handler.Handle(ctx, nr)
}

func (h redactingHandler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = redactAttr(a)
	}
	return redactingHandler{h.Handler.WithAttrs(out)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{h.Handler.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		g := a.Value.Group()
		out := make([]any, len(g))
		for i, ga := range g {
			out[i] = redactAttr(ga)
		}
		return slog.Group(a.Key, out...)
	}
	return Redact(nil, a)
}

// fanout sends records to every handler.
type multiHandler []slog.Handler

func fanout(hs []slog.Handler) slog.Handler {
	if len(hs) == 0 {
		return slog.DiscardHandler
	}
	if len(hs) == 1 {
		return hs[0]
	}
	return multiHandler(hs)
}

func (m multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (m multiHandler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithAttrs(as)
	}
	return out
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithGroup(name)
	}
	return out
}

// EnsureDir creates dir with 0700 permissions.
func EnsureDir(dir string) error { return os.MkdirAll(filepath.Clean(dir), 0o700) }
