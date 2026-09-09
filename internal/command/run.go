// Package command is the tether CLI: connect, plan, probe, hosts, status,
// completion. Diagnostics go to stderr; plan, probe, and status write JSON.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/tether/internal/config"
	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
	"github.com/roshbhatia/tether/internal/rank"
)

// StatusVersion tags the status output.
const StatusVersion = "tether.status/v1"

var tierNames = []string{rank.NativeMux, rank.MoshMux, rank.SSHRaw, rank.SSH}
var modeNames = []string{string(config.ModeAuto), string(config.ModeNative), string(config.ModeRoam), string(config.ModePersist)}

var specification = completion.Command{
	Name:            "tether",
	Description:     "Pick the hop that carries a remote session",
	Synopsis:        "tether [--version] <connect|plan|probe|hosts|status|completion>",
	LongDescription: "Resolve which transport carries an inner command to a host, from what both ends have installed, and run it. connect execs the hop; plan, probe, hosts --json, and status write JSON.",
	Flags: []completion.Flag{
		{Name: "version", Description: "Write the version to stdout"},
		{Name: "help", Short: "h", Description: "Print command help"},
	},
	Subcommands: []completion.Command{
		{
			Name:              "connect",
			Description:       "Probe if needed, plan, then exec the hop",
			Synopsis:          "tether connect <host> [--session <name>] [--mode <mode>] [--pin <tier>] [--dry-run] [--quiet] [--no-probe] [-- <inner argv>]",
			LongDescription:   "Resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and replace this process with the winning local hop. Inside WezTerm an ssh-config host may win native-mux, which opens a tab in the ssh:<host> domain instead.",
			CompletionCommand: []string{"tether", "hosts", "--names"},
			Flags: []completion.Flag{
				{Name: "session", Description: "Session to attach with zmx or tmux when no inner argv is given; overrides defaults.session", Value: true},
				{Name: "mode", Description: "Ordering mode; overrides the config", Value: true, Values: modeNames},
				{Name: "pin", Description: "Tier that must win; overrides the config", Value: true, Values: tierNames},
				{Name: "dry-run", Description: "Write the plan JSON and exit without connecting"},
				{Name: "quiet", Description: "Do not announce the winning hop on stderr"},
				{Name: "no-probe", Description: "Never run the ssh round trip; plan from the cache only"},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:            "plan",
			Description:     "Choose a tier from the cache, never the network",
			Synopsis:        "tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]",
			LongDescription: "Read the local tools, the tailscale registry, and the cached remote inventory, then rank the tiers. The inner argv after -- is wrapped, never composed.",
			Flags: []completion.Flag{
				{Name: "host", Description: "ssh config alias or tailnet peer", Value: true, CompletionCommand: []string{"tether", "hosts", "--names"}},
				{Name: "session", Description: "Session name echoed in the output", Value: true},
				{Name: "native", Description: "Opaque ref of the caller's native mux domain", Value: true},
				{Name: "native-raw", Description: "Opaque ref of the caller's raw ssh domain", Value: true},
				{Name: "mode", Description: "Ordering mode; overrides the config", Value: true, Values: modeNames},
				{Name: "pin", Description: "Tier that must win; overrides the config", Value: true, Values: tierNames},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:            "probe",
			Description:     "Measure both ends and write the host record",
			Synopsis:        "tether probe --host <host> [--force]",
			LongDescription: "Run the local, registry, remote, and link layers. One ssh round trip; a tailnet host the registry says is offline gets none.",
			Flags: []completion.Flag{
				{Name: "host", Description: "ssh config alias or tailnet peer", Value: true, CompletionCommand: []string{"tether", "hosts", "--names"}},
				{Name: "force", Description: "Probe even inside the unreachable-host backoff"},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:            "hosts",
			Description:     "List the hosts connect can name",
			Synopsis:        "tether hosts [--json] [--names]",
			LongDescription: "The union of the literal Host entries in ~/.ssh/config and the peers of the tailnet, with each host's source, online state, OS, and whether it advertises Tailscale SSH.",
			Flags: []completion.Flag{
				{Name: "json", Description: "Write tether.hosts/v1 instead of a table"},
				{Name: "names", Description: "Write one name per line, for completion"},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:            "status",
			Description:     "Show the cached host records",
			Synopsis:        "tether status [--host <host>]",
			LongDescription: "List every host record with its age, staleness, and live tailnet state, or one host.",
			Flags: []completion.Flag{
				{Name: "host", Description: "Only this host", Value: true, CompletionCommand: []string{"tether", "hosts", "--names"}},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:            "completion",
			Description:     "Generate shell completions",
			Synopsis:        "tether completion <bash|fish|nu|zsh>",
			LongDescription: "Write a shell definition to stdout.",
		},
	},
}

// Streams are the process edges, injected for tests.
type Streams struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	// Tools defaults to the real process boundary.
	Tools *probe.Tools
	// Exec replaces the process; it only returns on failure. Nil means
	// syscall.Exec.
	Exec func(path string, argv []string, env []string) error
	// Getenv defaults to os.Getenv.
	Getenv func(key string) string
	// SSHConfig defaults to ~/.ssh/config.
	SSHConfig string
}

func (streams Streams) tools() probe.Tools {
	if streams.Tools != nil {
		return *streams.Tools
	}
	return probe.DefaultTools()
}

func (streams Streams) exec(path string, argv []string, env []string) error {
	if streams.Exec != nil {
		return streams.Exec(path, argv, env)
	}
	return syscall.Exec(path, argv, env)
}

func (streams Streams) getenv(key string) string {
	if streams.Getenv != nil {
		return streams.Getenv(key)
	}
	return os.Getenv(key)
}

func (streams Streams) resolver(settings config.Config) hosts.Resolver {
	return hosts.Resolver{Tools: streams.tools(), SSHConfig: streams.SSHConfig, UserFor: settings.UserFor}
}

// Specification is the command tree for completions and docs.
func Specification() completion.Command { return specification }

// Run dispatches one invocation and returns the exit code.
func Run(args []string, streams Streams) int {
	if len(args) > 0 {
		switch args[0] {
		case "connect":
			return runConnect(args[1:], streams)
		case "plan":
			return runPlan(args[1:], streams)
		case "probe":
			return runProbe(args[1:], streams)
		case "hosts":
			return runHosts(args[1:], streams)
		case "status":
			return runStatus(args[1:], streams)
		case "completion":
			return runCompletion(args[1:], streams)
		}
	}
	flags := newFlagSet(specification, streams)
	showVersion := flags.Bool("version", false, "")
	if err := flags.Parse(args); err != nil {
		return usageExit(err)
	}
	if *showVersion {
		fmt.Fprintln(streams.Stdout, streams.Version)
		return 0
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(streams.Stderr, "tether: unknown command %q\n", flags.Arg(0))
	}
	flags.Usage()
	return 2
}

func newFlagSet(spec completion.Command, streams Streams) *flag.FlagSet {
	flags := flag.NewFlagSet(spec.Name, flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	flags.Usage = func() { _, _ = io.WriteString(streams.Stderr, completion.Text(spec)) }
	return flags
}

func subcommand(name string) completion.Command {
	for _, spec := range specification.Subcommands {
		if spec.Name == name {
			return spec
		}
	}
	panic("unknown subcommand: " + name)
}

func usageExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func runPlan(args []string, streams Streams) int {
	spec := subcommand("plan")
	flags := newFlagSet(spec, streams)
	host := flags.String("host", "", "")
	session := flags.String("session", "", "")
	native := flags.String("native", "", "")
	nativeRaw := flags.String("native-raw", "", "")
	mode := flags.String("mode", "", "")
	pin := flags.String("pin", "", "")
	if err := flags.Parse(args); err != nil {
		return usageExit(err)
	}
	if *host == "" {
		fmt.Fprintln(streams.Stderr, "tether plan: --host is required")
		return 2
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}
	ctx := context.Background()
	target, _ := streams.resolver(settings).Resolve(ctx, *host)
	output, err := rankHost(ctx, streams, settings, target, request{
		Session: *session, Native: *native, NativeRaw: *nativeRaw, Mode: *mode, Pin: *pin, Inner: flags.Args(),
	})
	if err != nil && !isRankError(err) {
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}
	if code := writeJSON(streams, output); code != 0 {
		return code
	}
	if err != nil {
		return 1
	}
	return 0
}

// request is one plan: the caller's refs and overrides plus the inner argv.
type request struct {
	Session   string
	Native    string
	NativeRaw string
	Mode      string
	Pin       string
	Inner     []string
}

// rankError marks a ranking failure, such as an unavailable pin, whose plan
// document still renders. Other errors are I/O.
type rankError struct{ err error }

func (err rankError) Error() string { return err.err.Error() }
func (err rankError) Unwrap() error { return err.err }

func isRankError(err error) bool {
	var target rankError
	return errors.As(err, &target)
}

// rankHost runs the plan layers (never the network) for a resolved host and
// renders the tether.plan/v1 document.
func rankHost(ctx context.Context, streams Streams, settings config.Config, target hosts.Host, req request) (plan.Output, error) {
	prefs := rank.Prefs{
		Mode:       rank.Mode(settings.ModeFor(target.Name)),
		Pin:        settings.PinFor(target.Name),
		FlakyRTTMs: settings.Flaky.RTTMs,
		FlakyLoss:  settings.Flaky.Loss,
	}
	if req.Mode != "" {
		prefs.Mode = rank.Mode(req.Mode)
	}
	if req.Pin != "" {
		prefs.Pin = req.Pin
	}

	tools := streams.tools()
	now := tools.Now()
	local := probe.LocalLayer(ctx, tools, target.Target)
	registry := probe.RegistryLayer(ctx, tools, target.Name, local.Hostname)
	record, cached, err := probe.Read(target.Name)
	if err != nil {
		return plan.Output{}, err
	}

	caps := rank.Caps{
		LocalMosh:       local.Mosh != "",
		LocalWezterm:    local.Wezterm != "",
		LocalSSH:        local.SSH != "",
		RegistryOffline: registry.Present && registry.Peer && !registry.Online,
		NativeRef:       req.Native,
		NativeRawRef:    req.NativeRaw,
	}
	link := rank.Link{}
	probeInfo := plan.Probe{Stale: true}
	facts := []string{}
	if cached {
		caps.RemoteKnown = true
		caps.RemoteOK = record.Remote.OK
		caps.RemoteReason = record.Remote.Reason
		caps.RemoteMoshServer = record.Remote.MoshServer != ""
		caps.RemoteZmx = record.Remote.Zmx != ""
		caps.RemoteTmux = record.Remote.Tmux != ""
		caps.RemoteMuxServer = record.Remote.WeztermMuxServer != ""
		at := record.At
		age := int(now.Sub(at) / time.Second)
		probeInfo = plan.Probe{At: &at, AgeS: &age, Stale: record.Stale(now)}
		if record.LinkFresh(now) {
			link = rank.Link{Known: true, RTTMs: record.Link.RTTMs, Loss: record.Link.Loss}
			if record.Link.Direct != nil {
				link.Relayed = !*record.Link.Direct
			}
		}
		facts = append(facts, remoteFact(record.Remote))
	} else {
		facts = append(facts, "remote: no host record; run tether probe --host "+target.Name)
	}
	facts = append(facts, localFact(local), registryFact(registry), transportFact(target))

	result, err := rank.Rank(caps, link, prefs)
	result.Reasons = append(result.Reasons, facts...)
	if result.Offline {
		probeInfo.Stale = true
	}
	subject := plan.Subject{Host: target.Name, Session: req.Session}
	if target.Target != target.Name {
		subject.Target = target.Target
	}
	if err != nil {
		return plan.Failure(subject, err, result, probeInfo), rankError{err}
	}
	return plan.Build(subject, req.Inner, plan.Refs{Native: req.Native, NativeRaw: req.NativeRaw}, result, probeInfo), nil
}

// transportFact records how ssh reaches the host. The `tailscale ssh` wrapper
// is never used: it takes its first argument as the host, so neither ssh -t
// nor mosh --ssh (both put options before the host) can drive it.
func transportFact(target hosts.Host) string {
	switch target.Source {
	case hosts.SourceBoth:
		fact := "transport: ssh alias " + target.Name + " (ssh config, hostname " + target.Hostname + "; tailnet peer"
		if target.Peer != nil && target.Peer.TailscaleSSH {
			fact += ", tailscale ssh advertised"
		}
		return fact + ")"
	case hosts.SourceSSHConfig:
		return "transport: ssh alias " + target.Name + " (ssh config, hostname " + target.Hostname + ")"
	case hosts.SourceTailnet:
		fact := "transport: ssh to " + target.Target + " (tailnet peer, not in ssh config"
		if target.Peer != nil && target.Peer.TailscaleSSH {
			fact += "; tailscale ssh advertised"
		}
		return fact + ")"
	default:
		return "transport: ssh to " + target.Target + " (not in ssh config, not a tailnet peer)"
	}
}

func remoteFact(remote probe.Remote) string {
	if !remote.OK {
		return "remote: unreachable, " + remote.Reason
	}
	var present []string
	for _, tool := range []struct{ name, path string }{
		{"mosh-server", remote.MoshServer}, {"zmx", remote.Zmx}, {"tmux", remote.Tmux}, {"wezterm-mux-server", remote.WeztermMuxServer},
	} {
		if tool.path == "" {
			continue
		}
		if version := remote.Versions[tool.name]; version != "" {
			present = append(present, tool.name+" "+strings.TrimPrefix(version, tool.name+" "))
		} else {
			present = append(present, tool.name)
		}
	}
	if len(present) == 0 {
		return "remote: none of mosh-server, zmx, tmux, wezterm-mux-server"
	}
	return "remote: " + strings.Join(present, ", ")
}

func localFact(local probe.Local) string {
	var parts []string
	for _, tool := range []struct{ name, path string }{{"mosh", local.Mosh}, {"wezterm", local.Wezterm}, {"ssh", local.SSH}} {
		state := "absent"
		if tool.path != "" {
			state = "present"
		}
		parts = append(parts, tool.name+" "+state)
	}
	return "local: " + strings.Join(parts, ", ")
}

func registryFact(registry probe.Registry) string {
	switch {
	case !registry.Present:
		return "registry: tailscale absent"
	case !registry.Peer:
		return "registry: host is not a tailnet peer"
	case !registry.Online:
		return "registry: peer offline"
	case registry.Direct:
		return "registry: peer online, direct path active"
	default:
		return "registry: peer online, relay " + registry.Relay
	}
}

func runHosts(args []string, streams Streams) int {
	spec := subcommand("hosts")
	flags := newFlagSet(spec, streams)
	asJSON := flags.Bool("json", false, "")
	names := flags.Bool("names", false, "")
	if err := flags.Parse(args); err != nil {
		return usageExit(err)
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(streams.Stderr, "tether hosts: takes no positional arguments")
		return 2
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether hosts: %v\n", err)
		return 1
	}
	output, err := streams.resolver(settings).List(context.Background())
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether hosts: %v\n", err)
		return 1
	}
	switch {
	case *asJSON:
		return writeJSON(streams, output)
	case *names:
		for _, host := range output.Hosts {
			fmt.Fprintln(streams.Stdout, host.Name)
		}
		return 0
	}
	rows := [][]string{{"NAME", "SOURCE", "HOSTNAME", "ONLINE", "OS", "TAILSCALE-SSH"}}
	for _, host := range output.Hosts {
		online, os, ssh := "-", "-", "-"
		if host.Peer != nil {
			online, os = "yes", host.Peer.OS
			if !host.Peer.Online {
				online = "no"
				if host.Peer.LastSeen != nil {
					online = "no (seen " + host.Peer.LastSeen.UTC().Format("2006-01-02") + ")"
				}
			}
			ssh = "no"
			if host.Peer.TailscaleSSH {
				ssh = "yes"
			}
		}
		rows = append(rows, []string{host.Name, host.Source, host.Hostname, online, os, ssh})
	}
	writeTable(streams.Stdout, rows)
	return 0
}

func writeTable(out io.Writer, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(cell)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
			}
		}
		fmt.Fprintln(out, line.String())
	}
}

func runProbe(args []string, streams Streams) int {
	spec := subcommand("probe")
	flags := newFlagSet(spec, streams)
	host := flags.String("host", "", "")
	force := flags.Bool("force", false, "")
	if err := flags.Parse(args); err != nil {
		return usageExit(err)
	}
	if *host == "" || flags.NArg() > 0 {
		fmt.Fprintln(streams.Stderr, "tether probe: --host is required and takes no positional arguments")
		return 2
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether probe: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	target, _ := streams.resolver(settings).Resolve(ctx, *host)
	record, err := probe.Run(ctx, streams.tools(), target.Name, probe.Options{Force: *force, Target: target.Target})
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether probe: %v\n", err)
		return 1
	}
	return writeJSON(streams, record)
}

type statusOutput struct {
	Version  string         `json:"version"`
	StateDir string         `json:"state_dir"`
	Config   string         `json:"config"`
	Hosts    []statusRecord `json:"hosts"`
}

type statusRecord struct {
	Host      string         `json:"host"`
	At        string         `json:"at"`
	AgeS      int            `json:"age_s"`
	Stale     bool           `json:"stale"`
	RemoteOK  bool           `json:"remote_ok"`
	Reason    string         `json:"reason,omitempty"`
	Backoff   bool           `json:"backoff"`
	LinkFresh bool           `json:"link_fresh"`
	Tailnet   *tailnetStatus `json:"tailnet" jsonschema:"nullable"`
	Pin       string         `json:"pin,omitempty"`
	Mode      string         `json:"mode"`
	Path      string         `json:"path"`
	Record    *probe.Host    `json:"record,omitempty"`
}

// tailnetStatus is the live tailnet view of one host; nil when the host is
// not a peer.
type tailnetStatus struct {
	Online       bool   `json:"online"`
	Path         string `json:"path" jsonschema:"enum=direct,enum=relay,enum=idle,enum=offline"`
	Relay        string `json:"relay,omitempty"`
	TailscaleSSH bool   `json:"tailscale_ssh"`
	DNSName      string `json:"dns_name"`
}

func tailnetStatusFor(peer probe.Peer) *tailnetStatus {
	entry := &tailnetStatus{Online: peer.Online, Relay: peer.Relay, TailscaleSSH: peer.TailscaleSSH, DNSName: peer.DNSName}
	switch {
	case !peer.Online:
		entry.Path = "offline"
	case peer.Direct:
		entry.Path = "direct"
	case peer.Relay != "":
		entry.Path = "idle"
	}
	return entry
}

func runStatus(args []string, streams Streams) int {
	spec := subcommand("status")
	flags := newFlagSet(spec, streams)
	host := flags.String("host", "", "")
	if err := flags.Parse(args); err != nil {
		return usageExit(err)
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether status: %v\n", err)
		return 1
	}
	var records []probe.Host
	if *host != "" {
		record, ok, err := probe.Read(*host)
		if err != nil {
			fmt.Fprintf(streams.Stderr, "tether status: %v\n", err)
			return 1
		}
		if ok {
			records = []probe.Host{record}
		}
	} else if records, err = probe.List(); err != nil {
		fmt.Fprintf(streams.Stderr, "tether status: %v\n", err)
		return 1
	}
	tools := streams.tools()
	now := tools.Now()
	tailnet := probe.LoadTailnet(context.Background(), tools)
	output := statusOutput{Version: StatusVersion, StateDir: probe.Dir(), Config: config.Path(), Hosts: []statusRecord{}}
	for _, record := range records {
		record := record
		entry := statusRecord{
			Host: record.Host, At: record.At.UTC().Format(time.RFC3339), AgeS: int(now.Sub(record.At) / time.Second),
			Stale: record.Stale(now), RemoteOK: record.Remote.OK, Reason: record.Remote.Reason,
			Backoff: record.InBackoff(now), LinkFresh: record.LinkFresh(now),
			Pin: settings.PinFor(record.Host), Mode: string(settings.ModeFor(record.Host)), Path: probe.Path(record.Host),
		}
		if peer, ok := tailnet.Find(record.Host, record.Local.Hostname); ok {
			entry.Tailnet = tailnetStatusFor(peer)
		}
		if *host != "" {
			entry.Record = &record
		}
		output.Hosts = append(output.Hosts, entry)
	}
	return writeJSON(streams, output)
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

func writeJSON(streams Streams, value any) int {
	encoder := json.NewEncoder(streams.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		fmt.Fprintf(streams.Stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}
