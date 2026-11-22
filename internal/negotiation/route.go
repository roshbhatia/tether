package negotiation

import (
	"context"
	"fmt"
	"strings"

	"github.com/roshbhatia/tether/internal/probe"
)

// Resolve checks addresses in order. Only a failed network connection permits
// advancing to another address. Inventory is observed before launching anything.
func (coordinator Coordinator) Resolve(ctx context.Context, addresses []string, user string) (Facts, error) {
	if user == "" && len(addresses) > 0 && coordinator.Resolver.UserFor != nil {
		user = coordinator.Resolver.UserFor(addresses[0])
	}
	var facts Facts
	var failures []string
	for _, address := range addresses {
		var err error
		facts, err = coordinator.Observe(ctx, address, user)
		if err != nil {
			return facts, err
		}
		resolution := facts.Resolution
		if skip, reason := probe.GuardsRemote(resolution.Local.Hostname, resolution.Registry); skip {
			failures = append(failures, address+": "+reason)
			continue
		}
		remote := probe.RouteLayer(ctx, coordinator.Tools, resolution.Host.Target, facts.Now)
		if remote.OK {
			facts.Record = probe.Host{
				Host: resolution.Host.Name, Target: resolution.Host.Target,
				At: facts.Now, Local: resolution.Local, Remote: remote,
				Registry: resolution.Registry, Link: facts.Record.Link,
			}
			facts.Cached = true
			return facts, nil
		}
		if !unreachable(remote.Reason) {
			return facts, fmt.Errorf("%s: %s", address, remote.Reason)
		}
		failures = append(failures, address+": "+remote.Reason)
	}
	return facts, fmt.Errorf("no reachable address (%s)", strings.Join(failures, "; "))
}

func unreachable(reason string) bool {
	reason = strings.ToLower(strings.TrimPrefix(reason, "ssh: "))
	reason = strings.TrimPrefix(reason, "ssh: ")
	if strings.HasPrefix(reason, "could not resolve hostname ") {
		return true
	}
	if !strings.HasPrefix(reason, "connect to host ") {
		return false
	}
	for _, failure := range []string{
		"operation timed out", "connection timed out", "connection refused",
		"no route to host", "network is unreachable", "network unreachable", "host is down",
	} {
		if strings.HasSuffix(reason, ": "+failure) {
			return true
		}
	}
	return false
}
