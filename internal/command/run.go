package command

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/go-utils/ui"
)

var specification = completion.Command{
	Name:            "tether",
	Description:     "Print a greeting",
	Synopsis:        "tether [--json] [--version] [name]",
	LongDescription: "Print a human-readable greeting or one JSON record.",
	Flags: []completion.Flag{
		{Name: "json", Description: "Write JSON to stdout"},
		{Name: "version", Description: "Write the version to stdout"},
		{Name: "help", Short: "h", Description: "Print command help"},
	},
	Subcommands: []completion.Command{{
		Name:            "completion",
		Description:     "Generate shell completions",
		Synopsis:        "tether completion <bash|fish|nu|zsh>",
		LongDescription: "Write a shell definition to stdout.",
	}},
}

type Streams struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

type result struct {
	Message string    `json:"message"`
	Status  ui.Status `json:"status"`
}

func Run(args []string, streams Streams) int {
	if len(args) > 0 && args[0] == "completion" {
		return runCompletion(args[1:], streams)
	}

	flags := flag.NewFlagSet(specification.Name, flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	flags.Usage = func() {
		_, _ = io.WriteString(streams.Stderr, completion.Text(specification))
	}
	jsonOutput := flags.Bool("json", false, flagDescription("json"))
	showVersion := flags.Bool("version", false, flagDescription("version"))
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(streams.Stdout, streams.Version)
		return 0
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(streams.Stderr, "%s accepts at most one name\n", specification.Name)
		return 2
	}

	name := "world"
	if flags.NArg() == 1 {
		name = flags.Arg(0)
	}
	output := result{Message: "hello, " + name, Status: ui.StatusDone}
	if *jsonOutput {
		if err := json.NewEncoder(streams.Stdout).Encode(output); err != nil {
			fmt.Fprintf(streams.Stderr, "write output: %v\n", err)
			return 1
		}
		return 0
	}
	if _, err := fmt.Fprintln(streams.Stdout, output.Message); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		fmt.Fprintf(streams.Stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}

func Specification() completion.Command {
	return specification
}

func runCompletion(args []string, streams Streams) int {
	if len(args) != 1 {
		fmt.Fprintf(streams.Stderr, "usage: %s completion <bash|fish|nu|zsh>\n", specification.Name)
		return 2
	}
	rendered, err := completion.Generate(args[0], specification)
	if err != nil {
		fmt.Fprintln(streams.Stderr, err)
		return 2
	}
	if _, err := io.WriteString(streams.Stdout, rendered); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		fmt.Fprintf(streams.Stderr, "write completion: %v\n", err)
		return 1
	}
	return 0
}

func flagDescription(name string) string {
	for _, option := range specification.Flags {
		if option.Name == name {
			return option.Description
		}
	}
	panic("unknown command flag: " + name)
}
