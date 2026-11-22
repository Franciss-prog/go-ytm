package lyrics

import "testing"

func TestTransliterate(t *testing.T) {
	for in, want := range map[string]string{
		"plain ascii":       "",
		"Ты меня не любишь": "Ty menya ne lyubish'",
		"Beyoncé":           "Beyonce",
		"こんにちは":             "konnichiha",
	} {
		if got := Transliterate(in); got != want {
			t.Errorf("Transliterate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVisual(t *testing.T) {
	for in, want := range map[string]string{
		"no rtl here":     "no rtl here",
		"שלום":            "םולש",
		"hello שלום עולם": "hello םלוע םולש",
		"שלום world":      "world םולש",
		"مرحبا 123":       "123 ابحرم",
	} {
		if got := Visual(in); got != want {
			t.Errorf("Visual(%q) = %q, want %q", in, got, want)
		}
	}
	if !HasRTL("abc שלום") || HasRTL("Ελληνικά") {
		t.Error("HasRTL")
	}
}
