# tether

Transport negotiator. Given a host, an inner command, and what the caller can
reach natively, `tether` picks the hop tool that carries the command and says
what that hop loses. It never composes the inner command and never knows what
`zmx` or `sy` are; it wraps the argv the caller passes after `--`.

`tether connect <host>` is the verb that runs the hop: it probes when the host
record is stale, ranks the tiers, and replaces itself with `mosh` or `ssh`.

## Tiers

| Rank | Tier | Runs | Gives | Loses |
| --- | --- | --- | --- | --- |
| 1 | `native-mux` | inner argv at the caller's WezTerm ssh domain (`multiplexing = "WezTerm"`) | native-panes, osc, byte-clean, scrollback | roaming, local-echo |
| 2 | `mosh-mux` | `mosh <host> -- <inner>` in a local pane | roaming, local-echo, persistence | native-panes, osc, scrollback |
| 3 | `ssh-raw` | inner argv at a caller-declared raw ssh domain (`multiplexing = "None"`) | native-panes, osc, byte-clean | roaming, local-echo, persistence |
| 4 | `ssh` | `ssh -t <host> -- <inner>` in a local pane | osc, byte-clean | native-panes, roaming, local-echo, persistence |

Ranking, in override order: a pin wins and never falls back silently; hard
requirements filter tiers; the mode orders survivors (`auto`, `native`,
`roam`, `persist`); under `auto` a flaky link prefers `mosh-mux`; ties break
on tier number.

## Connecting

```bash
tether connect arrakis                       # login shell over the best hop
tether connect arrakis --session sysinit     # zmx attach sysinit (tmux new -A when zmx is absent)
tether connect arrakis -- htop               # any inner argv after --
tether connect arrakis --dry-run             # the tether.plan/v1 document, no connection
tether connect arrakis --pin ssh             # exit 2 with the reasons when the pin is unavailable
```

`connect` resolves the host, refuses an offline tailnet peer (exit 1, with its
last-seen time), runs `tether probe` when the record is missing or older than
10 minutes (`--no-probe` skips it), then ranks. One stderr line names the
winner, `tether: mosh-mux -> arrakis (loses native-panes, osc, scrollback)`;
`--quiet` drops it.

- A `local` hop (`mosh-mux`, `ssh`) execs the plan command. The process is
  replaced and the exit status is the hop's.
