package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// fake is a scripted process boundary. Keys are the command name followed by
// its arguments; a missing key is a LookPath miss or a run error.
type fake struct {
	paths map[string]string
	runs  map[string]string
	fails map[string]string
	calls []string
}

func (f *fake) tools(now time.Time) Tools {
	return Tools{
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

	record, err := Run(context.Background(), f.tools(epoch), "arrakis", Options{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if record.Version != HostVersion || record.Host != "arrakis" || !record.At.Equal(epoch) {
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
	read, ok, err := Read("arrakis")
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

	record, err := Run(context.Background(), f.tools(epoch), "arrakis", Options{})
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

	record, err := Run(context.Background(), f.tools(epoch), "arrakis", Options{})
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

	first, err := Run(context.Background(), f.tools(epoch), "arrakis", Options{})
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if first.Remote.OK || first.Remote.Reason != "ssh: ssh: connect to host arrakis.stork-eel.ts.net port 22: Operation timed out" {
		t.Fatalf("first remote: %+v", first.Remote)
	}
	if !first.InBackoff(epoch.Add(time.Minute)) || first.InBackoff(epoch.Add(NegativeTTL)) {
		t.Fatalf("backoff window wrong: %+v", first.Remote)
	}

	// Inside the window the round trip is skipped and the failure kept.
	f.calls = nil
	delete(f.fails, "ssh -o BatchMode")
	second, err := Run(context.Background(), f.tools(epoch.Add(time.Minute)), "arrakis", Options{})
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
	third, err := Run(context.Background(), f.tools(epoch.Add(time.Minute)), "arrakis", Options{Force: true})
	if err != nil || !third.Remote.OK {
		t.Fatalf("forced Run() = %+v, %v", third.Remote, err)
	}
}

func TestReadMissesOnVersionMismatchAndMissingFile(t *testing.T) {
	setState(t)
	if _, ok, err := Read("nowhere"); ok || err != nil {
		t.Fatalf("Read(missing) = %v, %v", ok, err)
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path("old"), []byte(`{"version":"tether.host/v0","host":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Read("old"); ok || err != nil {
		t.Fatalf("Read(old version) = %v, %v", ok, err)
	}
	if err := os.WriteFile(Path("bad"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read("bad"); err == nil {
		t.Fatal("Read(bad json) returned no error")
	}
	if _, err := List(); err == nil {
		t.Fatal("List() hid a corrupt record")
	}
	if err := os.Remove(Path("bad")); err != nil {
		t.Fatal(err)
	}
	records, err := List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("List() = %+v", records)
	}
}

func TestStaleAndLinkFresh(t *testing.T) {
	t.Parallel()
	record := Host{At: epoch, Link: &Link{At: epoch}}
	if record.Stale(epoch.Add(HostTTL)) || !record.Stale(epoch.Add(HostTTL+time.Second)) {
		t.Fatal("host TTL boundary wrong")
	}
	if !record.LinkFresh(epoch.Add(LinkTTL)) || record.LinkFresh(epoch.Add(LinkTTL+time.Second)) {
		t.Fatal("link TTL boundary wrong")
	}
	if (Host{}).LinkFresh(epoch) {
		t.Fatal("nil link is fresh")
	}
}

func TestParseTailscaleStatusMatchesByEveryName(t *testing.T) {
	t.Parallel()
	status := []byte(`{"Peer":{"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","TailscaleIPs":["100.94.4.109"],"Online":true,"Relay":"sea","CurAddr":"192.168.50.18:41641"}}}`)
	for _, row := range [][2]string{{"arrakis", "arrakis.stork-eel.ts.net"}, {"box", "100.94.4.109"}, {"ARRAKIS", "arrakis"}, {"alias", "Arrakis.stork-eel.ts.net."}} {
		registry := parseTailscaleStatus(status, row[0], row[1])
		if !registry.Present || !registry.Peer || !registry.Online || !registry.Direct {
			t.Fatalf("%v: %+v", row, registry)
		}
	}
	registry := parseTailscaleStatus(status, "other", "other.example.com")
	if !registry.Present || registry.Peer {
		t.Fatalf("non-peer: %+v", registry)
	}
	if registry := parseTailscaleStatus([]byte("garbage"), "a", "a"); registry.Present {
		t.Fatalf("garbage parsed: %+v", registry)
	}
}

func TestGuardsRemoteOnlyForOfflineTailnetPeers(t *testing.T) {
	t.Parallel()
	offline := Registry{Present: true, Peer: true, Online: false}
	if skip, _ := GuardsRemote("arrakis.stork-eel.ts.net", offline); !skip {
		t.Fatal("offline tailnet peer not guarded")
	}
	if skip, _ := GuardsRemote("arrakis.example.com", offline); skip {
		t.Fatal("non-tailnet hostname guarded")
	}
	if skip, _ := GuardsRemote("arrakis.stork-eel.ts.net", Registry{Present: true, Peer: false}); skip {
		t.Fatal("unknown peer guarded")
	}
	if skip, _ := GuardsRemote("arrakis.stork-eel.ts.net", Registry{Present: true, Peer: true, Online: true}); skip {
		t.Fatal("online peer guarded")
	}
}

func TestParseTailscalePingRelayAndLoss(t *testing.T) {
	t.Parallel()
	link := parseTailscalePing([]byte("pong from arrakis (100.94.4.109) via DERP(sea) in 45ms\npong from arrakis (100.94.4.109) via DERP(sea) in 55ms\ntimed out\n"), epoch)
	if link == nil || link.RTTMs != 50 || link.Direct == nil || *link.Direct || link.Loss < 0.33 || link.Loss > 0.34 {
		t.Fatalf("link: %+v", link)
	}
	if parseTailscalePing([]byte(""), epoch) != nil {
		t.Fatal("empty output produced a sample")
	}
	if link := parseTailscalePing([]byte("timed out\ntimed out\ntimed out\n"), epoch); link == nil || link.Loss != 1 || link.Direct != nil {
		t.Fatalf("all lost: %+v", link)
	}
}

func TestRemoteScriptHasNoSingleQuotes(t *testing.T) {
	t.Parallel()
	if strings.Contains(remoteScript, "'") {
		t.Fatal("remote script cannot be wrapped in sh -c '...'")
	}
	if !strings.HasPrefix(RemoteScript(), "sh -c '") || !strings.HasSuffix(RemoteScript(), "'") {
		t.Fatalf("RemoteScript() = %q", RemoteScript())
	}
	for _, tool := range remoteTools {
		if !strings.Contains(remoteScript, tool) {
			t.Fatalf("script omits %s", tool)
		}
	}
}
