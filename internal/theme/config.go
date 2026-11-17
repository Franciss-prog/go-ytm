package theme

import (
	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
)

// SpecFromConfig builds a Spec from [theme] settings. Paths may start
// with "~"; blank paths take the spec 08 defaults.
func SpecFromConfig(c config.Theme, configDir, home string) Spec {
	exp := func(p string) string {
		if p == "" {
			return ""
		}
		return platform.ExpandHome(p, home)
	}
	return Spec{
		Source: c.Source, Name: c.Name, Fallback: c.Fallback,
		PywalPath: exp(c.PywalPath), MatugenPath: exp(c.MatugenPath), File: exp(c.File),
		ConfigDir: configDir, Home: home, ANSIAccent: c.ANSIAccent, PywalMap: c.PywalMap,
		EnforceContrast: c.EnforceContrast, Transparent: c.Transparent, Watch: c.Watch,
	}
}
