// Package buildinfo exposes version metadata injected at build time via -ldflags.
package buildinfo

import (
	"regexp"
	"runtime"
	"runtime/debug"
)

// pseudoVersion matches Go module pseudo-versions such as
// v0.0.0-20261116131842-eeb8cd105834 (optionally +dirty).
var pseudoVersion = regexp.MustCompile(`\d{14}-[0-9a-f]{12}`)

// isRelease reports whether v is a tagged module version (as stamped by
// `go install module@vX.Y.Z`), not a VCS pseudo-version.
func isRelease(v string) bool {
	return v != "" && v != "(devel)" && !pseudoVersion.MatchString(v)
}

// Set via -ldflags "-X github.com/Franciss-Prog/go-ytm/internal/buildinfo.Version=...".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info is a snapshot of build metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns build metadata, falling back to the VCS info embedded by the Go
// toolchain when -ldflags were not used (e.g. `go install`).
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "dev" && isRelease(bi.Main.Version) {
			info.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" && len(s.Value) >= 7 {
					info.Commit = s.Value[:7]
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			}
		}
	}
	return info
}
