package command

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/tether/internal/config"
	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
)

// RunTSH is the tsh entry point: `tsh <args>` is `tether connect <args>`
// announcing itself as tsh.
func RunTSH(args []string, streams Streams) int {
	return connect("tsh", args, streams, tshSpecification)
}

func runConnect(args []string, streams Streams) int {
	return connect("tether connect", args, streams, subcommand("connect"))
}

// connect parses an ssh-shaped argv, resolves and probes the host, ranks the
// tiers, and realizes the winner. prog prefixes every diagnostic.
func connect(prog string, args []string, streams Streams, spec completion.Command) int {
	options, err := parseTSH(args)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "%s: %v\n", prog, err)
		_, _ = fmt.Fprint(streams.Stderr, completion.Text(spec))
		return 2
	}
	if options.help {
		_, _ = fmt.Fprint(streams.Stderr, completion.Text(spec))
		return 0
	}
	if options.version {
		fmt.Fprintln(streams.Stdout, streams.Version)
		return 0
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "%s: %v\n", prog, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	tools := streams.tools()
	say := func(format string, values ...any) {
		if !options.quiet {
			fmt.Fprintf(streams.Stderr, prog+": "+format+"\n", values...)
		}
	}

	target, known := streams.resolver(settings).Resolve(ctx, options.host)
	if !known {
		say("%s is not in the ssh config and not a tailnet peer; trying ssh anyway", options.host)
	}
	if options.user != "" {
		target.User = options.user
		target.Target = withUser(options.user, target.Target)
	}
	if target.Peer != nil && !target.Peer.Online {
		fmt.Fprintf(streams.Stderr, "%s: %s is offline on the tailnet (%s)\n", prog, target.Name, lastSeen(*target.Peer))
		return 1
	}

	record, cached, err := probe.Read(target.Name)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "%s: %v\n", prog, err)
		return 1
	}
	if !options.noProbe && (!cached || record.Stale(tools.Now())) {
		say("probing %s", target.Name)
		probed, err := probe.Run(ctx, tools, target.Name, probe.Options{Target: target.Target})
		if err != nil {
			say("probe failed: %v", err)
		} else {
			record, cached = probed, true
		}
	}

	inner, note := defaultInner(options.command, options.session, target.Name, record.Remote, cached)
	if note != "" {
		say("%s", note)
	}
	flags := carry(options.options, options.user != "")
	req := request{
		Session: options.session, Mode: options.mode, Pin: options.pin, Inner: inner,
		Flags:       plan.Flags{SSH: flags.ssh, Mosh: flags.mosh, TTY: flags.tty},
		MoshUnfit:   flags.moshUnfit,
		NativeUnfit: flags.nativeUnfit,
	}
	output, err := rankHost(ctx, streams, settings, target, req)
	if err != nil && !isRankError(err) {
		fmt.Fprintf(streams.Stderr, "%s: %v\n", prog, err)
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
		fmt.Fprintf(streams.Stderr, "%s: %v\n", prog, err)
		for _, reason := range output.Reasons {
			fmt.Fprintln(streams.Stderr, "  "+reason)
		}
		return 2
	}
	return realize(prog, streams, tools, output, options.quiet)
}

func lastSeen(peer probe.Peer) string {
	if peer.LastSeen == nil {
		return "last seen unknown"
	}
	return "last seen " + peer.LastSeen.UTC().Format("2006-01-02T15:04:05Z")
}

// defaultInner picks the far-side command when the caller gave none: a login
// shell, exactly like ssh, unless a session was asked for. A session attaches
// through whatever mux the probe saw on the remote; with none, the hop opens
// the login shell and the note says why.
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

func realize(prog string, streams Streams, tools probe.Tools, output plan.Output, quiet bool) int {
	if output.Plan == nil || output.Hop == nil {
		fmt.Fprintf(streams.Stderr, "%s: plan has no command\n", prog)
		return 1
	}
	loses := ""
	if len(output.Loses) > 0 {
		loses = " (loses " + strings.Join(output.Loses, ", ") + ")"
	}
	switch output.Hop.Kind {
	case "local":
		if len(output.Plan.Command) == 0 {
			fmt.Fprintf(streams.Stderr, "%s: plan has no command\n", prog)
			return 1
		}
		path, err := tools.LookPath(output.Plan.Command[0])
		if err != nil {
			fmt.Fprintf(streams.Stderr, "%s: %s: %v\n", prog, output.Plan.Command[0], err)
			return 1
		}
		if !quiet {
			fmt.Fprintf(streams.Stderr, "%s: %s -> %s%s\n", prog, output.Chosen.Tier, output.Host, loses)
		}
		if err := streams.exec(path, output.Plan.Command, os.Environ()); err != nil {
			fmt.Fprintf(streams.Stderr, "%s: exec %s: %v\n", prog, path, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(streams.Stderr, "%s: unknown hop kind %q\n", prog, output.Hop.Kind)
		return 1
	}
}
