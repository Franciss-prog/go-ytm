// Command scrubfixtures turns raw InnerTube captures (from
// internal/ytmusic/record_test.go) into committed test fixtures.
//
//	go run ./scripts/scrubfixtures -in RAW_DIR -out internal/ytmusic/testdata/responses \
//	    -account ~/.config/ytm-go/account.json -personal library_playlists,library_playlists_cont,... FILE...
//
// Every file: tracking/logging keys are dropped, lists under
// contents/items/continuationItems are cut to -keep entries (a trailing
// continuation item is kept), thumbnail lists to 2, and the account's
// name, handle and channel id are replaced.
//
// Personal files (the account's library and own playlists) additionally
// get every free-text run replaced by a placeholder ("Text 7"), every id by
// a fake id with the same prefix, and thumbnail URLs by example.invalid
// URLs. Structural text the parsers rely on (separators, durations, counts,
// years, type labels) is kept.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var dropKeys = map[string]bool{
	"trackingParams": true, "clickTrackingParams": true, "responseContext": true,
	"loggingContext": true, "serializedShareEntity": true, "accessibility": true,
	"accessibilityData": true, "frameworkUpdates": true, "visitorData": true,
	"loggingDirectives": true, "microformat": true, "adSignalsInfo": true, "commandMetadata": true,
}

var listKeys = map[string]bool{"contents": true, "items": true, "continuationItems": true}

// keepText matches run texts that carry structure rather than content.
var keepText = regexp.MustCompile(`^(?:[\s•&,·:|]*|\d[\w ,.:]*|Album|Single|EP|Playlist|Auto playlist|New playlist|Liked Music|Episodes for Later|Song|Songs|Video|Videos|Artist|Artists|Albums|Singles|Singles & EPs|Playlists|Related|Shows|Episodes|Podcast|Podcasts|Shuffle|Play|Radio|Mix|Private|Public|Unlisted|You|Various Artists|Featured on|Fans might also like|Latest release|Top songs)$`)

// textKeys hold display text in personal files.
var textKeys = map[string]bool{"text": true, "content": true, "simpleText": true, "title": true, "label": true, "description": true}

var idKeys = map[string]bool{
	"videoId": true, "playlistId": true, "browseId": true, "setVideoId": true,
	"removedVideoId": true, "addedVideoId": true, "channelId": true, "targetId": true, "formId": true, "playlistSetVideoId": true,
}

type scrubber struct {
	keep     int
	personal bool
	replace  *strings.Replacer
	texts    map[string]string
	ids      map[string]string
	thumbs   int
}

func main() {
	in := flag.String("in", "", "raw capture dir")
	out := flag.String("out", "", "fixture dir")
	account := flag.String("account", "", "account.json with name/handle/channel_id to replace")
	personal := flag.String("personal", "", "comma-separated file stems to fully anonymize")
	keep := flag.Int("keep", 6, "max entries kept per content list")
	flag.Parse()
	if *in == "" || *out == "" || flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out, *account, *personal, *keep, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "scrubfixtures:", err)
		os.Exit(1)
	}
}

