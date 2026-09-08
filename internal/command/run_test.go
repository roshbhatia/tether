package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunText(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"Roshan"}, Streams{Stdout: &stdout, Stderr: &stderr, Version: "test"})
	if code != 0 || stdout.String() != "hello, Roshan\n" || stderr.Len() != 0 {
		t.Fatalf("Run() = %d, %q, %q", code, stdout.String(), stderr.String())
	}
}

func TestRunCompletion(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "fish", "nu", "zsh"} {
		shell := shell
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run([]string{"completion", shell}, Streams{Stdout: &stdout, Stderr: &stderr})
			if code != 0 || !strings.Contains(stdout.String(), "tether") || stderr.Len() != 0 {
				t.Fatalf("Run() = %d, %q, %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunJSON(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"--json", "Roshan"}, Streams{Stdout: &stdout, Stderr: &stderr, Version: "test"})
	if code != 0 {
		t.Fatalf("Run() = %d: %s", code, stderr.String())
	}
	var output result
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if output.Message != "hello, Roshan" || output.Status != "done" {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func TestRunRejectsExtraArguments(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"one", "two"}, Streams{Stdout: &stdout, Stderr: &stderr, Version: "test"})
	if code != 2 || stderr.String() != "tether accepts at most one name\n" {
		t.Fatalf("Run() = %d, %q", code, stderr.String())
	}
}

func TestRunHelpUsesCommandSpecification(t *testing.T) {
	t.Parallel()

	for _, option := range []string{"-h", "--help"} {
		option := option
		t.Run(option, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run([]string{option}, Streams{Stdout: &stdout, Stderr: &stderr, Version: "test"})
			if code != 0 || stdout.Len() != 0 {
				t.Fatalf("Run() = %d, stdout %q", code, stdout.String())
			}
			for _, want := range []string{"completion  Generate shell completions", "-h, --help", "--json"} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("help lacks %q:\n%s", want, stderr.String())
				}
			}
		})
	}
}
