package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/Franciss-Prog/go-ytm/internal/auth"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

type setupOpts struct {
	browser string
	slots   int
	slot    int
	manual  bool
	yes     bool
}

// Source choices in the interactive picker besides browser names.
const (
	sourceAuto   = "auto"
	sourceManual = "manual"
)

func newSetupCmd(a *App) *cobra.Command {
	var o setupOpts
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Sign in by reading your YouTube Music session from a browser",
		Long: "Reads your music.youtube.com session cookies from a browser (via yt-dlp),\n" +
			"a cookies file ([yt_dlp] cookies_file) or pasted request headers, lists the\n" +
			"Google accounts signed in there, and saves the one you pick to the config dir.\n\n" +
			"Supported browsers: " + strings.Join(auth.Browsers, ", ") + ".",
		Example: "# interactive\nytm setup\n" +
			"# non-interactive\nytm setup --browser firefox --slot 0 --yes\n" +
			"# headers copied from devtools (Network → a music.youtube.com request)\nytm setup --manual < headers.txt",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runSetup(cmd.Context(), o, cmd.Flags().Changed("slot"))
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&o.browser, "browser", "", "read cookies from `browser` ("+strings.Join(auth.Browsers, ", ")+")")
	fs.IntVar(&o.slots, "slots", auth.DefaultSlots, fmt.Sprintf("number of Google account slots to check (max %d)", auth.MaxSlots))
	fs.IntVar(&o.slot, "slot", 0, "use the account in this slot without asking")
	fs.BoolVar(&o.manual, "manual", false, "paste request headers instead of reading a browser (stdin when piped)")
	fs.BoolVar(&o.yes, "yes", false, "replace an existing session without asking")
	_ = cmd.RegisterFlagCompletionFunc("browser", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return auth.Browsers, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func usageErr(format string, args ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, args...)}
}

func unauthErr(format string, args ...any) error {
	return &ExitError{Code: ExitUnauth, Err: fmt.Errorf(format, args...)}
}

func (a *App) interactive() bool {
	return a.IO.InTTY != nil && a.IO.InTTY() && a.IO.IsTTY != nil && a.IO.IsTTY(a.IO.Err)
}

func (a *App) form(groups ...*huh.Group) *huh.Form {
	accessible := a.getenv("ACCESSIBLE") != ""
	in := a.IO.In
	if accessible {
		// Accessible mode scans lines from In with a fresh buffered reader
		// per form; feed it a byte at a time so one form never swallows the
		// next form's answers (piped input).
		in = oneByteReader{in}
	}
	return huh.NewForm(groups...).
		WithInput(in).
		WithOutput(a.IO.Err).
		WithTheme(style.HuhTheme(a.Theme, a.Config.Theme.Transparent)).
		WithAccessible(accessible)
}

type oneByteReader struct{ r io.Reader }

func (o oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return o.r.Read(p)
}

func (a *App) runForm(ctx context.Context, f *huh.Form) error {
	err := f.RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled) {
		return &ExitError{Code: ExitInterrupted, Err: errors.New("setup cancelled")}
	}
	return err
}

// stderr writes styled text to stderr.
func (a *App) stderr(build func(r *style.Renderer) string) {
	m := a.modeFor(a.IO.Err, false)
	_, _ = io.WriteString(m.Writer(a.IO.Err), build(a.Renderer(m)))
}

