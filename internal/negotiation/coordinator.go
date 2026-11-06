package negotiation

import (
	"context"
	"time"

	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/probe"
)

type Facts struct {
	Resolution hosts.Resolution
	Record     probe.Host
	Cached     bool
	Now        time.Time
}

type Coordinator struct {
	Tools    probe.Tools
	Resolver hosts.Resolver
	Store    probe.Store
}

func (coordinator Coordinator) Observe(ctx context.Context, name, user string) (Facts, error) {
	resolver := coordinator.Resolver
	if user != "" {
		resolver.UserFor = func(string) string { return user }
	}
	resolution := resolver.Inspect(ctx, name)
	facts := Facts{Resolution: resolution, Now: coordinator.Tools.Now()}
	var err error
	facts.Record, facts.Cached, err = coordinator.Store.Read(resolution.Host.Name)
	if facts.Cached && !facts.Record.MatchesConnection(resolution.Host.Target, resolution.Local) {
		facts.Record, facts.Cached = probe.Host{}, false
	}
	return facts, err
}

func (coordinator Coordinator) Refresh(ctx context.Context, facts Facts, force bool) (Facts, error) {
	target := facts.Resolution.Host
	record := probe.Host{
		Version: probe.HostVersion, Host: target.Name, Target: target.Target,
		At: coordinator.Tools.Now(), Local: facts.Resolution.Local, Registry: facts.Resolution.Registry,
	}
	facts.Record = probe.Measure(ctx, coordinator.Tools, record, facts.Record, facts.Cached, force)
	facts.Cached = true
	facts.Now = coordinator.Tools.Now()
	return facts, coordinator.Store.Write(facts.Record)
}

func (coordinator Coordinator) Probe(ctx context.Context, name string, force bool) (probe.Host, error) {
	// An explicit probe can replace an unreadable cache, as before.
	facts, _ := coordinator.Observe(ctx, name, "")
	facts, err := coordinator.Refresh(ctx, facts, force)
	return facts.Record, err
}
