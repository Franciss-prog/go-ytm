package style

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

func TestTablePlainLayout(t *testing.T) {
	r := New(Options{Theme: theme.Fallback(), Mode: Mode{}})
	cols := []Column{{Header: "#", Right: true}, {Header: "Title", Shrink: true, Min: 5}, {Header: "Time", Right: true}, {Header: "ID", Muted: true}}
	rows := [][]string{
		{"1", "A very long title that will not fit in the space we have", "3:00", "abc"},
		{"10", "Short", "", "defghi"},
	}
	got := r.Table(cols, rows, 40)
	want := "" +
		"  #  Title                  Time  ID\n" +
		"  1  A very long title th…  3:00  abc\n" +
		" 10  Short                        defghi\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	for _, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if lipgloss.Width(l) > 40 {
			t.Errorf("line wider than 40: %q", l)
		}
	}
	// Non-shrinkable columns are never truncated even when too wide.
	wide := r.Table([]Column{{Header: "ID"}}, [][]string{{strings.Repeat("x", 60)}}, 20)
	if !strings.Contains(wide, strings.Repeat("x", 60)) {
		t.Error("non-shrink column truncated")
	}
}

func TestTableTransparentNoBackground(t *testing.T) {
	for _, transparent := range []bool{true, false} {
		r := New(Options{Theme: theme.Fallback(), Transparent: transparent, Mode: Mode{Styled: true, Profile: colorprofile.TrueColor}})
		out := r.Heading("Search · x", "all · 1 result") +
			r.Table([]Column{{Header: "Title"}, {Header: "ID", Muted: true}}, [][]string{{"a", "b"}}, 0)
		bg := BackgroundSGRs(out)
		if transparent && len(bg) > 0 {
			t.Errorf("transparent table/heading emitted backgrounds: %v", bg)
		}
		if !transparent && len(bg) == 0 {
			t.Error("opaque heading should use a background")
		}
	}
}

// styleFields collects every lipgloss.Style reachable in v.
func styleFields(v reflect.Value, path string, out map[string]lipgloss.Style) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			styleFields(v.Elem(), path, out)
		}
	case reflect.Struct:
		if s, ok := v.Interface().(lipgloss.Style); ok {
			out[path] = s
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				styleFields(v.Field(i), path+"."+v.Type().Field(i).Name, out)
			}
		}
	}
}

func TestHuhThemeTransparent(t *testing.T) {
	for _, dark := range []bool{true, false} {
		s := HuhTheme(theme.Fallback(), true).Theme(dark)
		all := map[string]lipgloss.Style{}
		styleFields(reflect.ValueOf(s), "Styles", all)
		if len(all) < 20 {
			t.Fatalf("only %d styles found", len(all))
		}
		for path, st := range all {
			if _, none := st.GetBackground().(lipgloss.NoColor); !none && st.GetBackground() != nil {
				t.Errorf("%s has background %v in transparent mode", path, st.GetBackground())
			}
		}
	}
	opaque := HuhTheme(theme.Fallback(), false).Theme(true)
	if _, none := opaque.Focused.FocusedButton.GetBackground().(lipgloss.NoColor); none {
		t.Error("opaque focused button has no background")
	}
	var _ huh.Theme = HuhTheme(theme.Fallback(), false) //nolint:staticcheck // compile-time interface check
}
