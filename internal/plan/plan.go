// Package plan renders a ranking as the tether.plan/v1 contract.
package plan

import (
	"time"

	"github.com/roshbhatia/tether/internal/rank"
)

// Version tags the plan output.
const Version = "tether.plan/v1"

// CommandPlan is orc's CommandPlan shape, so a later orc adapter passes it
// through unchanged.
type CommandPlan struct {
	Command      []string          `json:"command"`
	Cwd          *string           `json:"cwd" jsonschema:"nullable"`
	Environment  map[string]string `json:"environment"`
	SuccessCodes []int             `json:"successCodes"`
}

// Hop says where plan.command runs: "local" is the display side, "native" is
// the ref the caller named, and the command is then the inner argv unchanged.
type Hop struct {
	Kind string `json:"kind" jsonschema:"enum=local,enum=native"`
	Ref  string `json:"ref,omitempty" jsonschema:"description=Opaque caller ref; echoed and never parsed"`
}

// Chosen names the winning tier.
type Chosen struct {
	Tier string `json:"tier"`
	Rank int    `json:"rank"`
}

// Alternative is a surviving tier that did not win.
type Alternative struct {
	Tier  string      `json:"tier"`
	Rank  int         `json:"rank"`
	Plan  CommandPlan `json:"plan"`
	Hop   Hop         `json:"hop"`
	Gives []string    `json:"gives"`
	Loses []string    `json:"loses"`
}

// Filtered is a tier a hard requirement removed.
type Filtered struct {
	Tier   string `json:"tier"`
	Rank   int    `json:"rank"`
	Reason string `json:"reason"`
}

// Probe says how old the cached remote inventory is.
type Probe struct {
	At    *time.Time `json:"at" jsonschema:"nullable,description=When the remote inventory was taken; null with no record"`
	AgeS  *int       `json:"age_s" jsonschema:"nullable"`
	Stale bool       `json:"stale"`
}

// Output is the whole tether.plan/v1 document.
type Output struct {
	Version      string        `json:"version"`
	Host         string        `json:"host"`
	Session      string        `json:"session,omitempty"`
	Status       string        `json:"status" jsonschema:"enum=ok,enum=error"`
	Error        string        `json:"error,omitempty"`
	Chosen       *Chosen       `json:"chosen,omitempty"`
	Plan         *CommandPlan  `json:"plan,omitempty"`
	Hop          *Hop          `json:"hop,omitempty"`
	Gives        []string      `json:"gives,omitempty"`
	Loses        []string      `json:"loses,omitempty"`
	Reasons      []string      `json:"reasons"`
	Alternatives []Alternative `json:"alternatives"`
	Filtered     []Filtered    `json:"filtered"`
	Probe        Probe         `json:"probe"`
}

// Refs are the caller's opaque native refs.
type Refs struct {
	Native    string
	NativeRaw string
}

// CommandFor wraps the inner argv for one tier. tether never composes the
// inner command; it only decides the hop that carries it.
func CommandFor(tier, host string, inner []string, refs Refs) (CommandPlan, Hop) {
	command := CommandPlan{Environment: map[string]string{}, SuccessCodes: []int{0}}
	switch tier {
	case rank.NativeMux:
		command.Command = append([]string{}, inner...)
		return command, Hop{Kind: "native", Ref: refs.Native}
	case rank.SSHRaw:
		command.Command = append([]string{}, inner...)
		return command, Hop{Kind: "native", Ref: refs.NativeRaw}
	case rank.MoshMux:
		command.Command = wrap([]string{"mosh", host}, inner)
	default:
		command.Command = wrap([]string{"ssh", "-t", host}, inner)
	}
	return command, Hop{Kind: "local"}
}

func wrap(prefix, inner []string) []string {
	if len(inner) == 0 {
		return prefix
	}
	return append(append(prefix, "--"), inner...)
}

// Build renders a successful ranking.
func Build(host, session string, inner []string, refs Refs, result rank.Result, probe Probe) Output {
	output := Output{
		Version: Version, Host: host, Session: session, Status: "ok",
		Reasons: append([]string{}, result.Reasons...), Probe: probe,
		Alternatives: []Alternative{}, Filtered: filtered(result),
	}
	if len(result.Ordered) == 0 {
		return output
	}
	chosen := result.Ordered[0]
	command, hop := CommandFor(chosen.Name, host, inner, refs)
	output.Chosen = &Chosen{Tier: chosen.Name, Rank: chosen.Rank}
	output.Plan = &command
	output.Hop = &hop
	output.Gives = chosen.Gives
	output.Loses = chosen.Loses
	for _, tier := range result.Ordered[1:] {
		command, hop := CommandFor(tier.Name, host, inner, refs)
		output.Alternatives = append(output.Alternatives, Alternative{
			Tier: tier.Name, Rank: tier.Rank, Plan: command, Hop: hop, Gives: tier.Gives, Loses: tier.Loses,
		})
	}
	return output
}

// Failure renders a ranking error, such as a pinned tier that is unavailable.
func Failure(host, session string, err error, result rank.Result, probe Probe) Output {
	return Output{
		Version: Version, Host: host, Session: session, Status: "error", Error: err.Error(),
		Reasons: append([]string{}, result.Reasons...), Probe: probe,
		Alternatives: []Alternative{}, Filtered: filtered(result),
	}
}

func filtered(result rank.Result) []Filtered {
	out := make([]Filtered, 0, len(result.Filtered))
	for _, entry := range result.Filtered {
		out = append(out, Filtered{Tier: entry.Tier.Name, Rank: entry.Tier.Rank, Reason: entry.Reason})
	}
	return out
}
