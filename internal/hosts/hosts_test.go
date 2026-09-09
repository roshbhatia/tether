package hosts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/roshbhatia/tether/internal/probe"
)

const status = `{"Self":{"HostName":"urth","DNSName":"urth.stork-eel.ts.net.","OS":"macOS","Online":true},
"MagicDNSSuffix":"stork-eel.ts.net",
"Peer":{
 "k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","OS":"linux","TailscaleIPs":["100.94.4.109"],"Online":true,"Relay":"sea","CurAddr":"192.168.50.18:41641"},
 "k2":{"HostName":"lv426","DNSName":"lv426.stork-eel.ts.net.","OS":"macOS","Online":false,"LastSeen":"2026-09-05T06:42:59.1Z"},
 "k3":{"HostName":"portfolio","DNSName":"portfolio.stork-eel.ts.net.","OS":"linux","Tags":["tag:k8s"],"Online":true},
 "k4":{"HostName":"portfolio","DNSName":"portfolio-1.stork-eel.ts.net.","OS":"linux","Tags":["tag:k8s"],"Online":true},
 "k5":{"HostName":"vault","DNSName":"vault.stork-eel.ts.net.","OS":"linux","Online":true,"sshHostKeys":["ssh-ed25519 AAAA"]}
}}`

func fakeTools(runs map[string]string) probe.Tools {
	return probe.Tools{
		LookPath: func(name string) (string, error) {
			switch name {
			case "ssh", "tailscale":
				return "/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
			key := filepath.Base(name) + " " + strings.Join(args, " ")
			if out, ok := runs[key]; ok {
				return []byte(out), nil, nil
			}
			if strings.HasPrefix(key, "ssh -G ") {
				return []byte("hostname " + strings.TrimPrefix(key, "ssh -G ") + "\nuser me\n"), nil, nil
			}
			return nil, nil, errors.New("unscripted: " + key)
		},
	}
}

func writeConfig(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "config")
}

func TestSSHConfigHostsSkipsPatternsAndFollowsInclude(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, map[string]string{
		"config":      "# comment\nHost arrakis\n  HostName arrakis.stork-eel.ts.net\nInclude conf.d/*\nHost *.stork-eel.ts.net !bad\n  User u\nhost lv426 arrakis\nMatch host x\n",
		"conf.d/work": "Host huey\n  HostName huey.taila415c.ts.net\n",
	})
	hosts, err := SSHConfigHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"arrakis", "huey", "lv426"}; !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts %v, want %v", hosts, want)
	}
	if hosts, err := SSHConfigHosts(filepath.Join(t.TempDir(), "absent")); err != nil || hosts != nil {
		t.Fatalf("missing file: %v %v", hosts, err)
	}
}

func TestResolveOrderAliasThenTailnetThenUnknown(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, map[string]string{"config": "Host arrakis\n  HostName arrakis.stork-eel.ts.net\n  User rshnbhatia\nHost lan-only\n  HostName 10.0.0.5\n"})
	resolver := Resolver{
		Tools:     fakeTools(map[string]string{"tailscale status --json": status, "ssh -G arrakis": "hostname arrakis.stork-eel.ts.net\nuser rshnbhatia\n"}),
		SSHConfig: path,
		UserFor:   func(host string) string { return map[string]string{"vault": "ops", "lan-only": "root"}[host] },
	}
	rows := []struct {
		name   string
		known  bool
		source string
		target string
		host   string
		online *bool
	}{
		{"arrakis", true, SourceBoth, "arrakis", "arrakis", ptr(true)},
		{"ARRAKIS", true, SourceBoth, "arrakis", "arrakis", ptr(true)},
		{"lan-only", true, SourceSSHConfig, "root@lan-only", "lan-only", nil},
		{"vault", true, SourceTailnet, "ops@vault.stork-eel.ts.net", "vault", ptr(true)},
		{"vault.stork-eel.ts.net", true, SourceTailnet, "ops@vault.stork-eel.ts.net", "vault", ptr(true)},
		{"100.94.4.109", true, SourceTailnet, "arrakis.stork-eel.ts.net", "arrakis", ptr(true)},
		{"lv426", true, SourceTailnet, "lv426.stork-eel.ts.net", "lv426", ptr(false)},
		{"elsewhere.example.com", false, "", "elsewhere.example.com", "elsewhere.example.com", nil},
	}
	for _, row := range rows {
		host, known := resolver.Resolve(context.Background(), row.name)
		if known != row.known || host.Source != row.source || host.Target != row.target || host.Name != row.host {
			t.Fatalf("%s: known %v %+v", row.name, known, host)
		}
		if got := host.Online(); (got == nil) != (row.online == nil) || (got != nil && *got != *row.online) {
			t.Fatalf("%s: online %v", row.name, got)
		}
	}
	host, _ := resolver.Resolve(context.Background(), "vault")
	if !host.Peer.TailscaleSSH {
		t.Fatalf("vault advertises tailscale ssh: %+v", host.Peer)
	}
	host, _ = resolver.Resolve(context.Background(), "lv426")
	if host.Peer.LastSeen == nil || host.Peer.LastSeen.Year() != 2026 {
		t.Fatalf("offline peer keeps last seen: %+v", host.Peer)
	}
}

func TestListUnionsBothSourcesWithoutSelfOrDuplicates(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, map[string]string{"config": "Host arrakis\n  HostName arrakis.stork-eel.ts.net\nHost urth\n  HostName urth.stork-eel.ts.net\nHost huey\n  HostName huey.taila415c.ts.net\n"})
	resolver := Resolver{Tools: fakeTools(map[string]string{
		"tailscale status --json": status,
		"ssh -G arrakis":          "hostname arrakis.stork-eel.ts.net\n",
		"ssh -G urth":             "hostname urth.stork-eel.ts.net\n",
		"ssh -G huey":             "hostname huey.taila415c.ts.net\n",
	}), SSHConfig: path}
	output, err := resolver.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if output.Version != Version || !output.Tailnet || output.SSHConfig != path {
		t.Fatalf("output: %+v", output)
	}
	var got []string
	for _, host := range output.Hosts {
		got = append(got, host.Name+":"+host.Source)
	}
	want := []string{"arrakis:both", "huey:ssh-config", "lv426:tailnet", "portfolio:tailnet", "portfolio-1:tailnet", "urth:ssh-config", "vault:tailnet"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts %v, want %v", got, want)
	}
}

func TestListWithoutTailscaleIsTheSSHConfig(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, map[string]string{"config": "Host a\n"})
	tools := fakeTools(nil)
	tools.LookPath = func(name string) (string, error) {
		if name == "ssh" {
			return "/bin/ssh", nil
		}
		return "", errors.New("not found")
	}
	output, err := Resolver{Tools: tools, SSHConfig: path}.List(context.Background())
	if err != nil || output.Tailnet || len(output.Hosts) != 1 || output.Hosts[0].Source != SourceSSHConfig || output.Hosts[0].User != "me" {
		t.Fatalf("output: %+v %v", output, err)
	}
}

func ptr(value bool) *bool { return &value }
