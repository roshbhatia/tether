package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/roshbhatia/tether/internal/probe"
)

func (options tshOptions) nativeShape() bool {
	return !options.foreground && options.user == "" && options.session == "" &&
		len(options.options) == 0 && len(options.command) == 0
}

func (streams Streams) liveWezTerm(ctx context.Context, tools probe.Tools) bool {
	if streams.Interactive != nil {
		if !streams.Interactive() {
			return false
		}
	} else {
		file, ok := streams.Stdout.(*os.File)
		if !ok {
			return false
		}
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	pane, err := strconv.ParseUint(streams.getenv("WEZTERM_PANE"), 10, 64)
	if err != nil {
		return false
	}
	wezterm, err := tools.LookPath("wezterm")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	clients, _, err := tools.Run(ctx, wezterm, "cli", "--no-auto-start", "list-clients", "--format", "json")
	var attached []json.RawMessage
	if err != nil || json.Unmarshal(clients, &attached) != nil || len(attached) == 0 {
		return false
	}
	data, _, err := tools.Run(ctx, wezterm, "cli", "--no-auto-start", "list", "--format", "json")
	var panes []struct {
		ID uint64 `json:"pane_id"`
	}
	if err != nil || json.Unmarshal(data, &panes) != nil {
		return false
	}
	for _, live := range panes {
		if live.ID == pane {
			return true
		}
	}
	return false
}

// runSession preserves the remote provider's CLI and keeps its terminal attached.
func runSession(args []string, streams Streams) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		newFlagSet(subcommand("session"), streams).Usage()
		return 0
	}
	valid := len(args) == 2 && args[0] == "list" || len(args) == 3 && args[0] == "attach"
	if !valid || strings.HasPrefix(args[1], "-") || (len(args) == 3 && strings.HasPrefix(args[2], "-")) {
		fmt.Fprintln(streams.Stderr, "Usage: "+subcommand("session").Synopsis)
		return 2
	}
	command := append([]string{"--foreground", "--pin", "ssh", args[1], "--", "wezterm-remote", args[0]}, args[2:]...)
	return connect("tether session", command, streams, subcommand("session"), false)
}
