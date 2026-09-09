// Command generate renders the committed artifacts: completions, the README
// command section, and the JSON Schemas. --check fails when any differs.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/roshbhatia/go-utils/completion"
	goconfig "github.com/roshbhatia/go-utils/config"
	"github.com/roshbhatia/tether/internal/command"
	"github.com/roshbhatia/tether/internal/config"
	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
)

var completionFiles = map[string]string{
	"bash": "completions/tether.bash",
	"fish": "completions/tether.fish",
	"nu":   "completions/tether.nu",
	"zsh":  "completions/_tether",
}

var tshCompletionFiles = map[string]string{
	"bash": "completions/tsh.bash",
	"fish": "completions/tsh.fish",
	"nu":   "completions/tsh.nu",
	"zsh":  "completions/_tsh",
}

func main() {
	check := flag.Bool("check", false, "fail when a generated artifact differs")
	flag.Parse()

	specification := command.Specification()
	for shell, path := range completionFiles {
		rendered, err := completion.Generate(shell, specification)
		if err != nil {
			fail(err)
		}
		update(path, []byte(rendered), *check)
	}

	tsh := command.TSHSpecification()
	for shell, path := range tshCompletionFiles {
		rendered, err := completion.Generate(shell, tsh)
		if err != nil {
			fail(err)
		}
		update(path, []byte(rendered), *check)
	}

	readme, err := os.ReadFile("README.md")
	if err != nil {
		fail(err)
	}
	rendered, err := completion.ReplaceSection(string(readme), "tsh", completion.Markdown(tsh))
	if err != nil {
		fail(err)
	}
	rendered, err = completion.ReplaceSection(rendered, "commands", completion.Markdown(specification))
	if err != nil {
		fail(err)
	}
	update("README.md", []byte(rendered), *check)

	for path, render := range map[string]func() ([]byte, error){
		"schema/tether.plan.v1.schema.json":  func() ([]byte, error) { return goconfig.Schema[plan.Output](plan.Version) },
		"schema/tether.host.v1.schema.json":  func() ([]byte, error) { return goconfig.Schema[probe.Host](probe.HostVersion) },
		"schema/tether.hosts.v1.schema.json": func() ([]byte, error) { return goconfig.Schema[hosts.Output](hosts.Version) },
		"schema/config.schema.json":          config.Schema,
	} {
		schema, err := render()
		if err != nil {
			fail(err)
		}
		update(path, schema, *check)
	}
}

func update(path string, content []byte, check bool) {
	current, err := os.ReadFile(path)
	if check {
		if err != nil {
			fail(err)
		}
		if !bytes.Equal(current, content) {
			fail(fmt.Errorf("%s is stale; run ./hack/generate.sh", path))
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