func (a *App) runSetup(ctx context.Context, o setupOpts, slotSet bool) error {
	if o.browser != "" && !slices.Contains(auth.Browsers, o.browser) {
		return usageErr("unknown browser %q (want one of %s)", o.browser, strings.Join(auth.Browsers, ", "))
	}
	if o.browser != "" && o.manual {
		return usageErr("--browser and --manual are mutually exclusive")
	}
	if o.slots < 1 || o.slots > auth.MaxSlots {
		return usageErr("--slots must be between 1 and %d", auth.MaxSlots)
	}
	if slotSet && (o.slot < 0 || o.slot >= auth.MaxSlots) {
		return usageErr("--slot must be between 0 and %d", auth.MaxSlots-1)
	}
	tty := a.interactive()
	if !tty && !o.manual && o.browser == "" && a.Config.YtDlp.CookiesFile == "" && a.Config.YtDlp.CookieBrowser == "" {
		return usageErr("stdin is not a terminal; pass --browser/--slot or --manual < headers.txt")
	}

	src, err := a.chooseSource(ctx, o, tty)
	if err != nil {
		return err
	}
	jar, base, src, err := a.readSession(ctx, src, tty)
	if err != nil {
		return err
	}

	// Which slots to check: an explicit --slot, the slot in pasted headers,
	// or all of them.
	var probed []auth.Probed
	switch {
	case slotSet || src == sourceManual:
		slot := base.AuthUser
		if slotSet {
			slot = o.slot
		}
		c := base
		c.AuthUser = slot
		info, err := a.clientFactory()(c).Account(ctx)
		if err != nil && !errors.Is(err, ytmusic.ErrUnauthenticated) {
			return a.apiError(ctx, "setup: account lookup", err)
		}
		if err == nil {
			probed = []auth.Probed{{Slot: slot, Info: info}}
		}
	default:
		probed, err = auth.ProbeSlots(ctx, a.clientFactory(), base, o.slots)
		if err != nil {
			return a.apiError(ctx, "setup: account lookup", err)
		}
	}
	if len(probed) == 0 {
		hint := fmt.Sprintf("checked slots 0-%d; try --slots %d or --manual", o.slots-1, min(o.slots+5, auth.MaxSlots))
		if slotSet || src == sourceManual {
			hint = fmt.Sprintf("checked slot %d only", base.AuthUser)
			if slotSet {
				hint = fmt.Sprintf("checked slot %d only", o.slot)
			}
		}
		return unauthErr("YouTube Music did not recognize a signed-in account in that session (%s)", hint)
	}

	chosen, err := a.chooseAccount(ctx, probed, tty)
	if err != nil {
		return err
	}

	store := a.authStore()
	if store.Exists() && !o.yes {
		if !tty {
			return usageErr("a session is already saved; pass --yes to replace it")
		}
		replace := true
		prev, _ := store.Load()
		title := "Replace the saved session?"
		if prev.Account.Name != "" {
			title = fmt.Sprintf("Replace the saved session for %s?", prev.Account.Name)
		}
		if err := a.runForm(ctx, a.form(huh.NewGroup(huh.NewConfirm().Title(title).Value(&replace)))); err != nil {
			return err
		}
		if !replace {
			a.stderr(func(r *style.Renderer) string { return r.Message(style.StatusInfo, "Kept the saved session.") })
			return nil
		}
	}

	base.AuthUser = chosen.Slot
	sess := auth.Session{
		Credentials: base,
		Account: auth.Account{
			Name: chosen.Info.Name, Handle: chosen.Info.Handle, ChannelID: chosen.Info.ChannelID,
			Source: src, SavedAt: time.Now().UTC(),
		},
		StreamJar: auth.StreamJar(jar),
	}
	if err := store.Save(sess); err != nil {
		return &ExitError{Code: ExitFailure, Err: fmt.Errorf("save session: %w", err)}
	}
	a.Log.Info("setup: session saved", "source", src, "slot", chosen.Slot, "channel", chosen.Info.ChannelID != "")

	m := a.Mode(false)
	r := a.Renderer(m)
	who := chosen.Info.Name
	if chosen.Info.Handle != "" {
		who += " (" + chosen.Info.Handle + ")"
	}
	out := r.Message(style.StatusOK, "Signed in as "+who+". Saved to "+a.Paths.Config)
	if chosen.Info.ChannelID == "" || src == sourceManual {
		out += r.Message(style.StatusWarn, "Automatic session renewal is unavailable for this account; re-run ytm setup when it expires.")
	}
	return a.Print(m, out)
}

// chooseSource returns a browser name, "cookies_file" or "manual".
func (a *App) chooseSource(ctx context.Context, o setupOpts, tty bool) (string, error) {
	switch {
	case o.manual:
		return sourceManual, nil
	case o.browser != "":
		return o.browser, nil
	case a.Config.YtDlp.CookiesFile != "":
		return "cookies_file", nil
	case a.Config.YtDlp.CookieBrowser != "":
		return a.Config.YtDlp.CookieBrowser, nil
	case !tty:
		return sourceAuto, nil
	}
	choice := sourceAuto
	opts := []huh.Option[string]{huh.NewOption("Detect automatically", sourceAuto)}
	for _, b := range auth.Browsers {
		opts = append(opts, huh.NewOption(browserLabel(b), b))
	}
	opts = append(opts, huh.NewOption("Paste request headers manually", sourceManual))
	sel := huh.NewSelect[string]().
		Title("Where should ytm-go read your YouTube Music session from?").
		Description("Sign in at music.youtube.com in that browser first.").
		Options(opts...).Value(&choice)
	if err := a.runForm(ctx, a.form(huh.NewGroup(sel))); err != nil {
		return "", err
	}
	return choice, nil
}

func browserLabel(b string) string {
	switch b {
	case "firefox":
		return "Firefox"
	case "zen":
		return "Zen"
	case "chrome":
		return "Chrome"
	case "chromium":
		return "Chromium"
	case "brave":
		return "Brave"
	case "edge":
		return "Edge"
	case "vivaldi":
		return "Vivaldi"
	case "opera":
		return "Opera"
	case "helium":
		return "Helium"
	}
	return b
}

