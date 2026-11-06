package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func parseTailscaleStatus(out []byte, host, hostname string) Registry {
	return parseTailnet(out).Registry(host, hostname)
}

func setState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("SYSINIT_PATHS_MANIFEST", filepath.Join(dir, "absent.json"))
	return dir
}

func TestReadMissesOnVersionMismatchAndMissingFile(t *testing.T) {
	setState(t)
	if _, ok, err := DefaultStore().Read("nowhere"); ok || err != nil {
		t.Fatalf("Read(missing) = %v, %v", ok, err)
	}
	if err := os.MkdirAll(DefaultStore().Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DefaultStore().Path("old"), []byte(`{"version":"tether.host/v0","host":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := DefaultStore().Read("old"); ok || err != nil {
		t.Fatalf("Read(old version) = %v, %v", ok, err)
	}
	if err := os.WriteFile(DefaultStore().Path("bad"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DefaultStore().Read("bad"); err == nil {
		t.Fatal("Read(bad json) returned no error")
	}
	if _, err := DefaultStore().List(); err == nil {
		t.Fatal("List() hid a corrupt record")
	}
	if err := os.Remove(DefaultStore().Path("bad")); err != nil {
		t.Fatal(err)
	}
	records, err := DefaultStore().List()
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
	for _, hostname := range []string{"arrakis.example.com", "arrakis.other-tail.ts.net"} {
		registry := parseTailscaleStatus(status, hostname, hostname)
		if registry.Peer {
			t.Fatalf("unrelated DNS name %s matched a peer: %+v", hostname, registry)
		}
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

func TestConnectionCompatibilityPreservesLegacyRecords(t *testing.T) {
	t.Parallel()
	record := Host{Host: "arrakis", Local: Local{Hostname: "arrakis.example", User: "u", Port: 22}}
	local := record.Local
	local.Mosh = "/new/local/mosh"
	if !record.MatchesConnection("arrakis", local) {
		t.Fatal("optional target or local tool change invalidated compatible inventory")
	}
}
