// Package command is the tether CLI: plan, probe, status, completion.
// Every command writes JSON to stdout; diagnostics go to stderr.
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
	"time"

	"github.com/roshbhatia/go-utils/completion"
	"github.com/roshbhatia/tether/internal/config"
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
	Synopsis:        "tether [--version] <plan|probe|status|completion>",
	LongDescription: "Resolve which transport carries an inner command to a host, from what both ends have installed. Output is JSON.",
	Flags: []completion.Flag{
		{Name: "version", Description: "Write the version to stdout"},
		{Name: "help", Short: "h", Description: "Print command help"},
	},
	Subcommands: []completion.Command{
		{
			Name:            "plan",
			Description:     "Choose a tier from the cache, never the network",
			Synopsis:        "tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]",
			LongDescription: "Read the local tools, the tailscale registry, and the cached remote inventory, then rank the tiers. The inner argv after -- is wrapped, never composed.",
			Flags: []completion.Flag{
				{Name: "host", Description: "ssh host alias", Value: true},
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
				{Name: "host", Description: "ssh host alias", Value: true},
				{Name: "force", Description: "Probe even inside the unreachable-host backoff"},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:            "status",
			Description:     "Show the cached host records",
			Synopsis:        "tether status [--host <host>]",
			LongDescription: "List every host record with its age and staleness, or one host.",
			Flags: []completion.Flag{
				{Name: "host", Description: "Only this host", Value: true},
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
}

func (streams Streams) tools() probe.Tools {
	if streams.Tools != nil {
		return *streams.Tools
	}
	return probe.DefaultTools()
}

// Specification is the command tree for completions and docs.
func Specification() completion.Command { return specification }

// Run dispatches one invocation and returns the exit code.
func Run(args []string, streams Streams) int {
	if len(args) > 0 {
		switch args[0] {
		case "plan":
			return runPlan(args[1:], streams)
		case "probe":
			return runProbe(args[1:], streams)
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
	inner := flags.Args()

	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}
	prefs := rank.Prefs{
		Mode:       rank.Mode(settings.ModeFor(*host)),
		Pin:        settings.PinFor(*host),
		FlakyRTTMs: settings.Flaky.RTTMs,
		FlakyLoss:  settings.Flaky.Loss,
	}
	if *mode != "" {
		prefs.Mode = rank.Mode(*mode)
	}
	if *pin != "" {
		prefs.Pin = *pin
	}

	tools := streams.tools()
	now := tools.Now()
	ctx := context.Background()
	local := probe.LocalLayer(ctx, tools, *host)
	registry := probe.RegistryLayer(ctx, tools, *host, local.Hostname)
	record, cached, err := probe.Read(*host)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}

	caps := rank.Caps{
		LocalMosh:       local.Mosh != "",
		LocalWezterm:    local.Wezterm != "",
		LocalSSH:        local.SSH != "",
		RegistryOffline: registry.Present && registry.Peer && !registry.Online,
		NativeRef:       *native,
		NativeRawRef:    *nativeRaw,
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
		facts = append(facts, "remote: no host record; run tether probe --host "+*host)
	}
	facts = append(facts, localFact(local), registryFact(registry))

	result, err := rank.Rank(caps, link, prefs)
	result.Reasons = append(result.Reasons, facts...)
	if result.Offline {
		probeInfo.Stale = true
	}
	var output plan.Output
	if err != nil {
		output = plan.Failure(*host, *session, err, result, probeInfo)
	} else {
		output = plan.Build(*host, *session, inner, plan.Refs{Native: *native, NativeRaw: *nativeRaw}, result, probeInfo)
	}
	if code := writeJSON(streams, output); code != 0 {
		return code
	}
	if err != nil {
		return 1
	}
	return 0
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	record, err := probe.Run(ctx, streams.tools(), *host, probe.Options{Force: *force})
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
	Host      string      `json:"host"`
	At        string      `json:"at"`
	AgeS      int         `json:"age_s"`
	Stale     bool        `json:"stale"`
	RemoteOK  bool        `json:"remote_ok"`
	Reason    string      `json:"reason,omitempty"`
	Backoff   bool        `json:"backoff"`
	LinkFresh bool        `json:"link_fresh"`
	Online    *bool       `json:"online"`
	Pin       string      `json:"pin,omitempty"`
	Mode      string      `json:"mode"`
	Path      string      `json:"path"`
	Record    *probe.Host `json:"record,omitempty"`
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
	now := streams.tools().Now()
	output := statusOutput{Version: StatusVersion, StateDir: probe.Dir(), Config: config.Path(), Hosts: []statusRecord{}}
	for _, record := range records {
		record := record
		entry := statusRecord{
			Host: record.Host, At: record.At.UTC().Format(time.RFC3339), AgeS: int(now.Sub(record.At) / time.Second),
			Stale: record.Stale(now), RemoteOK: record.Remote.OK, Reason: record.Remote.Reason,
			Backoff: record.InBackoff(now), LinkFresh: record.LinkFresh(now),
			Pin: settings.PinFor(record.Host), Mode: string(settings.ModeFor(record.Host)), Path: probe.Path(record.Host),
		}
		if record.Registry.Peer {
			online := record.Registry.Online
			entry.Online = &online
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
