package command

import (
	"os"
	"strings"
	"testing"

	"github.com/roshbhatia/tether/internal/probe"
)

func TestCommandsObserveOnce(t *testing.T) {
	isolate(t)
	f := connectFake()
	for _, row := range []struct {
		name   string
		args   []string
		remote int
	}{
		{"cold connect", []string{"connect", "arrakis", "--", "true"}, 1},
		{"warm connect", []string{"connect", "--no-probe", "arrakis", "--", "true"}, 0},
		{"plan", []string{"plan", "--host", "arrakis", "--", "true"}, 0},
		{"probe", []string{"probe", "--host", "arrakis"}, 1},
	} {
		f.calls = nil
		code, _, stderr := runWith(t, f, f.tools(epoch), row.args...)
		if code != 0 {
			t.Fatalf("%s: %d %s", row.name, code, stderr)
		}
		if f.called("ssh -G ") != 1 || f.called("tailscale status --json") != 1 {
			t.Fatalf("%s repeated observations: %v", row.name, f.calls)
		}
		if f.called("ssh -o BatchMode") != row.remote || f.called("tailscale ping") != row.remote {
			t.Fatalf("%s remote calls: %v", row.name, f.calls)
		}
		t.Logf("%s: ssh-G=1 tailscale-status=1 remote-SSH=%d", row.name, row.remote)
	}
}

func TestExplicitProbeRepairsCorruptCache(t *testing.T) {
	isolate(t)
	store := probe.DefaultStore()
	if err := os.MkdirAll(store.Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path("arrakis"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := connectFake()
	for _, args := range [][]string{{"plan", "--host", "arrakis"}, {"connect", "--no-probe", "arrakis"}} {
		code, _, stderr := runWith(t, f, f.tools(epoch), args...)
		if code != 1 || !strings.Contains(stderr, "decode") {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
	}
	if f.called("ssh -o BatchMode") != 0 {
		t.Fatal("cache failure initiated a remote probe")
	}
	if code, _, stderr := runWith(t, f, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
		t.Fatalf("repair: %d %s", code, stderr)
	}
	if record, found, err := store.Read("arrakis"); err != nil || !found || !record.Remote.OK {
		t.Fatalf("repaired record: %+v %v %v", record, found, err)
	}
}
