package probe

import (
	"context"
	"encoding/json"
	"strings"
)

type tailscaleStatus struct {
	Peer map[string]tailscalePeer `json:"Peer"`
}

type tailscalePeer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	Relay        string   `json:"Relay"`
	CurAddr      string   `json:"CurAddr"`
}

// RegistryLayer reads the local tailscale daemon. It never opens a connection
// to the host. With no tailscale, Present is false and the caller falls back to
// a connect timeout.
func RegistryLayer(ctx context.Context, tools Tools, host, hostname string) Registry {
	tailscale := tools.lookPath("tailscale")
	if tailscale == "" || tools.Run == nil {
		return Registry{}
	}
	out, _, err := tools.Run(ctx, tailscale, "status", "--json")
	if err != nil {
		return Registry{}
	}
	return parseTailscaleStatus(out, host, hostname)
}

func parseTailscaleStatus(out []byte, host, hostname string) Registry {
	var status tailscaleStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return Registry{}
	}
	registry := Registry{Present: true}
	want := map[string]struct{}{
		strings.ToLower(host):                 {},
		strings.ToLower(hostname):             {},
		strings.ToLower(firstLabel(hostname)): {},
		strings.ToLower(trimDot(hostname)):    {},
		strings.ToLower(firstLabel(host)):     {},
	}
	for _, peer := range status.Peer {
		candidates := []string{peer.HostName, trimDot(peer.DNSName), firstLabel(peer.DNSName)}
		candidates = append(candidates, peer.TailscaleIPs...)
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if _, ok := want[strings.ToLower(candidate)]; ok {
				registry.Peer = true
				registry.Online = peer.Online
				registry.Relay = peer.Relay
				registry.Direct = peer.CurAddr != ""
				return registry
			}
		}
	}
	return registry
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
