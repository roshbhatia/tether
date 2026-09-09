package probe

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type tailscaleStatus struct {
	Self           *tailscalePeer           `json:"Self"`
	MagicDNSSuffix string                   `json:"MagicDNSSuffix"`
	Peer           map[string]tailscalePeer `json:"Peer"`
}

// tailscalePeer is the subset of ipnstate.PeerStatus tether reads. SSHHostKeys
// is `sshHostKeys`: the node's SSH host keys as advertised through the
// coordination server, present only when the peer runs Tailscale SSH.
type tailscalePeer struct {
	HostName     string    `json:"HostName"`
	DNSName      string    `json:"DNSName"`
	OS           string    `json:"OS"`
	Tags         []string  `json:"Tags"`
	TailscaleIPs []string  `json:"TailscaleIPs"`
	Online       bool      `json:"Online"`
	Relay        string    `json:"Relay"`
	CurAddr      string    `json:"CurAddr"`
	LastSeen     time.Time `json:"LastSeen"`
	SSHHostKeys  []string  `json:"sshHostKeys"`
}

// Peer is one tailnet node as the local daemon reports it.
type Peer struct {
	Name         string     `json:"name" jsonschema:"description=MagicDNS label; unique on the tailnet"`
	HostName     string     `json:"hostname" jsonschema:"description=The node's own hostname; not unique"`
	DNSName      string     `json:"dns_name" jsonschema:"description=Fully qualified MagicDNS name without the trailing dot"`
	IPs          []string   `json:"ips"`
	OS           string     `json:"os"`
	Tags         []string   `json:"tags"`
	Online       bool       `json:"online"`
	Relay        string     `json:"relay,omitempty"`
	Direct       bool       `json:"direct" jsonschema:"description=A direct path was active at status time"`
	TailscaleSSH bool       `json:"tailscale_ssh" jsonschema:"description=The peer advertises SSH host keys, so it runs Tailscale SSH"`
	LastSeen     *time.Time `json:"last_seen" jsonschema:"nullable,description=Set for an offline peer"`
}

// Tailnet is what the local tailscale daemon knows. Present is false when
// tailscale is absent or the daemon did not answer.
type Tailnet struct {
	Present bool
	Suffix  string
	Self    *Peer
	Peers   []Peer
}

// LoadTailnet reads `tailscale status --json` once. It never opens a
// connection to any peer.
func LoadTailnet(ctx context.Context, tools Tools) Tailnet {
	tailscale := tools.lookPath("tailscale")
	if tailscale == "" || tools.Run == nil {
		return Tailnet{}
	}
	out, _, err := tools.Run(ctx, tailscale, "status", "--json")
	if err != nil {
		return Tailnet{}
	}
	return parseTailnet(out)
}

func parseTailnet(out []byte) Tailnet {
	var status tailscaleStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return Tailnet{}
	}
	tailnet := Tailnet{Present: true, Suffix: status.MagicDNSSuffix}
	if status.Self != nil {
		self := peerFrom(*status.Self)
		tailnet.Self = &self
	}
	for _, raw := range status.Peer {
		tailnet.Peers = append(tailnet.Peers, peerFrom(raw))
	}
	sort.Slice(tailnet.Peers, func(i, j int) bool { return tailnet.Peers[i].Name < tailnet.Peers[j].Name })
	return tailnet
}

func peerFrom(raw tailscalePeer) Peer {
	peer := Peer{
		Name:         strings.ToLower(firstLabel(raw.DNSName)),
		HostName:     raw.HostName,
		DNSName:      trimDot(raw.DNSName),
		IPs:          append([]string{}, raw.TailscaleIPs...),
		OS:           raw.OS,
		Tags:         append([]string{}, raw.Tags...),
		Online:       raw.Online,
		Relay:        raw.Relay,
		Direct:       raw.CurAddr != "",
		TailscaleSSH: len(raw.SSHHostKeys) > 0,
	}
	if peer.Name == "" {
		peer.Name = strings.ToLower(raw.HostName)
	}
	if !raw.Online && !raw.LastSeen.IsZero() {
		seen := raw.LastSeen
		peer.LastSeen = &seen
	}
	return peer
}

// Find matches a peer by any of the names: MagicDNS label, hostname, DNS
// name, or tailnet IP. Case does not matter.
func (tailnet Tailnet) Find(names ...string) (Peer, bool) {
	want := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		want[strings.ToLower(trimDot(name))] = struct{}{}
		want[strings.ToLower(firstLabel(name))] = struct{}{}
	}
	for _, peer := range tailnet.Peers {
		candidates := append([]string{peer.Name, peer.HostName, peer.DNSName}, peer.IPs...)
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if _, ok := want[strings.ToLower(candidate)]; ok {
				return peer, true
			}
		}
	}
	return Peer{}, false
}

// Registry projects one peer onto the host record.
func (tailnet Tailnet) Registry(host, hostname string) Registry {
	if !tailnet.Present {
		return Registry{}
	}
	registry := Registry{Present: true}
	peer, ok := tailnet.Find(host, hostname)
	if !ok {
		return registry
	}
	registry.Peer = true
	registry.Online = peer.Online
	registry.Relay = peer.Relay
	registry.Direct = peer.Direct
	registry.DNSName = peer.DNSName
	registry.SSH = peer.TailscaleSSH
	return registry
}

// RegistryLayer reads the local tailscale daemon. It never opens a connection
// to the host. With no tailscale, Present is false and the caller falls back to
// a connect timeout.
func RegistryLayer(ctx context.Context, tools Tools, host, hostname string) Registry {
	return LoadTailnet(ctx, tools).Registry(host, hostname)
}

func parseTailscaleStatus(out []byte, host, hostname string) Registry {
	return parseTailnet(out).Registry(host, hostname)
}

func trimDot(name string) string { return strings.TrimSuffix(name, ".") }

func firstLabel(name string) string {
	label, _, _ := strings.Cut(trimDot(name), ".")
	return label
}

// IsTailnetName reports whether the ssh hostname is a MagicDNS name.
func IsTailnetName(hostname string) bool {
	return strings.HasSuffix(strings.ToLower(trimDot(hostname)), ".ts.net")
}

// GuardsRemote is the connect-stall guard: a tailnet host the registry says is
// offline gets no ssh probe at all.
func GuardsRemote(hostname string, registry Registry) (skip bool, reason string) {
	if IsTailnetName(hostname) && registry.Present && registry.Peer && !registry.Online {
		return true, "tailscale reports " + trimDot(hostname) + " offline"
	}
	return false, ""
}
