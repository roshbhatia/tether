# tether

A Nix-first template for small Go command-line tools.

The tether keeps stdout machine-readable, writes diagnostics to stderr, and
uses `go-utils` for shared terminal vocabulary.

## Start a project

Create a repository from this template. Then initialize it with an owner,
repository, and optional binary name.

```bash
./hack/init-template.sh OWNER PROJECT [BINARY]
```

The script works on macOS and Linux. It updates the Go module, application
name, Nix package, release metadata, generated help, and completion filenames.
Project and binary names must start with a lowercase letter. They may contain
only lowercase letters, digits, and hyphens.

## Development

```bash
nix develop
go test -race ./...
./hack/generate.sh --check
nix build
nix flake check
```

## Example

```bash
nix run . -- Roshan
nix run . -- --json Roshan
```

## Commands
<!-- BEGIN GENERATED:commands -->

### `tether`

tether [--json] [--version] [name]

Print a human-readable greeting or one JSON record.

| Option | Description |
| --- | --- |
| `--json` | Write JSON to stdout |
| `--version` | Write the version to stdout |
| `--help`, `-h` | Print command help |

### `tether completion`

tether completion <bash|fish|nu|zsh>

Write a shell definition to stdout.

<!-- END GENERATED:commands -->
