package command

import (
	"fmt"
	"strings"

	"github.com/roshbhatia/go-utils/completion"
)

// tsh is the porcelain: the argv shape of ssh and mosh, options before the
// host, the remote command after it. Every ssh option is accepted; tsh takes
// -s (session) and -q (quiet) for itself and adds a few long options.
var tshSpecification = completion.Command{
	Name:              "tsh",
	Description:       "ssh, with the hop negotiated",
	Synopsis:          "tsh [ssh options] [-s <session>] [-q] [--pin <tier>] [--mode <mode>] [--dry-run] [--no-probe] [user@]<host> [-- <command>]",
	LongDescription:   "A drop-in for `ssh host` and `mosh host`: resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and exec the winner. ssh options ride the ssh tier verbatim and the mosh tier through --ssh; one that mosh cannot carry (a port forward) filters mosh out with the reason.",
	CompletionCommand: []string{"tether", "hosts", "--names"},
	Flags: []completion.Flag{
		{Name: "session", Short: "s", Description: "Attach this remote mux session (zmx, else tmux) instead of a login shell", Value: true},
		{Name: "quiet", Short: "q", Description: "Do not announce the winning hop on stderr"},
		{Name: "login", Short: "l", Description: "Remote user, as ssh -l", Value: true},
		{Name: "port", Short: "p", Description: "Remote port, as ssh -p", Value: true},
		{Name: "mode", Description: "Ordering mode; overrides the config", Value: true, Values: modeNames},
		{Name: "pin", Description: "Tier that must win; overrides the config", Value: true, Values: tierNames},
		{Name: "dry-run", Description: "Write the tether.plan/v1 document and exit without connecting"},
		{Name: "no-probe", Description: "Never run the ssh round trip; plan from the cache only"},
		{Name: "version", Description: "Write the version to stdout"},
		{Name: "help", Short: "h", Description: "Print command help"},
	},
}

// TSHSpecification is the tsh command tree for completions and docs.
func TSHSpecification() completion.Command { return tshSpecification }

// ssh option letters. Value ones take an argument; the rest are switches.
const (
	sshValueOptions  = "BbcDEeFIiJLlmOoPpQRSWw"
	sshSwitchOptions = "46AaCfGgKkMNnqsTtVvXxYy"
)

// moshRefuses maps an ssh option to why mosh cannot carry it. Everything else
// rides the bootstrap through --ssh.
var moshRefuses = map[byte]string{
	'L': "-L port forwarding needs the ssh session mosh closes after bootstrap",
	'R': "-R port forwarding needs the ssh session mosh closes after bootstrap",
	'D': "-D forwarding needs the ssh session mosh closes after bootstrap",
	'W': "-W forwarding needs the ssh session mosh closes after bootstrap",
	'w': "-w tunnel forwarding needs the ssh session mosh closes after bootstrap",
	'J': "-J jump host: mosh UDP does not traverse a jump host",
	'A': "-A agent forwarding needs the ssh session mosh closes after bootstrap",
	'X': "-X X11 forwarding needs the ssh session mosh closes after bootstrap",
	'Y': "-Y X11 forwarding needs the ssh session mosh closes after bootstrap",
	'N': "-N (no command) has no meaning for mosh",
	'f': "-f (background) has no meaning for mosh",
	'n': "-n (stdin from /dev/null) has no meaning for mosh",
	'T': "-T (no tty) contradicts mosh, which always allocates one",
	'M': "-M control master has no meaning for mosh",
	'O': "-O control command has no meaning for mosh",
	'S': "-S control socket has no meaning for mosh",
	'e': "-e escape char: mosh has its own escape handling",
	'G': "-G prints the ssh configuration and never connects",
	'V': "-V prints the ssh version and never connects",
	'Q': "-Q queries ssh and never connects",
	'g': "-g gateway ports needs a port forward, which mosh cannot carry",
}

// nativeHarmless are ssh options a wezterm domain may ignore without changing
// what the caller asked for.
const nativeHarmless = "tqv"

type sshOption struct {
	letter byte
	value  string
}

// tshOptions is one parsed tsh invocation.
type tshOptions struct {
	host    string
	user    string // from -l or user@host; "" keeps the config user
	options []sshOption
	session string
	mode    string
	pin     string
	quiet   bool
	dryRun  bool
	noProbe bool
	help    bool
	version bool
	command []string
}

