package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/tether/internal/plan"
	"github.com/roshbhatia/tether/internal/probe"
)

var epoch = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

type fake struct {
	paths map[string]string
	runs  map[string]string
	calls []string
}

func (f *fake) tools(now time.Time) *probe.Tools {
	return &probe.Tools{
		LookPath: func(name string) (string, error) {
			if path, ok := f.paths[name]; ok {
				return path, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
			key := filepath.Base(name) + " " + strings.Join(args, " ")
			f.calls = append(f.calls, key)
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

func newFake() *fake {
	return &fake{
		paths: map[string]string{"wezterm": "/bin/wezterm", "ssh": "/bin/ssh", "tailscale": "/bin/tailscale", "ping": "/sbin/ping"},
		runs: map[string]string{
			"ssh -G arrakis":          "hostname arrakis.stork-eel.ts.net\nuser u\nport 22\n",
			"tailscale status --json": `{"Peer":{"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","Online":true,"Relay":"sea","CurAddr":""}}}`,
			"ssh -o BatchMode=yes":    "path:zmx=/bin/zmx\nversion:zmx=zmx 0.7.0\npath:wezterm-mux-server=/bin/wms\nversion:wezterm-mux-server=wezterm-mux-server 0-unstable\n",
			"tailscale ping":          "pong from arrakis (100.94.4.109) via DERP(sea) in 45ms\n",
		},
	}
}

func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("SYSINIT_PATHS_MANIFEST", filepath.Join(dir, "absent.json"))
	t.Setenv("TETHER_CONFIG", "")
}

func run(t *testing.T, tools *probe.Tools, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, Streams{Stdout: &stdout, Stderr: &stderr, Version: "test", Tools: tools})
	return code, stdout.String(), stderr.String()
}

func decodePlan(t *testing.T, raw string) plan.Output {
	t.Helper()
	var output plan.Output
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, raw)
	}
	return output
}

func TestPlanWithoutARecordLeavesTheBaseline(t *testing.T) {
	isolate(t)
	f := newFake()
	code, stdout, stderr := run(t, f.tools(epoch), "plan", "--host", "arrakis", "--native", "ssh:arrakis", "--", "zmx", "attach", "sysinit")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	output := decodePlan(t, stdout)
	if output.Version != plan.Version || output.Status != "ok" || output.Chosen.Tier != "ssh" {
		t.Fatalf("output: %+v", output)
	}
	if got := strings.Join(output.Plan.Command, " "); got != "ssh -t arrakis -- zmx attach sysinit" {
		t.Fatalf("command: %s", got)
	}
	if !output.Probe.Stale || output.Probe.At != nil {
		t.Fatalf("probe: %+v", output.Probe)
	}
	joined := strings.Join(output.Reasons, "\n")
	for _, want := range []string{"native-mux filtered: remote not probed", "remote: no host record; run tether probe --host arrakis", "local: mosh absent, wezterm present, ssh present"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reasons lack %q:\n%s", want, joined)
		}
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "ssh -o BatchMode") || strings.Contains(call, "ping") {
			t.Fatalf("plan touched the network: %s", call)
		}
	}
}

func TestProbeThenPlanChoosesFromTheCache(t *testing.T) {
	isolate(t)
	f := newFake()
	code, stdout, stderr := run(t, f.tools(epoch), "probe", "--host", "arrakis")
	if code != 0 {
		t.Fatalf("probe exit %d: %s", code, stderr)
	}
	var record probe.Host
	if err := json.Unmarshal([]byte(stdout), &record); err != nil || record.Version != probe.HostVersion || !record.Remote.OK {
		t.Fatalf("probe output: %v %+v", err, record)
	}

	// Local mosh absent: mosh-mux filtered with the reason, native-mux wins.
	later := epoch.Add(41 * time.Second)
	code, stdout, _ = run(t, f.tools(later), "plan", "--host", "arrakis", "--session", "sysinit", "--native", "ssh:arrakis", "--", "zmx", "attach", "sysinit")
	if code != 0 {
		t.Fatalf("plan exit %d", code)
	}
	output := decodePlan(t, stdout)
	if output.Chosen.Tier != "native-mux" || output.Hop.Kind != "native" || output.Hop.Ref != "ssh:arrakis" || output.Session != "sysinit" {
		t.Fatalf("output: %+v", output)
	}
	if got := strings.Join(output.Plan.Command, " "); got != "zmx attach sysinit" {
		t.Fatalf("native plan must be the inner argv unchanged: %s", got)
	}
	if *output.Probe.AgeS != 41 || output.Probe.Stale {
		t.Fatalf("probe: %+v", output.Probe)
	}
	joined := strings.Join(output.Reasons, "\n")
	for _, want := range []string{"mosh-mux filtered: local mosh absent, remote mosh-server absent", "link flaky: relayed via DERP", "remote: zmx 0.7.0, wezterm-mux-server 0-unstable"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reasons lack %q:\n%s", want, joined)
		}
	}
	if len(output.Alternatives) != 1 || output.Alternatives[0].Tier != "ssh" {
		t.Fatalf("alternatives: %+v", output.Alternatives)
	}

	// After the link TTL the sample no longer counts.
	code, stdout, _ = run(t, f.tools(epoch.Add(2*time.Minute)), "plan", "--host", "arrakis", "--native", "ssh:arrakis")
	if code != 0 || !strings.Contains(stdout, "link: no fresh sample") {
		t.Fatalf("stale link plan: %d %s", code, stdout)
	}

	// After the host TTL the plan is stale.
	code, stdout, _ = run(t, f.tools(epoch.Add(probe.HostTTL+time.Second)), "plan", "--host", "arrakis", "--native", "ssh:arrakis")
	if code != 0 || !decodePlan(t, stdout).Probe.Stale {
		t.Fatalf("stale host plan: %d %s", code, stdout)
	}
}

