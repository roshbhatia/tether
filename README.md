# tether

`tsh` is `ssh` with the hop negotiated. Type `tsh arrakis` where you typed
`ssh arrakis` or `mosh arrakis`: it lands in a login shell over mosh when both
ends have it, over plain ssh otherwise. Interactive login in a live WezTerm pane prefers native attachment. Use
`--foreground` for an inline connection. Remote commands and SSH options stay
in the foreground.
One stderr line says which hop won.

```bash
tsh arrakis                          # login shell, like ssh arrakis
tsh arrakis -- htop                  # a command, like ssh arrakis htop
tsh -p 2222 -l ops arrakis           # ssh options ride the winning tier
tsh -s sysinit arrakis               # attach the remote zmx (else tmux) session
tsh -L 8080:localhost:80 arrakis     # a forward cannot ride mosh: ssh wins, and says why
tsh --dry-run arrakis                # the tether.plan/v1 document, no connection
```

`tether` is the plumbing behind it, the way git's plumbing sits behind its
porcelain: `tether connect` is `tsh` under another name, and `tether plan`,
`probe`, `hosts`, and `status` are what a script or the WezTerm session tree
call for the pieces. Given a host, an inner command, and what the caller can
reach natively, `tether plan` picks the hop tool that carries the command and
says what that hop loses. It never composes the inner command and never knows
what `zmx` or `sy` are; it wraps the argv the caller passes after `--`.

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

`tsh` takes ssh's argv: options first, then `[user@]host`, then the remote
command (a leading `--` is optional). Every ssh option is accepted; `tsh`
keeps `-s` (session) and `-q` (quiet) for itself and adds `--pin`, `--mode`,
`--dry-run`, and `--no-probe`.

It resolves the host, refuses an offline tailnet peer (exit 1, with its
last-seen time), runs the probe when the record is missing or older than 10
minutes (`--no-probe` skips it), then ranks. One stderr line names the winner,
`tsh: mosh-mux -> arrakis (loses native-panes, osc, scrollback)`; `-q` drops
it. A pinned tier that is unavailable exits 2 with the reasons and never falls
back.

- A `local` hop (`mosh-mux`, `ssh`) execs the plan command. The process is
  replaced and the exit status is the hop's.
- Bare interactive `tsh` inside WezTerm can attach via the terminal client.
  `tether attach HOST` explicitly opens the native domain from any terminal.
  Both reuse address selection from the picker.
- `tether session list HOST` and `tether session attach HOST NAME` run the
  remote `wezterm-remote` provider in the foreground. The provider owns named
  sessions and their persistence.
- With no command the far side gets its login shell, exactly like `ssh`. With
  `-s S` it gets `wezterm-remote attach S` when available, else `zmx attach S`, else
  `tmux new -A -s S` when it saw `tmux`, else the login shell and a stderr
  note. The mux is read from the host record, never assumed.

### ssh options per tier

| Tier | What the caller's ssh options become |
| --- | --- |
| `ssh` | Verbatim, before the host. A command gets `-t` unless the caller passed `-t` or `-T`. |
| `mosh-mux` | `-l user` becomes `user@host`; the rest ride the bootstrap as `--ssh='ssh -p 2222 -i key ...'`; `-t` is implied. An option mosh cannot carry (`-L -R -D -W -w -J -A -X -Y -N -f -n -T -M -O -S -e -G -V -Q -g`) filters `mosh-mux` out with the reason, so `ssh` wins; under a pin it is an error instead. |
| `native-mux`, `ssh-raw` | A WezTerm domain has its own ssh configuration, so any option other than `-t`, `-q`, `-v`, or a user override, filters both native tiers out with the reason. |

The probe's own round trip uses the ssh config, not the caller's options: a
host only reachable with `-p 2222` needs that port in `~/.ssh/config` for the
inventory to be seen.

## Hosts and the tailnet

`tether hosts` lists what `tsh` can name: the literal `Host` entries of
`~/.ssh/config` (following `Include`) and SSH-enabled peers of the tailnet from
`tailscale status --json`, each marked `ssh-config`, `tailnet`, or `both`.
`--json` writes `tether.hosts/v1`; `--names` feeds the completions.

Tailnet discovery requires advertised `sshHostKeys`. Peers without keys are
excluded from automatic discovery, including peers with an empty key list.
Explicit SSH config entries remain available for ordinary OpenSSH servers.
Offline SSH-enabled peers remain listed with their offline status.

Resolution order is the ssh config alias, then an SSH-enabled tailnet peer by MagicDNS
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

### Preferred addresses and fallback

A configured host is one catalog row. Its name is the preferred SSH route.
`fallback` lists additional SSH aliases in connection order. `aliases` folds
equivalent names into the catalog entry without adding a retry. All remain
direct SSH destinations and appear in `tether hosts --names`.

```json
{
  "hosts": {
    "arrakis": {
      "fallback": ["arrakis.local"],
      "aliases": ["arrakis.stork-eel.ts.net."]
    }
  }
}
```

`tether plan --resolve` checks routes before ranking. The same resolution is
used by `tsh` when a configured name has fallback addresses and there are no
SSH option overrides. The read-only SSH inventory requires a known host key.
It advances after DNS or connection failures or an offline tailnet route.
Authentication failures, host-key failures, and other errors end resolution.
Use `--native-prefix ssh:` with a caller that registers WezTerm domains under
that prefix. It uses the selected configured SSH alias for the domain.

## Probe layers

