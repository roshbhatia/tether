package command

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/roshbhatia/tether/internal/config"
	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
)

// connectOptions are the connect flags. The host is positional and the flags
// may follow it, so the flag set is parsed twice.
type connectOptions struct {
	host    string
	session string
	mode    string
	pin     string
	dryRun  bool
	quiet   bool
	noProbe bool
	inner   []string
}

func parseConnect(args []string, streams Streams) (connectOptions, int, bool) {
	spec := subcommand("connect")
	flags := newFlagSet(spec, streams)
	var options connectOptions
	flags.StringVar(&options.session, "session", "", "")
	flags.StringVar(&options.mode, "mode", "", "")
	flags.StringVar(&options.pin, "pin", "", "")
	flags.BoolVar(&options.dryRun, "dry-run", false, "")
	flags.BoolVar(&options.quiet, "quiet", false, "")
	flags.BoolVar(&options.noProbe, "no-probe", false, "")
	if err := flags.Parse(args); err != nil {
		return options, usageExit(err), false
	}
	rest := flags.Args()
	if len(rest) == 0 || rest[0] == "--" || onlyInner(args, rest) {
		fmt.Fprintln(streams.Stderr, "tether connect: a host is required")
		flags.Usage()
		return options, 2, false
	}
	options.host, rest = rest[0], rest[1:]
	if len(rest) > 0 && rest[0] != "--" && strings.HasPrefix(rest[0], "-") {
		if err := flags.Parse(rest); err != nil {
			return options, usageExit(err), false
		}
		rest = flags.Args()
	} else if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	options.inner = rest
	return options, 0, true
}

// onlyInner reports whether every remaining argument follows a --, so no
// host was named. flag.Parse drops the -- itself.
func onlyInner(args, rest []string) bool {
	for i, arg := range args {
		if arg == "--" {
			return len(rest) == len(args)-i-1
		}
	}
	return false
}

func runConnect(args []string, streams Streams) int {
	options, code, ok := parseConnect(args, streams)
	if !ok {
		return code
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether connect: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	tools := streams.tools()

	target, known := streams.resolver(settings).Resolve(ctx, options.host)
	if !known && !options.quiet {
		fmt.Fprintf(streams.Stderr, "tether: %s is not in the ssh config and not a tailnet peer; trying ssh anyway\n", options.host)
	}
	if target.Peer != nil && !target.Peer.Online {
		fmt.Fprintf(streams.Stderr, "tether: %s is offline on the tailnet (%s)\n", target.Name, lastSeen(*target.Peer))
		return 1
	}

	record, cached, err := probe.Read(target.Name)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether connect: %v\n", err)
		return 1
	}
	if !options.noProbe && (!cached || record.Stale(tools.Now())) {
		if !options.quiet {
			fmt.Fprintf(streams.Stderr, "tether: probing %s\n", target.Name)
		}
		probed, err := probe.Run(ctx, tools, target.Name, probe.Options{Target: target.Target})
		if err != nil {
			fmt.Fprintf(streams.Stderr, "tether: probe failed: %v\n", err)
		} else {
			record, cached = probed, true
		}
	}

	session := options.session
	if session == "" {
		session = settings.Defaults.Session
	}
	inner, note := defaultInner(options.inner, session, target.Name, record.Remote, cached)
	if note != "" && !options.quiet {
		fmt.Fprintln(streams.Stderr, "tether: "+note)
	}

	req := request{Session: session, Mode: options.mode, Pin: options.pin, Inner: inner}
	if ref, ok := nativeRef(ctx, streams, tools, target); ok {
		req.Native = ref
	}
	output, err := rankHost(ctx, streams, settings, target, req)
	if err != nil && !isRankError(err) {
		fmt.Fprintf(streams.Stderr, "tether connect: %v\n", err)
		return 1
	}
	if options.dryRun {
		if code := writeJSON(streams, output); code != 0 {
			return code
		}
		if err != nil {
			return 2
		}
		return 0
	}
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether: %v\n", err)
		for _, reason := range output.Reasons {
			fmt.Fprintln(streams.Stderr, "  "+reason)
		}
		return 2
	}
	return realize(ctx, streams, tools, output, record.Remote.Home, options.quiet)
}

func lastSeen(peer probe.Peer) string {
	if peer.LastSeen == nil {
		return "last seen unknown"
	}
	return "last seen " + peer.LastSeen.UTC().Format("2006-01-02T15:04:05Z")
}

