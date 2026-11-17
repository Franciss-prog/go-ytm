package cli

import "os"

// shutdownSignals end the player cleanly (spec 01).
var shutdownSignals = []os.Signal{os.Interrupt}
