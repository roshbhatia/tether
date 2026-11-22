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
	Synopsis:        "tether [--version] <connect|attach|session|plan|probe|hosts|status|completion>",
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
			LongDescription:   "Same argv as tsh: resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and replace this process with the winning local hop. Interactive login from a live WezTerm pane can attach its native domain.",
			CompletionCommand: []string{"tether", "hosts", "--names"},
			Flags:             tshSpecification.Flags,
		},
		{
			Name:        "attach",
			Description: "Attach a native WezTerm mux domain",
			Synopsis:    "tether attach [--dry-run] [--no-probe] <host>",
			Flags: []completion.Flag{
				{Name: "dry-run", Description: "Print the cached attachment plan"},
				{Name: "no-probe", Description: "Use cached capability data"},
				{Name: "help", Short: "h", Description: "Print command help"},
			},
		},
		{
			Name:        "session",
			Description: "List or attach remote WezTerm named sessions",
			Synopsis:    "tether session list <host> | tether session attach <host> <name>",
		},
		{
			Name:            "plan",
			Description:     "Choose a tier from the cache or resolve an address explicitly",
			Synopsis:        "tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--resolve] [--native-prefix <prefix>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]",
			LongDescription: "Read the local tools, the tailscale registry, and the cached remote inventory, then rank the tiers. --resolve probes the preferred and fallback addresses before ranking. The inner argv after -- is wrapped, never composed.",
			Flags: []completion.Flag{
				{Name: "host", Description: "ssh config alias or tailnet peer", Value: true, CompletionCommand: []string{"tether", "hosts", "--names"}},
				{Name: "session", Description: "Session name echoed in the output", Value: true},
				{Name: "native", Description: "Opaque ref of the caller's native mux domain", Value: true},
				{Name: "native-raw", Description: "Opaque ref of the caller's raw ssh domain", Value: true},
				{Name: "resolve", Description: "Check configured addresses in preference order"},
				{Name: "native-prefix", Description: "Build the native domain ref from the selected SSH alias", Value: true},
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
			Description:     "List logical hosts and their SSH addresses",
			Synopsis:        "tether hosts [--json] [--names]",
			LongDescription: "Group explicitly configured aliases from the SSH config and tailnet. --names also returns individual addresses for direct SSH and completion.",
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
	// Interactive overrides terminal detection in tests.
	Interactive func() bool
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
		case "attach":
			return connect("tether attach", args[1:], streams, subcommand("attach"), true)
		case "session":
			return runSession(args[1:], streams)
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
	resolve := flags.Bool("resolve", false, "")
	prefix := flags.String("native-prefix", "", "")
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
	coordinator := streams.coordinator(settings)
	var facts negotiation.Facts
	if *resolve {
		ctx, cancel := context.WithTimeout(ctx, 13*time.Second)
		defer cancel()
		facts, err = coordinator.Resolve(ctx, settings.AddressesFor(*host), "")
	} else {
		facts, err = coordinator.Observe(ctx, *host, "")
	}
	if err != nil {
		if *resolve {
			output := plan.Failure(plan.Subject{Host: *host, Session: *session}, err, rank.Result{}, plan.Probe{Stale: true})
			if code := writeJSON(streams, output); code != 0 {
				return code
			}
		}
		fmt.Fprintf(streams.Stderr, "tether plan: %v\n", err)
		return 1
	}
	if *native == "" && *prefix != "" && (facts.Resolution.Host.Source == hosts.SourceSSHConfig || facts.Resolution.Host.Source == hosts.SourceBoth) {
		*native = negotiation.NativeRef(facts, *prefix)
	}
	output, err := negotiation.Evaluate(settings, facts, negotiation.Request{
		Host: *host, Session: *session, Native: *native, NativeRaw: *nativeRaw, Mode: *mode, Pin: *pin, Inner: flags.Args(),
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
		output.Hosts = groupHosts(output.Hosts, settings)
		return writeJSON(streams, output)
	case *names:
		for _, host := range output.Hosts {
			fmt.Fprintln(streams.Stdout, host.Name)
		}
		return 0
	}
	rows := [][]string{{"NAME", "SOURCE", "HOSTNAME", "ONLINE", "OS", "TAILSCALE-SSH"}}
	for _, host := range groupHosts(output.Hosts, settings) {
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

func groupHosts(rows []hosts.Host, settings config.Config) []hosts.Host {
	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.Name] = true
	}
	members := map[string]bool{}
	for name, entry := range settings.Hosts {
		if !known[name] {
			continue
		}
		for _, alias := range settings.AddressesFor(name)[1:] {
			members[strings.ToLower(alias)] = true
		}
		for _, alias := range entry.Aliases {
			members[strings.ToLower(alias)] = true
		}
	}
	grouped := make([]hosts.Host, 0, len(rows))
	for _, row := range rows {
		if members[strings.ToLower(row.Name)] {
			continue
		}
		if entry, ok := settings.Hosts[row.Name]; ok {
			row.Addresses = settings.AddressesFor(row.Name)
			row.Aliases = entry.Aliases
		}
		grouped = append(grouped, row)
	}
	return grouped
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