// defaultInner picks the far-side command when the caller gave none. A
// session attaches through whatever mux the probe saw on the remote; with
// none, the hop opens its login shell and the note says why.
func defaultInner(inner []string, session, host string, remote probe.Remote, known bool) ([]string, string) {
	if len(inner) > 0 {
		return inner, ""
	}
	if session == "" {
		return nil, ""
	}
	switch {
	case !known:
		return nil, fmt.Sprintf("no host record for %s; opening a login shell instead of session %s", host, session)
	case remote.Zmx != "":
		return []string{"zmx", "attach", session}, ""
	case remote.Tmux != "":
		return []string{"tmux", "new", "-A", "-s", session}, ""
	default:
		return nil, fmt.Sprintf("neither zmx nor tmux on %s; opening a login shell instead of session %s", host, session)
	}
}

// nativeRef names the caller's WezTerm ssh domain, only when this process runs
// inside WezTerm, the mux answers, and the host is an ssh config alias (the
// domains are built from those). From a plain terminal there is no native hop.
func nativeRef(ctx context.Context, streams Streams, tools probe.Tools, target hosts.Host) (string, bool) {
	if streams.getenv("WEZTERM_PANE") == "" {
		return "", false
	}
	if target.Source != hosts.SourceSSHConfig && target.Source != hosts.SourceBoth {
		return "", false
	}
	wezterm, err := tools.LookPath("wezterm")
	if err != nil || tools.Run == nil {
		return "", false
	}
	if _, _, err := tools.Run(ctx, wezterm, "cli", "list", "--format", "json"); err != nil {
		return "", false
	}
	return "ssh:" + target.Name, true
}

// realize runs the chosen hop: a local hop replaces this process; a native
// hop opens a tab in the WezTerm domain the ref names. The remote mux server
// spawns a native command itself, and `wezterm cli spawn` would hand it this
// pane's cwd, which does not exist over there, so the remote home (or /) is
// passed instead.
func realize(ctx context.Context, streams Streams, tools probe.Tools, output plan.Output, remoteHome string, quiet bool) int {
	if output.Plan == nil || output.Hop == nil {
		fmt.Fprintln(streams.Stderr, "tether: plan has no command")
		return 1
	}
	loses := ""
	if len(output.Loses) > 0 {
		loses = " (loses " + strings.Join(output.Loses, ", ") + ")"
	}
	switch output.Hop.Kind {
	case "native":
		wezterm, err := tools.LookPath("wezterm")
		if err != nil || tools.Run == nil {
			fmt.Fprintln(streams.Stderr, "tether: native hop chosen but wezterm is absent")
			return 1
		}
		args := []string{"cli", "spawn", "--domain-name", output.Hop.Ref}
		if len(output.Plan.Command) > 0 {
			cwd := remoteHome
			if cwd == "" {
				cwd = "/"
			}
			args = append(args, "--cwd", cwd, "--")
			args = append(args, output.Plan.Command...)
		}
		out, errOut, err := tools.Run(ctx, wezterm, args...)
		if err != nil {
			message := strings.TrimSpace(string(errOut))
			if message == "" {
				message = err.Error()
			}
			fmt.Fprintf(streams.Stderr, "tether: wezterm cli spawn --domain-name %s: %s\n", output.Hop.Ref, message)
			return 1
		}
		if !quiet {
			fmt.Fprintf(streams.Stderr, "tether: %s -> %s in %s, pane %s%s\n", output.Chosen.Tier, output.Host, output.Hop.Ref, strings.TrimSpace(string(out)), loses)
		}
		return 0
	case "local":
		if len(output.Plan.Command) == 0 {
			fmt.Fprintln(streams.Stderr, "tether: plan has no command")
			return 1
		}
		path, err := tools.LookPath(output.Plan.Command[0])
		if err != nil {
			fmt.Fprintf(streams.Stderr, "tether: %s: %v\n", output.Plan.Command[0], err)
			return 1
		}
		if !quiet {
			fmt.Fprintf(streams.Stderr, "tether: %s -> %s%s\n", output.Chosen.Tier, output.Host, loses)
		}
		if err := streams.exec(path, output.Plan.Command, os.Environ()); err != nil {
			fmt.Fprintf(streams.Stderr, "tether: exec %s: %v\n", path, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(streams.Stderr, "tether: unknown hop kind %q\n", output.Hop.Kind)
		return 1
	}
}
