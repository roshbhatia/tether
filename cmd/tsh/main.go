// Command tsh is the porcelain: ssh's argv shape with the hop negotiated. It
// is a second main over the same command package rather than an argv[0]
// switch, so a wrapper or `nix run` that rewrites argv[0] cannot turn it back
// into tether, and the version ldflag lands in both binaries.
package main

import (
	"os"

	"github.com/roshbhatia/tether/internal/command"
)

var version = "dev"

func main() {
	os.Exit(command.RunTSH(os.Args[1:], command.Streams{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
	}))
}
