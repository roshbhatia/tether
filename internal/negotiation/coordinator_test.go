package negotiation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/tether/internal/config"
	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/probe"
)

func TestEvaluationUsesOnlyObservedFacts(t *testing.T) {
	t.Parallel()
	f := fullFake()
	tools := f.tools(epoch)
	store := probe.Store{Directory: t.TempDir()}
	coordinator := Coordinator{Tools: tools, Resolver: hosts.Resolver{Tools: tools, SSHConfig: filepath.Join(store.Directory, "absent")}, Store: store}
	if _, err := coordinator.Probe(context.Background(), "arrakis", false); err != nil {
		t.Fatal(err)
	}
	facts, err := coordinator.Observe(context.Background(), "arrakis", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Evaluate(config.Default(), facts, Request{Native: "ssh:arrakis"})
	if err != nil {
		t.Fatal(err)
	}
	calls := len(f.calls)
	if err := os.RemoveAll(store.Directory); err != nil {
		t.Fatal(err)
	}
	f.runs = map[string]string{}
	second, err := Evaluate(config.Default(), facts, Request{Native: "ssh:arrakis"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(first)
	after, _ := json.Marshal(second)
	if string(before) != string(after) || len(f.calls) != calls {
		t.Fatal("evaluation reloaded external state")
	}
}

var epoch = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// fake is a scripted process boundary. Keys are the command name followed by
// its arguments; a missing key is a LookPath miss or a run error.
type fake struct {
	paths map[string]string
	runs  map[string]string
	fails map[string]string
	calls []string
}

func (f *fake) tools(now time.Time) probe.Tools {
	return probe.Tools{
		LookPath: func(name string) (string, error) {
			if path, ok := f.paths[name]; ok {
				return path, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
			key := filepath.Base(name) + " " + strings.Join(args, " ")
			f.calls = append(f.calls, key)
			for prefix, stderr := range f.fails {
				if strings.HasPrefix(key, prefix) {
					return nil, []byte(stderr), errors.New("exit status 255")
				}
			}
			for prefix, stdout := range f.runs {
				if strings.HasPrefix(key, prefix) {
					return []byte(stdout), nil, nil
				}
			}
			return nil, nil, errors.New("unscripted: " + key)
		},
		Now: func() time.Time { return now },
	}
}

func testCoordinator(tools probe.Tools) Coordinator {
	return Coordinator{Tools: tools, Resolver: hosts.Resolver{Tools: tools, SSHConfig: filepath.Join(probe.DefaultStore().Directory, "absent")}, Store: probe.DefaultStore()}
}

func fullFake() *fake {
	return &fake{
		paths: map[string]string{"mosh": "/bin/mosh", "wezterm": "/bin/wezterm", "ssh": "/bin/ssh", "tailscale": "/bin/tailscale", "ping": "/sbin/ping"},
		runs: map[string]string{
			"ssh -G arrakis":          "user rshnbhatia\nhostname arrakis.stork-eel.ts.net\nport 22\n",
			"tailscale status --json": `{"Peer":{"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","TailscaleIPs":["100.94.4.109"],"Online":true,"Relay":"sea","CurAddr":""}}}`,
			"ssh -o BatchMode=yes":    "path:zmx=/bin/zmx\nversion:zmx=zmx\t\t0.7.0\npath:wezterm-mux-server=/bin/wezterm-mux-server\nversion:wezterm-mux-server=wezterm-mux-server 0-unstable-2026-08-23\n",
			"tailscale ping":          "pong from arrakis (100.94.4.109) via 192.168.50.18:41641 in 11ms\n",
		},
		fails: map[string]string{},
	}
}

func setState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("SYSINIT_PATHS_MANIFEST", filepath.Join(dir, "absent.json"))
	return dir
}

func TestRunWritesAVersionedRecord(t *testing.T) {
	dir := setState(t)
	f := fullFake()
	f.runs["tailscale status --json"] = `{"Peer":{"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","TailscaleIPs":["100.94.4.109"],"Online":true,"Relay":"sea","CurAddr":""}}}`

	record, err := testCoordinator(f.tools(epoch)).Probe(context.Background(), "arrakis", false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if record.Version != probe.HostVersion || record.Host != "arrakis" || !record.At.Equal(epoch) {
		t.Fatalf("header: %+v", record)
	}
	if record.Local.Mosh != "/bin/mosh" || record.Local.Hostname != "arrakis.stork-eel.ts.net" || record.Local.User != "rshnbhatia" || record.Local.Port != 22 {
		t.Fatalf("local: %+v", record.Local)
	}
	if !record.Registry.Present || !record.Registry.Peer || !record.Registry.Online || record.Registry.Relay != "sea" || record.Registry.Direct {
		t.Fatalf("registry: %+v", record.Registry)
	}
	if !record.Remote.OK || record.Remote.Zmx != "/bin/zmx" || record.Remote.WeztermMuxServer == "" || record.Remote.MoshServer != "" || record.Remote.Tmux != "" {
		t.Fatalf("remote: %+v", record.Remote)
	}
	if record.Remote.Versions["zmx"] != "zmx 0.7.0" {
		t.Fatalf("versions: %+v", record.Remote.Versions)
	}
	if record.Link == nil || record.Link.RTTMs != 11 || record.Link.Loss != 0 || record.Link.Direct == nil || !*record.Link.Direct || record.Link.Tool != "tailscale" {
		t.Fatalf("link: %+v", record.Link)
	}
	want := filepath.Join(dir, "tether", "hosts", "arrakis.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("record file: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(want)); len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	read, ok, err := probe.DefaultStore().Read("arrakis")
	if err != nil || !ok || read.Remote.Zmx != "/bin/zmx" {
		t.Fatalf("Read() = %+v, %v, %v", read, ok, err)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "ssh -o BatchMode=yes -o ConnectTimeout=5 arrakis -- sh -c '") {
			return
		}
	}
	t.Fatalf("remote round trip not made: %v", f.calls)
}

func TestRunStallGuardSkipsSSHForAnOfflineTailnetPeer(t *testing.T) {
	setState(t)
	f := fullFake()
	f.runs["tailscale status --json"] = `{"Peer":{"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","Online":false,"Relay":"sea","CurAddr":""}}}`

	record, err := testCoordinator(f.tools(epoch)).Probe(context.Background(), "arrakis", false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if record.Remote.OK || !strings.Contains(record.Remote.Reason, "offline") || record.Link != nil {
		t.Fatalf("record: remote %+v link %+v", record.Remote, record.Link)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "ssh -o BatchMode") || strings.HasPrefix(call, "tailscale ping") || strings.HasPrefix(call, "ping") {
			t.Fatalf("network touched: %s", call)
		}
	}
}

func TestRunWithoutTailscaleFallsBackToConnectTimeout(t *testing.T) {
	setState(t)
	f := fullFake()
	delete(f.paths, "tailscale")
	f.runs["ping -c 3"] = "64 bytes from 10.0.0.2: icmp_seq=0 ttl=64 time=8.1 ms\n64 bytes from 10.0.0.2: icmp_seq=1 ttl=64 time=9.9 ms\n\n--- ping statistics ---\n3 packets transmitted, 2 packets received, 33.3% packet loss\n"

	record, err := testCoordinator(f.tools(epoch)).Probe(context.Background(), "arrakis", false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if record.Registry.Present || !record.Remote.OK {
		t.Fatalf("record: %+v", record)
	}
	if record.Link == nil || record.Link.Tool != "ping" || record.Link.RTTMs != 9 || record.Link.Loss < 0.33 || record.Link.Loss > 0.34 || record.Link.Direct != nil {
		t.Fatalf("link: %+v", record.Link)
	}
	var sawTimeout bool
	for _, call := range f.calls {
		sawTimeout = sawTimeout || strings.Contains(call, "ConnectTimeout=5")
	}
	if !sawTimeout {
		t.Fatalf("no connect timeout: %v", f.calls)
	}
}

func TestRunNegativeCacheBacksOff(t *testing.T) {
	setState(t)
	f := fullFake()
	f.fails["ssh -o BatchMode"] = "ssh: connect to host arrakis.stork-eel.ts.net port 22: Operation timed out"

	first, err := testCoordinator(f.tools(epoch)).Probe(context.Background(), "arrakis", false)
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if first.Remote.OK || first.Remote.Reason != "ssh: ssh: connect to host arrakis.stork-eel.ts.net port 22: Operation timed out" {
		t.Fatalf("first remote: %+v", first.Remote)
	}
	if !first.InBackoff(epoch.Add(time.Minute)) || first.InBackoff(epoch.Add(probe.NegativeTTL)) {
		t.Fatalf("backoff window wrong: %+v", first.Remote)
	}

	// Inside the window the round trip is skipped and the failure kept.
	f.calls = nil
	delete(f.fails, "ssh -o BatchMode")
	second, err := testCoordinator(f.tools(epoch.Add(time.Minute))).Probe(context.Background(), "arrakis", false)
	if err != nil {
		t.Fatalf("second Run() error: %v", err)
	}
	if second.Remote.OK || !second.Remote.At.Equal(epoch) {
		t.Fatalf("second remote: %+v", second.Remote)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "ssh -o BatchMode") {
			t.Fatalf("round trip inside backoff: %v", f.calls)
		}
	}

	// --force ignores the window; so does its expiry.
	third, err := testCoordinator(f.tools(epoch.Add(time.Minute))).Probe(context.Background(), "arrakis", true)
	if err != nil || !third.Remote.OK {
		t.Fatalf("forced Run() = %+v, %v", third.Remote, err)
	}
}

func TestObserveDiscardsAnotherConnectionsBackoff(t *testing.T) {
	t.Parallel()
	f := fullFake()
	tools := f.tools(epoch)
	store := probe.Store{Directory: t.TempDir()}
	coordinator := Coordinator{Tools: tools, Resolver: hosts.Resolver{Tools: tools, SSHConfig: filepath.Join(store.Directory, "absent")}, Store: store}
	f.fails["ssh -o BatchMode"] = "permission denied"
	if _, err := coordinator.Probe(context.Background(), "arrakis", false); err != nil {
		t.Fatal(err)
	}
	delete(f.fails, "ssh -o BatchMode")
	f.calls = nil
	facts, err := coordinator.Observe(context.Background(), "arrakis", "ops")
	if err != nil {
		t.Fatal(err)
	}
	if facts.Cached || facts.Record.Remote.Reason != "" {
		t.Fatalf("another account's failure survived: %+v", facts)
	}
	facts, err = coordinator.Refresh(context.Background(), facts, false)
	if err != nil || !facts.Record.Remote.OK {
		t.Fatalf("new account stayed in backoff: %+v %v", facts.Record, err)
	}
	if facts.Record.Target != "ops@arrakis" {
		t.Fatalf("target=%s", facts.Record.Target)
	}
	var probes int
	for _, call := range f.calls {
		if strings.HasPrefix(call, "ssh -o BatchMode") {
			probes++
			if !strings.Contains(call, "ops@arrakis --") {
				t.Fatalf("wrong remote target: %s", call)
			}
		}
	}
	if probes != 1 {
		t.Fatalf("remote probes=%d", probes)
	}
}
