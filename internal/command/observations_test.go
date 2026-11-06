package command

import (
	"os"
	"strings"
	"testing"
	"time"

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

func TestConnectRejectsInventoryForAnotherConnection(t *testing.T) {
	for _, row := range []struct {
		name      string
		args      []string
		sshConfig string
		probes    int
	}{
		{"same connection", nil, "", 0},
		{"login option", []string{"-l", "ops"}, "", 1},
		{"login without probing", []string{"--no-probe", "-l", "ops"}, "", 0},
		{"configured user", nil, "hostname arrakis.stork-eel.ts.net\nuser ops\nport 22\n", 1},
		{"configured hostname", nil, "hostname replacement.example\nuser u\nport 22\n", 1},
		{"configured port", nil, "hostname arrakis.stork-eel.ts.net\nuser u\nport 2222\n", 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			isolate(t)
			f := connectFake()
			if code, _, stderr := runWith(t, f, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
				t.Fatal(stderr)
			}
			if row.sshConfig != "" {
				f.runs["ssh -G arrakis"] = row.sshConfig
			}
			// The new account has no mux. Old inventory must not add zmx attach.
			f.runs["ssh -o BatchMode=yes"] = "home:=/home/ops\n"
			f.calls = nil
			args := append([]string{"connect", "--dry-run", "-s", "review"}, row.args...)
			args = append(args, "arrakis")
			code, stdout, stderr := runWith(t, f, f.tools(epoch.Add(time.Minute)), args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			output := decodePlan(t, stdout)
			if got := f.called("ssh -o BatchMode=yes"); got != row.probes {
				t.Fatalf("probes=%d want=%d: %v", got, row.probes, f.calls)
			}
			if row.name != "same connection" && (output.Chosen.Tier != "ssh" || strings.Contains(stdout, "zmx attach")) {
				t.Fatalf("reused another connection's inventory: %s", stdout)
			}
			if row.name == "same connection" && output.Chosen.Tier != "mosh-mux" {
				t.Fatalf("discarded matching inventory: %s", stdout)
			}
		})
	}
}

func TestConnectRefreshesExpiredFailures(t *testing.T) {
	for _, row := range []struct {
		name    string
		failed  bool
		age     time.Duration
		noProbe bool
		probes  int
	}{
		{"failure in backoff", true, 4 * time.Minute, false, 0},
		{"failure at expiry", true, probe.NegativeTTL, false, 1},
		{"failure after expiry", true, 6 * time.Minute, false, 1},
		{"failure no probe", true, 6 * time.Minute, true, 0},
		{"success fresh", false, 6 * time.Minute, false, 0},
		{"success expired", false, probe.HostTTL + time.Second, false, 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			isolate(t)
			f := connectFake()
			remote := f.runs["ssh -o BatchMode=yes"]
			if row.failed {
				delete(f.runs, "ssh -o BatchMode=yes")
			}
			if code, _, stderr := runWith(t, f, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
				t.Fatal(stderr)
			}
			f.runs["ssh -o BatchMode=yes"] = remote
			f.calls = nil
			args := []string{"connect", "--dry-run"}
			if row.noProbe {
				args = append(args, "--no-probe")
			}
			args = append(args, "arrakis", "--", "true")
			code, stdout, stderr := runWith(t, f, f.tools(epoch.Add(row.age)), args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if got := f.called("ssh -o BatchMode=yes"); got != row.probes {
				t.Fatalf("probes=%d want=%d: %v", got, row.probes, f.calls)
			}
			want := "mosh-mux"
			if row.failed && row.probes == 0 {
				want = "ssh"
			}
			if got := decodePlan(t, stdout).Chosen.Tier; got != want {
				t.Fatalf("tier=%s want=%s", got, want)
			}
		})
	}
}
