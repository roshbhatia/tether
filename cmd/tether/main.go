package main

import (
	"os"

	"github.com/roshbhatia/tether/internal/command"
)

var version = "dev"

func main() {
	os.Exit(command.Run(os.Args[1:], command.Streams{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
	}))
}
