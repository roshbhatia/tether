package negotiation

import (
	"strings"
	"time"

	"github.com/roshbhatia/tether/internal/config"
	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
	"github.com/roshbhatia/tether/internal/rank"
)

// Request is one plan: the caller's refs and overrides plus the inner argv.
type Request struct {
	Session     string
	Native      string
	NativeRaw   string
	Mode        string
	Pin         string
	Inner       []string
	Flags       plan.Flags
	MoshUnfit   string
	NativeUnfit string
}

// Evaluate renders a plan from observed facts without I/O.
func Evaluate(settings config.Config, observed Facts, req Request) (plan.Output, error) {
	target := observed.Resolution.Host
	prefs := rank.Prefs{
		Mode:       settings.ModeFor(target.Name),
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

	now := observed.Now
	local, registry := observed.Resolution.Local, observed.Resolution.Registry
	record, cached := observed.Record, observed.Cached

	caps := rank.Caps{
		LocalMosh:       local.Mosh != "",
		LocalWezterm:    local.Wezterm != "",
		LocalSSH:        local.SSH != "",
		RegistryOffline: registry.Present && registry.Peer && !registry.Online,
		NativeRef:       req.Native,
		NativeRawRef:    req.NativeRaw,
		MoshUnfit:       req.MoshUnfit,
		NativeUnfit:     req.NativeUnfit,
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
	subject := plan.Subject{Host: target.Name, Session: req.Session, Flags: req.Flags}
	if target.Target != target.Name {
		subject.Target = target.Target
	}
	if err != nil {
		return plan.Failure(subject, err, result, probeInfo), err
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
		return "transport: ssh to " + target.Target + " (not in ssh config or advertised as a Tailscale SSH host)"
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
