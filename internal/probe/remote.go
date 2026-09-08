package probe

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// remoteTools are the far-side programs the tiers depend on.
var remoteTools = []string{"mosh-server", "zmx", "tmux", "wezterm-mux-server"}

// remoteScript runs under sh so the remote login shell does not matter
// (a nushell login shell has no `command -v`). One line, no single quotes.
const remoteScript = `for t in mosh-server zmx tmux wezterm-mux-server; do p=$(command -v "$t" 2>/dev/null) || continue; printf "path:%s=%s\n" "$t" "$p"; case $t in tmux) v=$(tmux -V 2>&1 | head -n 1);; *) v=$("$t" --version 2>&1 | head -n 1);; esac; printf "version:%s=%s\n" "$t" "$v"; done; exit 0`

// RemoteScript is the exact remote command line, exposed for tests and docs.
func RemoteScript() string {
	return "sh -c '" + remoteScript + "'"
}

// RemoteLayer does the one ssh round trip. BatchMode forbids prompts and
// ConnectTimeout bounds a silent host.
func RemoteLayer(ctx context.Context, tools Tools, host string, now time.Time) Remote {
	remote := Remote{Versions: map[string]string{}, At: now}
	ssh := tools.lookPath("ssh")
	if ssh == "" || tools.Run == nil {
		remote.Reason = "local ssh absent"
		return remote
	}
	ctx, cancel := context.WithTimeout(ctx, ConnectTimeout+15*time.Second)
	defer cancel()
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + strconv.Itoa(int(ConnectTimeout/time.Second)),
		host, "--", RemoteScript(),
	}
	out, errOut, err := tools.Run(ctx, ssh, args...)
	if err != nil {
		remote.Reason = remoteFailure(err, errOut)
		return remote
	}
	remote.OK = true
	parseRemoteInventory(out, &remote)
	return remote
}

func remoteFailure(err error, errOut []byte) string {
	message := strings.TrimSpace(string(errOut))
	if message == "" {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			message = strings.TrimSpace(string(exit.Stderr))
		}
	}
	if message == "" {
		message = err.Error()
	}
	if last := lastLine(message); last != "" {
		message = last
	}
	return fmt.Sprintf("ssh: %s", message)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func parseRemoteInventory(out []byte, remote *Remote) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		kind, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		tool, value, ok := strings.Cut(rest, "=")
		if !ok {
			continue
		}
		switch kind {
		case "path":
			switch tool {
			case "mosh-server":
				remote.MoshServer = value
			case "zmx":
				remote.Zmx = value
			case "tmux":
				remote.Tmux = value
			case "wezterm-mux-server":
				remote.WeztermMuxServer = value
			}
		case "version":
			// Some tools pad with tabs; keep one space between fields.
			if value = strings.Join(strings.Fields(value), " "); value != "" {
				remote.Versions[tool] = value
			}
		}
	}
}
