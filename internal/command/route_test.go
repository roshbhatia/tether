package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/roshbhatia/tether/internal/hosts"
)

func TestConfiguredAddressRouting(t *testing.T) {
	for _, tc := range []struct {
		name        string
		online      bool
		stderr      string
		wantAddress string
	}{
		{name: "prefers primary", online: true, wantAddress: "arrakis"},
		{name: "offline tailnet", wantAddress: "arrakis.local"},
		{name: "dns failure", online: true, stderr: "ssh: Could not resolve hostname arrakis: nodename not known", wantAddress: "arrakis.local"},
		{name: "refused connection", online: true, stderr: "ssh: connect to host arrakis port 22: Connection refused", wantAddress: "arrakis.local"},
		{name: "authentication error", online: true, stderr: "u@arrakis: Permission denied (publickey)."},
		{name: "changed host key", online: true, stderr: "Host key verification failed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := isolate(t)
			if err := os.WriteFile(path, []byte(sshConfig+"Host arrakis.local arrakis.example.\n  User u\n"), 0600); err != nil {
				t.Fatal(err)
			}
			writeSettings(t, `{"hosts":{"arrakis":{"fallback":["arrakis.local"],"aliases":["arrakis.example."]}}}`)
			f := newFake()
			if !tc.online {
				f.runs["tailscale status --json"] = strings.ReplaceAll(f.runs["tailscale status --json"], `"Online":true`, `"Online":false`)
			}
			f.runs["ssh -G arrakis.local"] = "hostname arrakis.local\nuser u\nport 22\n"
			// The precise fixture must win over the more general prefix.
			delete(f.runs, "ssh -G arrakis")
			tools := f.tools(epoch)
			runTool := tools.Run
			var checked []string
			tools.Run = func(ctx context.Context, binary string, args ...string) ([]byte, []byte, error) {
				switch {
				case len(args) == 2 && args[0] == "-G" && args[1] == "arrakis":
					return []byte("hostname arrakis.stork-eel.ts.net\nuser u\nport 22\n"), nil, nil
				case len(args) > 4 && args[1] == "BatchMode=yes":
					alias := args[len(args)-3]
					checked = append(checked, alias)
					if !strings.Contains(strings.Join(args, " "), "StrictHostKeyChecking=yes") {
						t.Fatal("route did not verify the host key")
					}
					if alias == "arrakis" && tc.stderr != "" {
						return nil, []byte(tc.stderr), errors.New("ssh exited 255")
					}
				}
				return runTool(ctx, binary, args...)
			}
			code, stdout, stderr := run(t, tools, "plan", "--host", "arrakis", "--resolve", "--native-prefix", "ssh:", "--mode", "native")
			if tc.wantAddress == "" {
				if code == 0 || len(checked) != 1 || !strings.Contains(stderr, tc.stderr) {
					t.Fatalf("route advanced on trust failure: %d %v %q", code, checked, stderr)
				}
				return
			}
			if code != 0 {
				t.Fatal(stderr)
			}
			connectCode, _, connectError := runWith(t, f, tools, "connect", "arrakis")
			if connectCode != 0 || len(f.execs) != 1 || f.execs[0][len(f.execs[0])-1] != tc.wantAddress {
				t.Fatalf("connect did not exec the selected endpoint: %v %s", f.execs, connectError)
			}
			output := decodePlan(t, stdout)
			if output.Host != "arrakis" || output.Hop.Kind != "native" || output.Hop.Ref != "ssh:"+tc.wantAddress {
				t.Fatalf("wrong route: %v", output)
			}
			_, stdout, _ = run(t, tools, "hosts", "--json")
			var catalog hosts.Output
			if err := json.Unmarshal([]byte(stdout), &catalog); err != nil {
				t.Fatal(err)
			}
			if len(catalog.Hosts) != 1 || strings.Join(catalog.Hosts[0].Addresses, ",") != "arrakis,arrakis.local" {
				t.Fatal(stdout)
			}
			_, stdout, _ = run(t, tools, "hosts", "--names")
			if !strings.Contains(stdout, "arrakis.local\n") {
				t.Fatal(stdout)
			}
		})
	}
}
