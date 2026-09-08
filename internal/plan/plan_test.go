package plan

import (
	"errors"
	"reflect"
	"testing"

	"github.com/roshbhatia/tether/internal/rank"
)

func TestCommandForWrapsAndNeverComposes(t *testing.T) {
	t.Parallel()

	inner := []string{"zmx", "attach", "sysinit"}
	refs := Refs{Native: "ssh:arrakis", NativeRaw: "raw:arrakis"}
	rows := []struct {
		tier    string
		command []string
		hop     Hop
	}{
		{rank.NativeMux, inner, Hop{Kind: "native", Ref: "ssh:arrakis"}},
		{rank.SSHRaw, inner, Hop{Kind: "native", Ref: "raw:arrakis"}},
		{rank.MoshMux, []string{"mosh", "arrakis", "--", "zmx", "attach", "sysinit"}, Hop{Kind: "local"}},
		{rank.SSH, []string{"ssh", "-t", "arrakis", "--", "zmx", "attach", "sysinit"}, Hop{Kind: "local"}},
	}
	for _, row := range rows {
		command, hop := CommandFor(row.tier, "arrakis", inner, refs)
		if !reflect.DeepEqual(command.Command, row.command) || hop != row.hop {
			t.Fatalf("%s: command %v hop %+v", row.tier, command.Command, hop)
		}
		if command.Cwd != nil || len(command.Environment) != 0 || !reflect.DeepEqual(command.SuccessCodes, []int{0}) {
			t.Fatalf("%s: unexpected plan fields %+v", row.tier, command)
		}
	}
	command, _ := CommandFor(rank.MoshMux, "arrakis", nil, refs)
	if !reflect.DeepEqual(command.Command, []string{"mosh", "arrakis"}) {
		t.Fatalf("empty inner: %v", command.Command)
	}
}

func TestBuildAndFailure(t *testing.T) {
	t.Parallel()

	result := rank.Result{
		Ordered:  []rank.Tier{rank.Tiers[1], rank.Tiers[3]},
		Filtered: []rank.Filtered{{Tier: rank.Tiers[0], Reason: "no native ref (--native)"}},
		Reasons:  []string{"mode roam"},
	}
	output := Build("arrakis", "sysinit", []string{"zmx", "attach", "sysinit"}, Refs{}, result, Probe{Stale: true})
	if output.Version != Version || output.Status != "ok" || output.Chosen.Tier != rank.MoshMux || output.Chosen.Rank != 2 {
		t.Fatalf("unexpected output: %+v", output)
	}
	if output.Hop.Kind != "local" || len(output.Alternatives) != 1 || output.Alternatives[0].Tier != rank.SSH {
		t.Fatalf("unexpected hop or alternatives: %+v", output)
	}
	if len(output.Filtered) != 1 || output.Filtered[0].Tier != rank.NativeMux || output.Filtered[0].Rank != 1 {
		t.Fatalf("unexpected filtered: %+v", output.Filtered)
	}
	if !output.Probe.Stale {
		t.Fatal("probe staleness dropped")
	}

	failure := Failure("arrakis", "", errors.New("pinned tier mosh-mux unavailable: local mosh absent"), result, Probe{})
	if failure.Status != "error" || failure.Chosen != nil || failure.Plan != nil || failure.Error == "" {
		t.Fatalf("unexpected failure: %+v", failure)
	}
}
