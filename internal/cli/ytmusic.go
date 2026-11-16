package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/auth"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// YTMusicFactory builds API clients; tests replace it with fakes.
var YTMusicFactory = func(o ytmusic.Options) ytmusic.Client { return ytmusic.New(o) }

// ExtractorFactory builds the browser cookie extractor; tests replace it.
var ExtractorFactory = func(a *App) auth.Extractor {
	e := a.platformEnv()
	home, _ := e.HomeDir()
	return auth.YtDlp{Binary: a.Config.YtDlp.Binary, GOOS: a.IO.GOOS, Home: home, Getenv: a.getenv}
}

func (a *App) authStore() auth.Store { return auth.Store{Dir: a.Paths.Config} }

// ytOptions are the client options derived from config.
func (a *App) ytOptions() ytmusic.Options {
	return ytmusic.Options{
		HTTP:    a.httpClient(),
		Timeout: time.Duration(a.Config.Playback.APITimeout) * time.Second,
		Region:  a.Config.UI.Region,
		BrandID: a.Config.General.BrandAccountID,
		Logger:  a.Log,
	}
}

// newYT returns a client and registers its Close with the app.
func (a *App) newYT(o ytmusic.Options) ytmusic.Client {
	c := YTMusicFactory(o)
	if cl, ok := c.(io.Closer); ok {
		a.addCloser(cl.Close)
	}
	return c
}

// clientFactory builds per-credential clients for slot probing.
func (a *App) clientFactory() auth.ClientFactory {
	return func(c ytmusic.Credentials) ytmusic.Client {
		o := a.ytOptions()
		o.Credentials = &c
		o.Retries = -1 // a probe that fails is just "no account here"
		return a.newYT(o)
	}
}

// extractFrom reads cookies from a recorded source: a browser name or
// "cookies_file".
func (a *App) extractFrom(ctx context.Context, source string) ([]auth.Cookie, error) {
	if source == "cookies_file" {
		if a.Config.YtDlp.CookiesFile == "" {
			return nil, errors.New("[yt_dlp] cookies_file is no longer set")
		}
		return auth.FileExtractor(a.expandPath(a.Config.YtDlp.CookiesFile))
	}
	return ExtractorFactory(a).Extract(ctx, source)
}

func (a *App) expandPath(p string) string {
	home, _ := a.platformEnv().HomeDir()
	return platform.ExpandHome(p, home)
}

// signedInClient returns a client using the saved session (with silent
// renewal), or an anonymous client when there is none. signedIn reports
// which one it is.
func (a *App) signedInClient() (c ytmusic.Client, signedIn bool, err error) {
	o := a.ytOptions()
	sess, err := a.authStore().Load()
	switch {
	case errors.Is(err, auth.ErrNotConfigured):
		return a.newYT(o), false, nil
	case err != nil:
		return nil, false, fmt.Errorf("read saved session: %w", err)
	}
	o.Credentials = &sess.Credentials
	r := auth.Renewer{Store: a.authStore(), Extract: a.extractFrom, NewClient: a.clientFactory()}
	o.Renew = r.Renew
	return a.newYT(o), true, nil
}