- A `native` hop (`native-mux`) is a WezTerm tab in the `ssh:<host>` mux
  domain, opened with `wezterm cli spawn --domain-name`. `connect` passes
  `--native ssh:<host>` only when it runs inside WezTerm (`WEZTERM_PANE` is
  set), `wezterm cli list` answers, and the host is an ssh config alias, since
  the GUI builds those domains from the ssh config. Native is a display
  concept: from a plain terminal, `connect` is always a local hop. The remote
  `wezterm-mux-server` spawns the inner argv itself, with its own PATH and in
  the remote home the probe recorded (`wezterm cli spawn` would otherwise hand
  it this pane's cwd, which does not exist over there).
- With no inner argv the far side gets its login shell. With `--session S`, or
  `defaults.session` in the config, it gets `zmx attach S` when the probe saw
  `zmx` on the remote, else `tmux new -A -s S` when it saw `tmux`, else the
  login shell and a stderr note. The mux is read from the host record, never
  assumed.

## Hosts and the tailnet

`tether hosts` lists what `connect` can name: the literal `Host` entries of
`~/.ssh/config` (following `Include`) and the peers of the tailnet from
`tailscale status --json`, each marked `ssh-config`, `tailnet`, or `both`.
`--json` writes `tether.hosts/v1`; `--names` feeds the completions.

Resolution order is the ssh config alias, then a tailnet peer by MagicDNS
label, hostname, DNS name, or tailnet IP. A tailnet-only host is keyed by its
MagicDNS label and reached by its DNS name, `user@name.ts.net` when the config
sets `hosts.<name>.user`. The `transport:` reason records which it was.

Tailscale SSH is detected from the peer's `sshHostKeys` in the status JSON:
the coordination server publishes a node's host keys only when it runs the
Tailscale SSH server. `hosts` and `status` show it. tether still reaches such a
peer with plain `ssh` and `mosh`: the `tailscale ssh` wrapper takes its first
argument as the host, so it cannot carry `ssh -t`, and `mosh --ssh` puts its
own options before the host, so the wrapper cannot bootstrap `mosh-server`
either. Plain `ssh` to a MagicDNS name reaches a Tailscale SSH server without
the wrapper.

## Probe layers

`tether plan` runs the first two layers only and never touches the network.

- Local (ms): `command -v mosh wezterm ssh`; `ssh -G <host>` resolves the alias.
- Registry (ms): `tailscale status --json` from the local daemon. A `*.ts.net`
  host whose peer is `Online:false` gets no ssh probe at all.
- Remote (one ssh round trip, `tether probe` only): `command -v` for
  `mosh-server zmx tmux wezterm-mux-server` under `sh -c`, cached in
  `$XDG_STATE_HOME/tether/hosts/<host>.json` for 10 minutes. An unreachable
  host is not probed again for 5 minutes. A link sample (`tailscale ping`,
  else `ping`) is fresh for 60 seconds.

## Configuration

`~/.config/tether/config.json`, or `$TETHER_CONFIG`. Schema in
`schema/config.schema.json`.

```json
{
  "mode": "auto",
  "flaky": { "rtt_ms": 60, "loss": 0 },
  "defaults": { "session": "main" },
  "hosts": {
    "arrakis": { "pin": "mosh-mux" },
    "vault": { "user": "ops" }
  }
}
```

`defaults.session` is what `connect` attaches when `--session` is absent;
`hosts.<name>.user` is prepended as `user@` to the ssh target.

## Example

```bash
tether probe --host arrakis
tether plan --host arrakis --session sysinit --native ssh:arrakis -- zmx attach sysinit
```

The plan is `tether.plan/v1` (`schema/tether.plan.v1.schema.json`). Its `plan`
member is a CommandPlan (`command, cwd, environment, successCodes`) and `hop`
says where it runs: `local` is the display side; `native` is the ref the
caller passed, echoed and never parsed. `target` is present when ssh is given
something other than `host`, such as `ops@vault.stork-eel.ts.net`.

## Design notes

- `internal/probe` measures; `internal/rank` decides; `internal/plan` renders;
  `internal/command` is the CLI. `rank.Rank` is a pure function and its table
  test is the decision table. Add a row there before changing a rule.
- The remote inventory runs under `sh -c '...'` because a remote login shell
  can be nushell, which has no `command -v`. The script holds no single quote.
- Registry `direct` is `CurAddr != ""`, which is empty on an idle peer. Only a
  fresh link sample decides "relayed".
- Output contracts are versioned: `tether.plan/v1`, `tether.host/v1`,
  `tether.hosts/v1`, `tether.config/v1`. A host record of another version is a
  cache miss.
- `connect` is the one verb that may do a round trip without being asked: the
  user is about to connect anyway. `plan` never does.
- `internal/hosts` resolves names; `connect`, `plan`, and `probe` all go
  through it, so `tether probe --host arrakis.stork-eel.ts.net` and
  `tether connect arrakis` share one host record.
- `schema/*.json`, `completions/*`, and the command section below are
  generated by `./hack/generate.sh` from the Go structs and the completion
  specification. CI runs `--check`; a stale artifact fails the build.
- The release tag must equal `v` + `version` in `flake.nix`.

## Development

```bash
nix develop
go test -race ./...
./hack/generate.sh --check
nix build
nix flake check
```

## Commands
<!-- BEGIN GENERATED:commands -->

### `tether`

tether [--version] <connect|plan|probe|hosts|status|completion>

Resolve which transport carries an inner command to a host, from what both ends have installed, and run it. connect execs the hop; plan, probe, hosts --json, and status write JSON.

| Option | Description |
| --- | --- |
| `--version` | Write the version to stdout |
| `--help`, `-h` | Print command help |

### `tether connect`

tether connect <host> [--session <name>] [--mode <mode>] [--pin <tier>] [--dry-run] [--quiet] [--no-probe] [-- <inner argv>]

Resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and replace this process with the winning local hop. Inside WezTerm an ssh-config host may win native-mux, which opens a tab in the ssh:<host> domain instead.

| Option | Description |
| --- | --- |
| `--session` `<value>` | Session to attach with zmx or tmux when no inner argv is given; overrides defaults.session |
| `--mode` `<value>` | Ordering mode; overrides the config |
| `--pin` `<value>` | Tier that must win; overrides the config |
| `--dry-run` | Write the plan JSON and exit without connecting |
| `--quiet` | Do not announce the winning hop on stderr |
| `--no-probe` | Never run the ssh round trip; plan from the cache only |
| `--help`, `-h` | Print command help |

### `tether plan`

tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]

Read the local tools, the tailscale registry, and the cached remote inventory, then rank the tiers. The inner argv after -- is wrapped, never composed.

| Option | Description |
| --- | --- |
| `--host` `<value>` | ssh config alias or tailnet peer |
| `--session` `<value>` | Session name echoed in the output |
| `--native` `<value>` | Opaque ref of the caller's native mux domain |
| `--native-raw` `<value>` | Opaque ref of the caller's raw ssh domain |
| `--mode` `<value>` | Ordering mode; overrides the config |
| `--pin` `<value>` | Tier that must win; overrides the config |
| `--help`, `-h` | Print command help |

### `tether probe`

tether probe --host <host> [--force]

Run the local, registry, remote, and link layers. One ssh round trip; a tailnet host the registry says is offline gets none.

| Option | Description |
| --- | --- |
| `--host` `<value>` | ssh config alias or tailnet peer |
| `--force` | Probe even inside the unreachable-host backoff |
| `--help`, `-h` | Print command help |

### `tether hosts`

tether hosts [--json] [--names]

The union of the literal Host entries in ~/.ssh/config and the peers of the tailnet, with each host's source, online state, OS, and whether it advertises Tailscale SSH.

| Option | Description |
| --- | --- |
| `--json` | Write tether.hosts/v1 instead of a table |
| `--names` | Write one name per line, for completion |
| `--help`, `-h` | Print command help |

### `tether status`

tether status [--host <host>]

List every host record with its age, staleness, and live tailnet state, or one host.

| Option | Description |
| --- | --- |
| `--host` `<value>` | Only this host |
| `--help`, `-h` | Print command help |

### `tether completion`

tether completion <bash|fish|nu|zsh>

Write a shell definition to stdout.

<!-- END GENERATED:commands -->