func TestPlanPinnedButUnavailableIsAnError(t *testing.T) {
	isolate(t)
	configDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tether")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"hosts":{"arrakis":{"pin":"mosh-mux"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFake()
	if code, _, stderr := run(t, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
		t.Fatalf("probe exit %d: %s", code, stderr)
	}
	code, stdout, _ := run(t, f.tools(epoch), "plan", "--host", "arrakis", "--native", "ssh:arrakis")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	output := decodePlan(t, stdout)
	if output.Status != "error" || output.Chosen != nil || !strings.Contains(output.Error, "pinned tier mosh-mux unavailable: local mosh absent") {
		t.Fatalf("output: %+v", output)
	}
	// A --pin flag overrides the config and can rescue the plan.
	code, stdout, _ = run(t, f.tools(epoch), "plan", "--host", "arrakis", "--native", "ssh:arrakis", "--pin", "ssh")
	if code != 0 || decodePlan(t, stdout).Chosen.Tier != "ssh" {
		t.Fatalf("pin override: %d %s", code, stdout)
	}
}

func TestPlanOfflinePeerMarksTheBaselineStale(t *testing.T) {
	isolate(t)
	f := newFake()
	if code, _, stderr := run(t, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
		t.Fatalf("probe exit %d: %s", code, stderr)
	}
	f.runs["tailscale status --json"] = `{"Peer":{"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","Online":false,"Relay":"sea","CurAddr":""}}}`
	code, stdout, _ := run(t, f.tools(epoch), "plan", "--host", "arrakis", "--native", "ssh:arrakis")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	output := decodePlan(t, stdout)
	if output.Chosen.Tier != "ssh" || !output.Probe.Stale || len(output.Alternatives) != 0 {
		t.Fatalf("output: %+v", output)
	}
	if !strings.Contains(strings.Join(output.Reasons, "\n"), "host offline: tailscale reports the peer offline") {
		t.Fatalf("reasons: %v", output.Reasons)
	}
}

func TestPlanRejectsBadModeAndMissingHost(t *testing.T) {
	isolate(t)
	f := newFake()
	if code, _, stderr := run(t, f.tools(epoch), "plan"); code != 2 || !strings.Contains(stderr, "--host is required") {
		t.Fatalf("missing host: %d %s", code, stderr)
	}
	code, stdout, _ := run(t, f.tools(epoch), "plan", "--host", "arrakis", "--mode", "fast")
	if code != 1 || decodePlan(t, stdout).Status != "error" {
		t.Fatalf("bad mode: %d %s", code, stdout)
	}
}

func TestStatusListsRecords(t *testing.T) {
	isolate(t)
	f := newFake()
	if code, _, stderr := run(t, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
		t.Fatalf("probe exit %d: %s", code, stderr)
	}
	code, stdout, stderr := run(t, f.tools(epoch.Add(time.Minute)), "status")
	if code != 0 {
		t.Fatalf("status exit %d: %s", code, stderr)
	}
	var output statusOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if output.Version != StatusVersion || len(output.Hosts) != 1 || output.Hosts[0].Host != "arrakis" || output.Hosts[0].AgeS != 60 || output.Hosts[0].Stale || !output.Hosts[0].RemoteOK || output.Hosts[0].Record != nil {
		t.Fatalf("status: %+v", output)
	}
	code, stdout, _ = run(t, f.tools(epoch), "status", "--host", "arrakis")
	if code != 0 || !strings.Contains(stdout, `"record"`) {
		t.Fatalf("status --host: %d %s", code, stdout)
	}
}

func TestVersionCompletionAndHelp(t *testing.T) {
	isolate(t)
	code, stdout, _ := run(t, nil, "--version")
	if code != 0 || stdout != "test\n" {
		t.Fatalf("--version: %d %q", code, stdout)
	}
	for _, shell := range []string{"bash", "fish", "nu", "zsh"} {
		code, stdout, stderr := run(t, nil, "completion", shell)
		if code != 0 || !strings.Contains(stdout, "tether") || stderr != "" {
			t.Fatalf("completion %s: %d %q", shell, code, stderr)
		}
	}
	code, _, stderr := run(t, nil, "--help")
	if code != 0 || !strings.Contains(stderr, "plan") || !strings.Contains(stderr, "probe") {
		t.Fatalf("--help: %d %q", code, stderr)
	}
	code, _, stderr = run(t, nil, "bogus")
	if code != 2 || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("bogus: %d %q", code, stderr)
	}
}
