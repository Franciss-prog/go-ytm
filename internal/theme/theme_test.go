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
