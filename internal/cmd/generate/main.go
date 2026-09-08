package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/roshbhatia/tether/internal/command"
	"github.com/roshbhatia/go-utils/completion"
)

var completionFiles = map[string]string{
	"bash": "completions/tether.bash",
	"fish": "completions/tether.fish",
	"nu":   "completions/tether.nu",
	"zsh":  "completions/_tether",
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

	readme, err := os.ReadFile("README.md")
	if err != nil {
		fail(err)
	}
	rendered, err := completion.ReplaceSection(string(readme), "commands", completion.Markdown(specification))
	if err != nil {
		fail(err)
	}
	update("README.md", []byte(rendered), *check)
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
