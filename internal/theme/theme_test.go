package theme

import (
	"reflect"
	"testing"
)

func TestBuiltinsComplete(t *testing.T) {
	for _, name := range BuiltinNames() {
		th, err := Builtin(name)
		if err != nil {
			t.Fatal(err)
		}
		v := reflect.ValueOf(th)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if f.Type.Kind() == reflect.Interface && v.Field(i).IsNil() {
				t.Errorf("%s: token %s is nil", name, f.Name)
			}
		}
	}
}

func TestBuiltinLookup(t *testing.T) {
	if _, err := Builtin("YTM-Dark"); err != nil {
		t.Errorf("case-insensitive lookup: %v", err)
	}
	if _, err := Builtin("nope"); err == nil {
		t.Error("unknown theme accepted")
	}
	if Fallback().Name != "ytm-dark" {
		t.Error("fallback")
	}
}

func TestBuiltinsShipped(t *testing.T) {
	want := []string{"gruvbox-dark", "nord", "ytm-dark", "ytm-light"}
	if got := BuiltinNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("BuiltinNames = %v, want %v", got, want)
	}
	if _, err := Builtin("../theme"); err == nil {
		t.Error("path traversal accepted")
	}
}

// Built-ins are hand-picked palettes: contrast enforcement must not need
// to touch them.
func TestBuiltinsMeetContrast(t *testing.T) {
	for _, name := range BuiltinNames() {
		th, _ := Builtin(name)
		if _, adj := EnforceContrast(th, th.Bg); len(adj) != 0 {
			t.Errorf("%s needs adjustments: %v", name, adj)
		}
	}
}
