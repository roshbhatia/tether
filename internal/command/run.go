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
	"github.com/roshbhatia/tether/internal/negotiation"
	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
	"github.com/roshbhatia/tether/internal/rank"
)

// StatusVersion tags the status output.
const StatusVersion = "tether.status/v1"

var tierNames = rank.TierNames()
var modeNames = rank.ModeNames()

var specification = completion.Command{
	Name:            "tether",
	Description:     "Pick the hop that carries a remote session",
	Synopsis:        "tether [--version] <connect|plan|probe|hosts|status|completion>",
	LongDescription: "The plumbing behind tsh. Resolve which transport carries a command to a host, from what both ends have installed. connect execs the hop; plan, probe, hosts --json, and status write JSON.",
	Flags: []completion.Flag{
		{Name: "version", Description: "Write the version to stdout"},
		{Name: "help", Short: "h", Description: "Print command help"},
	},
	Subcommands: []completion.Command{
		{
			Name:              "connect",
			Description:       "The plumbing behind tsh: negotiate the hop and exec it",
			Synopsis:          "tether connect [ssh options] [-s <session>] [-q] [--pin <tier>] [--mode <mode>] [--dry-run] [--no-probe] [user@]<host> [-- <command>]",
			LongDescription:   "Same argv as tsh: resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and replace this process with the winning local hop. The terminal environment does not change this behavior.",
			CompletionCommand: []string{"tether", "hosts", "--names"},
			Flags:             tshSpecification.Flags,
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
	Store   *probe.Store
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

func (streams Streams) store() probe.Store {
	if streams.Store != nil {
		return *streams.Store
	}
	return probe.DefaultStore()
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

func (streams Streams) coordinator(settings config.Config) negotiation.Coordinator {
	return negotiation.Coordinator{Tools: streams.tools(), Resolver: streams.resolver(settings), Store: streams.store()}
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
	if err := (rank.Prefs{Mode: rank.Mode(*mode), Pin: *pin}).Validate(); err != nil {
		output := plan.Failure(plan.Subject{Host: *host, Session: *session}, err, rank.Result{}, plan.Probe{Stale: true})
		if code := writeJSON(streams, output); code != 0 {
			return code
		}
		return 1
	}
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}
	ctx := context.Background()
	facts, err := streams.coordinator(settings).Observe(ctx, *host, "")
	if err != nil {
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}
	output, err := negotiation.Evaluate(settings, facts, negotiation.Request{
		Session: *session, Native: *native, NativeRaw: *nativeRaw, Mode: *mode, Pin: *pin, Inner: flags.Args(),
	})

	if code := writeJSON(streams, output); code != 0 {
		return code
	}
	if err != nil {
		return 1
	}
	return 0
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
	record, err := streams.coordinator(settings).Probe(ctx, *host, *force)
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
		record, ok, err := streams.store().Read(*host)
		if err != nil {
			fmt.Fprintf(streams.Stderr, "tether status: %v\n", err)
			return 1
		}
		if ok {
			records = []probe.Host{record}
		}
	} else if records, err = streams.store().List(); err != nil {
		fmt.Fprintf(streams.Stderr, "tether status: %v\n", err)
		return 1
	}
	tools := streams.tools()
	now := tools.Now()
	tailnet := probe.LoadTailnet(context.Background(), tools)
	output := statusOutput{Version: StatusVersion, StateDir: streams.store().Directory, Config: config.Path(), Hosts: []statusRecord{}}
	for _, record := range records {
		record := record
		entry := statusRecord{
			Host: record.Host, At: record.At.UTC().Format(time.RFC3339), AgeS: int(now.Sub(record.At) / time.Second),
			Stale: record.Stale(now), RemoteOK: record.Remote.OK, Reason: record.Remote.Reason,
			Backoff: record.InBackoff(now), LinkFresh: record.LinkFresh(now),
			Pin: settings.PinFor(record.Host), Mode: string(settings.ModeFor(record.Host)), Path: streams.store().Path(record.Host),
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
