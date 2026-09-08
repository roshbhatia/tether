// Package probe measures what both ends of a hop have installed, in three
// layers by cost: local (ms), node registry (ms), remote (one ssh round trip).
package probe

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// HostVersion tags the cached host record.
const HostVersion = "tether.host/v1"

const (
	// HostTTL is how long a remote inventory counts as fresh.
	HostTTL = 10 * time.Minute
	// NegativeTTL is how long an unreachable host is not probed again.
	NegativeTTL = 5 * time.Minute
	// LinkTTL is how long a link sample counts as fresh.
	LinkTTL = 60 * time.Second
	// ConnectTimeout bounds the ssh round trip when no registry vouches for the host.
	ConnectTimeout = 5 * time.Second
)

// Local is what the display side has installed. A path is "" when absent.
type Local struct {
	Mosh     string `json:"mosh" jsonschema:"description=Path to mosh; empty when absent"`
	Wezterm  string `json:"wezterm" jsonschema:"description=Path to wezterm; empty when absent"`
	SSH      string `json:"ssh" jsonschema:"description=Path to ssh; empty when absent"`
	Hostname string `json:"hostname" jsonschema:"description=HostName from ssh -G; else the alias"`
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
}

// Remote is the far-side inventory from one ssh round trip.
type Remote struct {
	OK               bool              `json:"ok"`
	Reason           string            `json:"reason,omitempty" jsonschema:"description=Why the round trip failed or was skipped"`
	MoshServer       string            `json:"mosh_server"`
	Zmx              string            `json:"zmx"`
	Tmux             string            `json:"tmux"`
	WeztermMuxServer string            `json:"wezterm_mux_server"`
	Versions         map[string]string `json:"versions"`
	At               time.Time         `json:"at" jsonschema:"description=When this remote layer last ran (success or failure)"`
}

// Registry is what the local tailscale daemon says about the host.
type Registry struct {
	Present bool   `json:"present" jsonschema:"description=tailscale status answered"`
	Peer    bool   `json:"peer" jsonschema:"description=The host matched a tailnet peer"`
	Online  bool   `json:"online"`
	Relay   string `json:"relay,omitempty"`
	Direct  bool   `json:"direct" jsonschema:"description=A direct path was active at status time"`
}

// Link is one round-trip sample.
type Link struct {
	RTTMs  float64   `json:"rtt_ms"`
	Loss   float64   `json:"loss" jsonschema:"description=Lost fraction (0-1)"`
	Direct *bool     `json:"direct" jsonschema:"nullable,description=Not relayed; null when the tool cannot tell"`
	Tool   string    `json:"tool" jsonschema:"enum=tailscale,enum=ping"`
	At     time.Time `json:"at"`
}

// Host is the cached record, one file per host.
type Host struct {
	Version  string    `json:"version"`
	Host     string    `json:"host"`
	At       time.Time `json:"at"`
	Local    Local     `json:"local"`
	Remote   Remote    `json:"remote"`
	Registry Registry  `json:"registry"`
	Link     *Link     `json:"link" jsonschema:"nullable,description=Latest link sample; null when the host was not sampled"`
}

// Stale reports whether the record is older than HostTTL.
func (host Host) Stale(now time.Time) bool {
	return now.Sub(host.At) > HostTTL
}

// LinkFresh reports whether the link sample is inside LinkTTL.
func (host Host) LinkFresh(now time.Time) bool {
	return host.Link != nil && now.Sub(host.Link.At) <= LinkTTL
}

// InBackoff reports whether a failed remote probe is inside NegativeTTL.
func (host Host) InBackoff(now time.Time) bool {
	return !host.Remote.OK && !host.Remote.At.IsZero() && now.Sub(host.Remote.At) < NegativeTTL
}

// Tools is the process boundary, injected so tests never fork.
type Tools struct {
	LookPath func(name string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
	Now      func() time.Time
}

// DefaultTools forks real processes.
func DefaultTools() Tools {
	return Tools{
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
			command := exec.CommandContext(ctx, name, args...)
			var errOut strings.Builder
			command.Stderr = &errOut
			out, err := command.Output()
			return out, []byte(errOut.String()), err
		},
		Now: time.Now,
	}
}

func (tools Tools) lookPath(name string) string {
	if tools.LookPath == nil {
		return ""
	}
	path, err := tools.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

// Options tune one probe run.
type Options struct {
	// Force runs the remote layer even inside a negative-cache backoff.
	Force bool
}

// Run measures every layer and writes the host record.
func Run(ctx context.Context, tools Tools, host string, options Options) (Host, error) {
	now := tools.Now()
	previous, hadPrevious, _ := Read(host)

	record := Host{Version: HostVersion, Host: host, At: now}
	record.Local = LocalLayer(ctx, tools, host)
	record.Registry = RegistryLayer(ctx, tools, host, record.Local.Hostname)

	switch {
	case hadPrevious && previous.InBackoff(now) && !options.Force:
		record.Remote = previous.Remote
	default:
		if skip, reason := GuardsRemote(record.Local.Hostname, record.Registry); skip {
			record.Remote = Remote{Reason: reason, Versions: map[string]string{}, At: now}
		} else {
			record.Remote = RemoteLayer(ctx, tools, host, now)
		}
	}

	if !record.Registry.Peer || record.Registry.Online {
		record.Link = LinkLayer(ctx, tools, record.Local.Hostname, record.Registry, now)
	}

	if err := Write(record); err != nil {
		return record, err
	}
	return record, nil
}
