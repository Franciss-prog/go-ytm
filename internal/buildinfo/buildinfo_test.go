package buildinfo

import "testing"

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.1.0":                             true,
		"v1.2.3-rc.1":                        true,
		"":                                   false,
		"(devel)":                            false,
		"v0.0.0-20261116131842-eeb8cd105834": false,
		"v0.0.0-20261116131842-eeb8cd105834+dirty": false,
	} {
		if got := isRelease(v); got != want {
			t.Errorf("isRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestGetFillsRuntime(t *testing.T) {
	i := Get()
	if i.GoVersion == "" || i.OS == "" || i.Arch == "" || i.Version == "" {
		t.Fatalf("incomplete info: %+v", i)
	}
}