// readSession returns the cookie jar, base credentials and resolved
// source. "auto" resolves to the first browser holding a signed-in session.
func (a *App) readSession(ctx context.Context, src string, tty bool) (_ []auth.Cookie, _ ytmusic.Credentials, resolved string, _ error) {
	now := time.Now()
	resolved = src
	if src == sourceManual {
		raw, err := a.readHeaders(ctx, tty)
		if err != nil {
			return nil, ytmusic.Credentials{}, "", err
		}
		ms, err := auth.ParseManual(raw)
		if err != nil {
			return nil, ytmusic.Credentials{}, "", usageErr("%v", err)
		}
		creds := ytmusic.Credentials{Cookie: ms.Cookie, UserAgent: ms.UserAgent, AuthUser: ms.AuthUser}
		if _, err := creds.SAPISID(); err != nil {
			return nil, ytmusic.Credentials{}, "", unauthErr("the pasted Cookie header is not from a signed-in session (%v)", err)
		}
		return auth.CookiesFromHeader(ms.Cookie, now), creds, resolved, nil
	}

	var jar []auth.Cookie
	if src == sourceAuto {
		var errs []error
		for _, b := range auth.Browsers {
			j, err := a.extractFrom(ctx, b)
			if ctx.Err() != nil {
				return nil, ytmusic.Credentials{}, "", ctx.Err()
			}
			if err != nil {
				if missingYtDlp(err) {
					return nil, ytmusic.Credentials{}, "", a.ytdlpMissing(err)
				}
				a.Log.Debug("setup: browser not usable", "browser", b, "err", err)
				errs = append(errs, err)
				continue
			}
			if auth.HasSession(j, now) {
				jar, resolved = j, b
				a.stderr(func(r *style.Renderer) string {
					return r.Message(style.StatusInfo, "Found a YouTube session in "+browserLabel(b)+".")
				})
				break
			}
		}
		if jar == nil {
			a.Log.Info("setup: auto-detect found no session", "err", errors.Join(errs...))
			return nil, ytmusic.Credentials{}, "", unauthErr("no browser has a signed-in YouTube Music session; sign in at music.youtube.com, or use --browser or --manual")
		}
	} else {
		j, err := a.extractFrom(ctx, src)
		if err != nil {
			if missingYtDlp(err) {
				return nil, ytmusic.Credentials{}, "", a.ytdlpMissing(err)
			}
			a.Log.Error("setup: cookie extraction failed", "source", src, "err", err)
			return nil, ytmusic.Credentials{}, "", &ExitError{Code: ExitFailure, Err: fmt.Errorf("could not read cookies from %s: %w", sourceLabel(src), err)}
		}
		if !auth.HasSession(j, now) {
			return nil, ytmusic.Credentials{}, "", unauthErr("%s has no signed-in YouTube session; sign in at music.youtube.com there first", sourceLabel(src))
		}
		jar = j
	}
	return jar, ytmusic.Credentials{Cookie: auth.CookieHeader(jar, now)}, resolved, nil
}

func sourceLabel(src string) string {
	if src == "cookies_file" {
		return "[yt_dlp] cookies_file"
	}
	return browserLabel(src)
}

func missingYtDlp(err error) bool {
	var ee *exec.Error
	return errors.As(err, &ee) || strings.HasPrefix(err.Error(), "run ")
}

func (a *App) ytdlpMissing(err error) error {
	a.Log.Error("setup: yt-dlp not runnable", "err", err)
	return &ExitError{Code: ExitMissingDep, Err: errors.New("yt-dlp is required to read browser cookies; install it or set [yt_dlp] binary (or use --manual)")}
}

func (a *App) readHeaders(ctx context.Context, tty bool) (string, error) {
	if !tty {
		raw, err := io.ReadAll(io.LimitReader(a.IO.In, 1<<20))
		if err != nil {
			return "", fmt.Errorf("read headers from stdin: %w", err)
		}
		return string(raw), nil
	}
	var raw string
	field := huh.NewText().
		Title("Paste the request headers").
		Description("In your browser: open music.youtube.com, devtools → Network, pick a\n" +
			"request to music.youtube.com/youtubei/v1/…, and copy its request headers.").
		Lines(8).CharLimit(1 << 20).Value(&raw).
		Validate(func(s string) error {
			_, err := auth.ParseManual(s)
			return err
		})
	if err := a.runForm(ctx, a.form(huh.NewGroup(field))); err != nil {
		return "", err
	}
	return raw, nil
}

func (a *App) chooseAccount(ctx context.Context, probed []auth.Probed, tty bool) (auth.Probed, error) {
	if len(probed) == 1 {
		return probed[0], nil
	}
	if !tty {
		labels := make([]string, len(probed))
		for i, p := range probed {
			labels[i] = p.Label()
		}
		return auth.Probed{}, usageErr("several accounts are signed in (%s); pass --slot N", strings.Join(labels, "; "))
	}
	idx := 0
	opts := make([]huh.Option[int], len(probed))
	for i, p := range probed {
		opts[i] = huh.NewOption(p.Label(), i)
	}
	sel := huh.NewSelect[int]().
		Title("Accounts found").
		Description("Pick your YouTube Music account (the Premium one, if you have it).").
		Options(opts...).Value(&idx)
	if err := a.runForm(ctx, a.form(huh.NewGroup(sel))); err != nil {
		return auth.Probed{}, err
	}
	return probed[idx], nil
}
