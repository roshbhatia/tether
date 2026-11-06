// Package hosts names the machines tether can reach: the literal Host entries
// of the ssh config and the peers of the tailnet, and resolves one name to the
// argument ssh and mosh get.
package hosts

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/roshbhatia/tether/internal/probe"
)

// Version tags the hosts output.
const Version = "tether.hosts/v1"

// Source says where a host is known.
const (
	SourceSSHConfig = "ssh-config"
	SourceTailnet   = "tailnet"
	SourceBoth      = "both"
)

// Host is one candidate. Name is what connect and the host record use;
// Target is what ssh and mosh are given.
type Host struct {
	Name     string      `json:"name"`
	Source   string      `json:"source" jsonschema:"enum=ssh-config,enum=tailnet,enum=both"`
	Hostname string      `json:"hostname" jsonschema:"description=HostName from ssh -G, else the MagicDNS name"`
	User     string      `json:"user,omitempty" jsonschema:"description=User from ssh -G or the config"`
	Target   string      `json:"target" jsonschema:"description=The ssh argument: the alias, or user@dns-name for a tailnet-only host"`
	Peer     *probe.Peer `json:"peer" jsonschema:"nullable,description=The tailnet peer; null when the host is only in the ssh config"`
}

// Online reports the tailnet view: nil when the host is not a peer.
func (host Host) Online() *bool {
	if host.Peer == nil {
		return nil
	}
	online := host.Peer.Online
	return &online
}

// Output is the whole tether.hosts/v1 document.
type Output struct {
	Version   string `json:"version"`
	SSHConfig string `json:"ssh_config"`
	Tailnet   bool   `json:"tailnet" jsonschema:"description=tailscale status answered"`
	Hosts     []Host `json:"hosts"`
}

// Resolver reads both sources. SSHConfig defaults to ~/.ssh/config; UserFor
// supplies the configured remote user, or "".
type Resolver struct {
	Tools     probe.Tools
	SSHConfig string
	UserFor   func(host string) string
}

func (resolver Resolver) sshConfig() string {
	if resolver.SSHConfig != "" {
		return resolver.SSHConfig
	}
	return DefaultSSHConfig()
}

// DefaultSSHConfig is ~/.ssh/config.
func DefaultSSHConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

func (resolver Resolver) userFor(host string) string {
	if resolver.UserFor == nil {
		return ""
	}
	return resolver.UserFor(host)
}

type Resolution struct {
	Host     Host
	Known    bool
	Local    probe.Local
	Registry probe.Registry
}

// Inspect resolves the target and observes local capabilities once.
func (resolver Resolver) Inspect(ctx context.Context, name string) Resolution {
	aliases, _ := SSHConfigHosts(resolver.sshConfig())
	tailnet := probe.LoadTailnet(ctx, resolver.Tools)
	host := Host{Name: name, Hostname: name, Target: withUser(resolver.userFor(name), name)}
	known := false
	if alias, ok := matchAlias(name, aliases); ok {
		local := probe.LocalLayer(ctx, resolver.Tools, withUser(resolver.userFor(alias), alias))
		host = resolver.fromAlias(alias, local, tailnet)
		return Resolution{host, true, local, tailnet.Registry(host.Name, local.Hostname)}
	} else if peer, ok := tailnet.Find(name); ok && peer.TailscaleSSH {
		host, known = resolver.fromPeer(peer), true
	}
	local := probe.LocalLayer(ctx, resolver.Tools, host.Target)
	return Resolution{host, known, local, tailnet.Registry(host.Name, local.Hostname)}
}

func (resolver Resolver) fromAlias(alias string, local probe.Local, tailnet probe.Tailnet) Host {
	host := Host{Name: alias, Source: SourceSSHConfig, Hostname: local.Hostname, User: local.User}
	if user := resolver.userFor(alias); user != "" {
		host.User = user
	}
	host.Target = withUser(resolver.userFor(alias), alias)
	if peer, ok := tailnet.Find(alias, local.Hostname); ok {
		host.Peer = &peer
		host.Source = SourceBoth
	}
	return host
}

func (resolver Resolver) fromPeer(peer probe.Peer) Host {
	host := Host{Name: peer.Name, Source: SourceTailnet, Hostname: peer.DNSName, Peer: &peer}
	host.User = resolver.userFor(peer.Name)
	host.Target = withUser(host.User, peer.DNSName)
	return host
}

func withUser(user, target string) string {
	if user == "" {
		return target
	}
	return user + "@" + target
}

func matchAlias(name string, aliases []string) (string, bool) {
	for _, alias := range aliases {
		if strings.EqualFold(alias, name) {
			return alias, true
		}
	}
	return "", false
}

// List combines SSH config hosts and SSH-enabled tailnet peers, sorted by name.
// The local tailnet node is not a discovered candidate.
func (resolver Resolver) List(ctx context.Context) (Output, error) {
	path := resolver.sshConfig()
	aliases, err := SSHConfigHosts(path)
	if err != nil {
		return Output{}, err
	}
	tailnet := probe.LoadTailnet(ctx, resolver.Tools)
	output := Output{Version: Version, SSHConfig: path, Tailnet: tailnet.Present, Hosts: []Host{}}
	seen := map[string]struct{}{}
	for _, alias := range aliases {
		local := probe.LocalLayer(ctx, resolver.Tools, withUser(resolver.userFor(alias), alias))
		host := resolver.fromAlias(alias, local, tailnet)
		output.Hosts = append(output.Hosts, host)
		if host.Peer != nil {
			seen[host.Peer.Name] = struct{}{}
		}
	}
	for _, peer := range tailnet.Peers {
		if !peer.TailscaleSSH {
			continue
		}
		if _, ok := seen[peer.Name]; ok {
			continue
		}
		if tailnet.Self != nil && peer.Name == tailnet.Self.Name {
			continue
		}
		seen[peer.Name] = struct{}{}
		output.Hosts = append(output.Hosts, resolver.fromPeer(peer))
	}
	sort.SliceStable(output.Hosts, func(i, j int) bool {
		return strings.ToLower(output.Hosts[i].Name) < strings.ToLower(output.Hosts[j].Name)
	})
	return output, nil
}

// SSHConfigHosts returns the literal Host entries of an ssh config, following
// Include. Patterns (*, ?, !) are not hosts. A missing file is an empty list.
func SSHConfigHosts(path string) ([]string, error) {
	seen := map[string]struct{}{}
	var hosts []string
	if err := collectHosts(path, seen, &hosts, 0); err != nil {
		return nil, err
	}
	return hosts, nil
}

func collectHosts(path string, seen map[string]struct{}, hosts *[]string, depth int) error {
	if depth > 8 {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			for _, name := range fields[1:] {
				if strings.ContainsAny(name, "*?!") {
					continue
				}
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				*hosts = append(*hosts, name)
			}
		case "include":
			for _, pattern := range fields[1:] {
				for _, included := range includeFiles(path, pattern) {
					if err := collectHosts(included, seen, hosts, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	return scanner.Err()
}

func includeFiles(config, pattern string) []string {
	if strings.HasPrefix(pattern, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			pattern = filepath.Join(home, pattern[2:])
		}
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(filepath.Dir(config), pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}