`tether plan` uses only local observations and cached inventory by default.
`--resolve` explicitly performs read-only SSH inventory checks.

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
  "hosts": {
    "arrakis": { "pin": "mosh-mux" },
    "vault": { "user": "ops" }
  }
}
```

`hosts.<name>.user` is prepended as `user@` to the ssh target; `tsh -l` and
`user@host` override it.

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
  `internal/negotiation` coordinates observations, refresh, and evaluation;
  `internal/command` is the CLI. `rank.Rank` is a pure function and its table
  test is the decision table. Add a row there before changing a rule.
- Each operation observes local tools, SSH configuration, and Tailscale status
  once. Planning evaluates those facts without further I/O. Refresh updates the
  remote inventory and writes through a directory-scoped `probe.Store`.
- The remote inventory runs under `sh -c '...'` because a remote login shell
  can be nushell, which has no `command -v`. The script holds no single quote.
- Registry `direct` is `CurAddr != ""`, which is empty on an idle peer. Only a
  fresh link sample decides "relayed".
- Output contracts are versioned: `tether.plan/v1`, `tether.host/v1`,
  `tether.hosts/v1`, `tether.config/v1`. A host record of another version is a
  cache miss.
- `tsh` (`tether connect`) is the one verb that may do a round trip without
  being asked: the user is about to connect anyway. `plan` does so only with `--resolve`.
- `tsh` is a second `main` over the same command package, not an argv[0]
  switch: a wrapper or `nix run` that rewrites argv[0] cannot turn it back
  into `tether`, and the version ldflag lands in both binaries.
- `internal/hosts` resolves names; `tsh`, `plan`, and `probe` all go through
  it, so `tether probe --host arrakis.stork-eel.ts.net` and `tsh arrakis`
  share one host record.
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

Demo recording tools are available separately with `nix develop .#demos`.
Run `python3 hack/extra-demos.py` from that shell to record the extras.

## tsh
<!-- BEGIN GENERATED:tsh -->

### `tsh`

tsh [ssh options] [-s <session>] [-q] [--pin <tier>] [--mode <mode>] [--dry-run] [--no-probe] [--foreground] [user@]<host> [-- <command>]

A drop-in for `ssh host` and `mosh host`: resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and exec the winner. ssh options ride the ssh tier verbatim and the mosh tier through --ssh; one that mosh cannot carry (a port forward) filters mosh out with the reason.

| Option | Description |
| --- | --- |
| `--session`, `-s` `<value>` | Attach this remote session (wezterm-remote, zmx, or tmux) instead of a login shell |
| `--quiet`, `-q` | Do not announce the winning hop on stderr |
| `--login`, `-l` `<value>` | Remote user, as ssh -l |
| `--port`, `-p` `<value>` | Remote port, as ssh -p |
| `--mode` `<value>` | Ordering mode; overrides the config |
| `--pin` `<value>` | Tier that must win; overrides the config |
| `--dry-run` | Write the tether.plan/v1 document and exit without connecting |
| `--foreground` | Keep the connection in this terminal |
| `--no-probe` | Never run the ssh round trip; plan from the cache only |
| `--version` | Write the version to stdout |
| `--help`, `-h` | Print command help |

<!-- END GENERATED:tsh -->

## Commands
<!-- BEGIN GENERATED:commands -->

### `tether`

tether [--version] <connect|attach|session|plan|probe|hosts|status|completion>

The plumbing behind tsh. Resolve which transport carries a command to a host, from what both ends have installed. connect execs the hop; plan, probe, hosts --json, and status write JSON.

| Option | Description |
| --- | --- |
| `--version` | Write the version to stdout |
| `--help`, `-h` | Print command help |

### `tether connect`

tether connect [ssh options] [-s <session>] [-q] [--pin <tier>] [--mode <mode>] [--dry-run] [--no-probe] [user@]<host> [-- <command>]

Same argv as tsh: resolve the host (ssh config alias, then tailnet peer), probe when the record is stale, rank the tiers, and replace this process with the winning local hop. Interactive login from a live WezTerm pane can attach its native domain.

| Option | Description |
| --- | --- |
| `--session`, `-s` `<value>` | Attach this remote session (wezterm-remote, zmx, or tmux) instead of a login shell |
| `--quiet`, `-q` | Do not announce the winning hop on stderr |
| `--login`, `-l` `<value>` | Remote user, as ssh -l |
| `--port`, `-p` `<value>` | Remote port, as ssh -p |
| `--mode` `<value>` | Ordering mode; overrides the config |
| `--pin` `<value>` | Tier that must win; overrides the config |
| `--dry-run` | Write the tether.plan/v1 document and exit without connecting |
| `--foreground` | Keep the connection in this terminal |
| `--no-probe` | Never run the ssh round trip; plan from the cache only |
| `--version` | Write the version to stdout |
| `--help`, `-h` | Print command help |

### `tether attach`

tether attach [--dry-run] [--no-probe] <host>

| Option | Description |
| --- | --- |
| `--dry-run` | Print the cached attachment plan |
| `--no-probe` | Use cached capability data |
| `--help`, `-h` | Print command help |

### `tether session`

tether session list <host> | tether session attach <host> <name>

### `tether plan`

tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--resolve] [--native-prefix <prefix>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]

Read the local tools, the tailscale registry, and the cached remote inventory, then rank the tiers. --resolve probes the preferred and fallback addresses before ranking. The inner argv after -- is wrapped, never composed.

| Option | Description |
| --- | --- |
| `--host` `<value>` | ssh config alias or tailnet peer |
| `--session` `<value>` | Session name echoed in the output |
| `--native` `<value>` | Opaque ref of the caller's native mux domain |
| `--native-raw` `<value>` | Opaque ref of the caller's raw ssh domain |
| `--resolve` | Check configured addresses in preference order |
| `--native-prefix` `<value>` | Build the native domain ref from the selected SSH alias |
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

Group explicitly configured aliases from the SSH config and tailnet. --names also returns individual addresses for direct SSH and completion.

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
