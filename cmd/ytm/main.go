// Command ytm is the ytm-go YouTube Music terminal client.
package main

import (
	"context"
	"os"
	"slices"

	"github.com/Franciss-Prog/go-ytm/internal/cli"
)

func main() {
	io := cli.SystemIO()
	// fang renders help and errors through colorprofile, which honors
	// NO_COLOR but not our --no-color flag; bridge the two.
	if slices.Contains(os.Args[1:], "--no-color") {
		_ = os.Setenv("NO_COLOR", "1")
		io.Env = append(io.Env, "NO_COLOR=1")
	}
	os.Exit(cli.Main(context.Background(), os.Args[1:], io))
}