func run(in, out, account, personal string, keep int, stems []string) error {
	var pairs []string
	if account != "" {
		b, err := os.ReadFile(account)
		if err != nil {
			return err
		}
		var a struct {
			Name      string `json:"name"`
			Handle    string `json:"handle"`
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(b, &a); err != nil {
			return err
		}
		for _, p := range [][2]string{{a.ChannelID, "UCtestuserchannel0000000"}, {a.Handle, "@testuser"}, {a.Name, "Test User"}} {
			if p[0] != "" {
				pairs = append(pairs, p[0], p[1])
			}
		}
	}
	pers := map[string]bool{}
	for _, s := range strings.Split(personal, ",") {
		pers[strings.TrimSpace(s)] = true
	}
	// Shared maps keep placeholders consistent across files (an owned
	// playlist id in the library list matches its playlist page).
	texts, ids := map[string]string{}, map[string]string{}
	for _, stem := range stems {
		b, err := os.ReadFile(filepath.Join(in, stem+".json"))
		if err != nil {
			return err
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return fmt.Errorf("%s: %w", stem, err)
		}
		s := &scrubber{keep: keep, personal: pers[stem], replace: strings.NewReplacer(pairs...), texts: texts, ids: ids}
		v = s.walk(v, "")
		o, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		if leak := s.leak(string(o), pairs); leak != "" {
			return fmt.Errorf("%s: private value %q survived scrubbing", stem, leak)
		}
		if err := os.WriteFile(filepath.Join(out, stem+".json"), append(o, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s: %d KB → %d KB\n", stem, len(b)/1024, len(o)/1024)
	}
	return nil
}

// leak returns an account value or (in personal files) an original id
// still present in out.
func (s *scrubber) leak(out string, pairs []string) string {
	for i := 0; i < len(pairs); i += 2 {
		if strings.Contains(out, pairs[i]) {
			return pairs[i]
		}
	}
	if s.personal {
		for orig := range s.ids {
			if len(orig) > 6 && strings.Contains(out, `"`+orig) {
				return orig
			}
		}
	}
	return ""
}

func (s *scrubber) walk(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			// feedbackToken, addToLibraryFeedbackToken, …: account-bound action tokens.
			if dropKeys[k] || strings.HasSuffix(strings.ToLower(k), "feedbacktoken") {
				delete(t, k)
				continue
			}
			t[k] = s.walk(c, k)
		}
		if s.personal {
			if u, ok := t["url"].(string); ok && strings.HasPrefix(u, "http") {
				s.thumbs++
				t["url"] = fmt.Sprintf("https://example.invalid/thumb/%d.jpg", s.thumbs)
			}
		}
		return t
	case []any:
		if listKeys[key] && len(t) > s.keep && !isMenu(t) {
			last := t[len(t)-1]
			t = t[:s.keep:s.keep]
			if m, ok := last.(map[string]any); ok && m["continuationItemRenderer"] != nil {
				t = append(t, last)
			}
		}
		if key == "thumbnails" && len(t) > 2 {
			t = append(t[:1:1], t[len(t)-1])
		}
		for i, c := range t {
			t[i] = s.walk(c, key)
		}
		return t
	case string:
		t = s.replace.Replace(t)
		if !s.personal {
			return t
		}
		switch {
		case textKeys[key]:
			return s.text(t)
		case idKeys[key]:
			return s.id(t)
		}
		return t
	}
	return v
}

func (s *scrubber) text(t string) string {
	if keepText.MatchString(strings.TrimSpace(t)) {
		return t
	}
	if r, ok := s.texts[t]; ok {
		return r
	}
	r := fmt.Sprintf("Text %d", len(s.texts)+1)
	s.texts[t] = r
	return r
}

// idPrefixes are kept so parsers still classify ids (album vs artist…).
var idPrefixes = []string{"FEmusic_", "MPREb_", "MPLA", "MPTC", "VLPL", "VLOLAK5uy_", "VLRD", "VL", "PL", "OLAK5uy_", "RD", "UC", "LM", "SE"}

func (s *scrubber) id(t string) string {
	if t == "" || strings.HasPrefix(t, "FEmusic_") || t == "LM" || t == "VLLM" || t == "SE" || t == "VLSE" {
		return t
	}
	if r, ok := s.ids[t]; ok {
		return r
	}
	prefix := ""
	for _, p := range idPrefixes {
		if strings.HasPrefix(t, p) {
			prefix = p
			break
		}
	}
	n := len(s.ids) + 1
	body := fmt.Sprintf("fake%0*d", max(len(t)-len(prefix)-4, 1), n)
	r := prefix + body
	// Equal ids with and without the VL browse prefix stay equal.
	if strings.HasPrefix(t, "VL") {
		s.ids[t[2:]] = r[2:]
	} else {
		s.ids["VL"+t] = "VL" + r
	}
	s.ids[t] = r
	return r
}

// isMenu reports a menu's item list (never cut: parsers look for specific
// entries such as the owner's "remove from playlist").
func isMenu(t []any) bool {
	m, ok := t[0].(map[string]any)
	if !ok {
		return false
	}
	for k := range m {
		if strings.HasPrefix(k, "menu") || strings.HasPrefix(k, "toggleMenu") {
			return true
		}
	}
	return false
}