// parseTSH reads an ssh-shaped argv: options first, then [user@]host, then
// the remote command (a leading -- is dropped).
func parseTSH(args []string) (tshOptions, error) {
	var options tshOptions
	i := 0
	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg[2:], "=")
			needValue := func() (string, error) {
				if hasValue {
					return value, nil
				}
				if i+1 >= len(args) {
					return "", fmt.Errorf("--%s needs a value", name)
				}
				i++
				return args[i], nil
			}
			var err error
			switch name {
			case "session":
				options.session, err = needValue()
			case "mode":
				options.mode, err = needValue()
			case "pin":
				options.pin, err = needValue()
			case "login":
				options.user, err = needValue()
			case "port":
				var port string
				if port, err = needValue(); err == nil {
					options.options = append(options.options, sshOption{'p', port})
				}
			case "quiet":
				options.quiet = true
			case "dry-run":
				options.dryRun = true
			case "no-probe":
				options.noProbe = true
			case "help":
				options.help = true
			case "version":
				options.version = true
			default:
				return options, fmt.Errorf("unknown option --%s", name)
			}
			if err != nil {
				return options, err
			}
			continue
		}
		// A short cluster: -tq, -p22, -p 22.
		cluster := arg[1:]
		for j := 0; j < len(cluster); j++ {
			letter := cluster[j]
			switch {
			case letter == 'h':
				options.help = true
			case letter == 'q':
				options.quiet = true
			case letter == 's' || strings.IndexByte(sshValueOptions, letter) >= 0:
				value := cluster[j+1:]
				if value == "" {
					if i+1 >= len(args) {
						return options, fmt.Errorf("-%c needs a value", letter)
					}
					i++
					value = args[i]
				}
				switch letter {
				case 's':
					options.session = value
				case 'l':
					options.user = value
				default:
					options.options = append(options.options, sshOption{letter, value})
				}
				j = len(cluster)
			case strings.IndexByte(sshSwitchOptions, letter) >= 0:
				options.options = append(options.options, sshOption{letter: letter})
			default:
				return options, fmt.Errorf("unknown option -%c", letter)
			}
		}
	}
	if options.help || options.version {
		return options, nil
	}
	if i >= len(args) {
		return options, fmt.Errorf("a host is required")
	}
	options.host = args[i]
	if user, host, ok := strings.Cut(options.host, "@"); ok && host != "" {
		options.host = host
		if options.user == "" {
			options.user = user
		}
	}
	rest := args[i+1:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) > 0 {
		options.command = rest
	}
	return options, nil
}

// carried is what the tiers get from the caller's ssh options.
type carried struct {
	ssh         []string // verbatim, before the ssh host
	mosh        []string // mosh options: --ssh=... when the bootstrap needs any
	tty         bool     // the caller passed -t or -T
	moshUnfit   string
	nativeUnfit string
}

// carry maps the ssh options per tier: verbatim for ssh; through --ssh for
// mosh, minus -t, which mosh implies; nothing for a wezterm domain, which has
// its own ssh configuration, so any option that changes the connection makes
// the native tiers unfit.
func carry(options []sshOption, userOverride bool) carried {
	var out carried
	var bootstrap []string
	var refused []string
	var native []string
	for _, option := range options {
		flag := "-" + string(option.letter)
		out.ssh = append(out.ssh, flag)
		if option.value != "" {
			out.ssh = append(out.ssh, option.value)
		}
		if option.letter == 't' || option.letter == 'T' {
			out.tty = true
		}
		if reason, ok := moshRefuses[option.letter]; ok {
			refused = append(refused, reason)
		} else if option.letter != 't' {
			bootstrap = append(bootstrap, flag)
			if option.value != "" {
				bootstrap = append(bootstrap, option.value)
			}
		}
		if strings.IndexByte(nativeHarmless, option.letter) < 0 {
			native = append(native, flag)
		}
	}
	if len(bootstrap) > 0 {
		out.mosh = []string{"--ssh=" + strings.Join(append([]string{"ssh"}, bootstrap...), " ")}
	}
	if len(refused) > 0 {
		out.moshUnfit = strings.Join(refused, "; ")
	}
	if userOverride {
		native = append([]string{"user"}, native...)
	}
	if len(native) > 0 {
		out.nativeUnfit = "caller ssh options (" + strings.Join(native, ", ") + ") do not reach a wezterm domain"
	}
	return out
}

// withUser replaces or adds the user@ part of an ssh target.
func withUser(user, target string) string {
	if user == "" {
		return target
	}
	if _, host, ok := strings.Cut(target, "@"); ok {
		return user + "@" + host
	}
	return user + "@" + target
}
