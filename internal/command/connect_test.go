package command

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/tether/internal/hosts"
	"github.com/roshbhatia/tether/internal/probe"
)

// connectFake has mosh locally and a full remote: mosh-mux wins outside
// WezTerm, native-mux inside it.
func connectFake() *fake {
	f := newFake()
	f.paths["mosh"] = "/bin/mosh"
	f.env = map[string]string{}
	f.runs["ssh -o BatchMode=yes"] = "home:=/home/u\npath:mosh-server=/bin/mosh-server\npath:zmx=/bin/zmx\nversion:zmx=zmx 0.7.0\npath:wezterm-mux-server=/bin/wms\n"
	f.runs["tailscale status --json"] = `{"Self":{"HostName":"urth","DNSName":"urth.stork-eel.ts.net."},"Peer":{
		"k1":{"HostName":"arrakis","DNSName":"arrakis.stork-eel.ts.net.","OS":"linux","Online":true,"Relay":"sea","CurAddr":"192.168.50.18:41641"},
		"k2":{"HostName":"vault","DNSName":"vault.stork-eel.ts.net.","OS":"linux","Online":true,"sshHostKeys":["ssh-ed25519 AAAA"]},
		"k3":{"HostName":"lv426","DNSName":"lv426.stork-eel.ts.net.","OS":"macOS","Online":false,"LastSeen":"2026-09-05T06:42:59.1Z"}}}`
	f.runs["tailscale ping"] = "pong from arrakis (100.94.4.109) via 192.168.50.18:41641 in 2ms\n"
	f.runs["wezterm cli list --format json"] = "[]"
	f.runs["wezterm cli spawn"] = "7\n"
	return f
}

func (f *fake) called(prefix string) int {
	count := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			count++
		}
	}
	return count
}

func TestDefaultInnerPerRemoteMux(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name    string
		inner   []string
		session string
		remote  probe.Remote
		known   bool
		want    []string
		note    string
	}{
		{"inner wins", []string{"htop"}, "s", probe.Remote{Zmx: "/bin/zmx"}, true, []string{"htop"}, ""},
		{"no session is a login shell", nil, "", probe.Remote{Zmx: "/bin/zmx"}, true, nil, ""},
		{"zmx first", nil, "s", probe.Remote{Zmx: "/bin/zmx", Tmux: "/bin/tmux"}, true, []string{"zmx", "attach", "s"}, ""},
		{"tmux second", nil, "s", probe.Remote{Tmux: "/bin/tmux"}, true, []string{"tmux", "new", "-A", "-s", "s"}, ""},
		{"no mux", nil, "s", probe.Remote{}, true, nil, "neither zmx nor tmux on h; opening a login shell instead of session s"},
		{"no record", nil, "s", probe.Remote{}, false, nil, "no host record for h; opening a login shell instead of session s"},
	}
	for _, row := range rows {
		got, note := defaultInner(row.inner, row.session, "h", row.remote, row.known)
		if !reflect.DeepEqual(got, row.want) || note != row.note {
			t.Fatalf("%s: %v %q", row.name, got, note)
		}
	}
}

func TestConnectOutsideWeztermExecsTheLocalHop(t *testing.T) {
	isolate(t)
	f := connectFake()
	code, stdout, stderr := runWith(t, f, f.tools(epoch), "connect", "arrakis", "--", "sh", "-c", "echo CONNECT_OK")
	if code != 0 || stdout != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	// No record: connect probes first, once.
	if f.called("ssh -o BatchMode=yes") != 1 {
		t.Fatalf("probe calls: %v", f.calls)
	}
	if !strings.Contains(stderr, "tether connect: probing arrakis\n") || !strings.Contains(stderr, "tether connect: mosh-mux -> arrakis (loses native-panes, osc, scrollback)\n") {
		t.Fatalf("stderr: %q", stderr)
	}
	want := [][]string{{"/bin/mosh", "mosh", "arrakis", "--", "sh", "-c", "echo CONNECT_OK"}}
	if !reflect.DeepEqual(f.execs, want) {
		t.Fatalf("execs %v, want %v", f.execs, want)
	}
	if f.called("wezterm") != 0 {
		t.Fatalf("plain terminal touched wezterm: %v", f.calls)
	}

	// A fresh record: no second probe, and --quiet drops the announcement.
	f.calls, f.execs = nil, nil
	code, _, stderr = runWith(t, f, f.tools(epoch.Add(time.Minute)), "connect", "-q", "arrakis")
	if code != 0 || stderr != "" || f.called("ssh -o BatchMode=yes") != 0 {
		t.Fatalf("exit %d stderr %q calls %v", code, stderr, f.calls)
	}
	if !reflect.DeepEqual(f.execs, [][]string{{"/bin/mosh", "mosh", "arrakis"}}) {
		t.Fatalf("login shell exec: %v", f.execs)
	}
}

func TestConnectInsideWeztermSpawnsANativeTab(t *testing.T) {
	isolate(t)
	f := connectFake()
	f.env["WEZTERM_PANE"] = "3"
	code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "-s", "sysinit", "arrakis")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if len(f.execs) != 0 {
		t.Fatalf("native hop must not exec: %v", f.execs)
	}
	if f.called("wezterm cli list --format json") != 1 || f.called("wezterm cli spawn --domain-name ssh:arrakis --cwd /home/u -- zmx attach sysinit") != 1 {
		t.Fatalf("wezterm calls: %v", f.calls)
	}
	if !strings.Contains(stderr, "tether connect: native-mux -> arrakis in ssh:arrakis, pane 7 (loses roaming, local-echo)\n") {
		t.Fatalf("stderr: %q", stderr)
	}

	// The mux not answering means no native ref: back to a local hop.
	delete(f.runs, "wezterm cli list --format json")
	f.calls = nil
	code, _, stderr = runWith(t, f, f.tools(epoch), "connect", "-s", "sysinit", "arrakis")
	if code != 0 || !strings.Contains(stderr, "tether connect: mosh-mux -> arrakis") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !reflect.DeepEqual(f.execs, [][]string{{"/bin/mosh", "mosh", "arrakis", "--", "zmx", "attach", "sysinit"}}) {
		t.Fatalf("execs: %v", f.execs)
	}

	// A login shell passes no command and no cwd: the domain's own default.
	f.runs["wezterm cli list --format json"] = "[]"
	f.calls = nil
	if code, _, _ = runWith(t, f, f.tools(epoch), "connect", "arrakis"); code != 0 || f.called("wezterm cli spawn --domain-name ssh:arrakis") != 1 || f.called("wezterm cli spawn --domain-name ssh:arrakis --cwd") != 0 {
		t.Fatalf("login shell spawn: %d %v", code, f.calls)
	}

	// A failed spawn is an error, never a silent fallback.
	delete(f.runs, "wezterm cli spawn")
	f.execs = nil
	code, _, stderr = runWith(t, f, f.tools(epoch), "connect", "arrakis")
	if code != 1 || !strings.Contains(stderr, "wezterm cli spawn --domain-name ssh:arrakis") || len(f.execs) != 0 {
		t.Fatalf("exit %d stderr %q execs %v", code, stderr, f.execs)
	}
}

func TestConnectSessionIsOptIn(t *testing.T) {
	isolate(t)
	f := connectFake()
	// No -s: a login shell, exactly like ssh arrakis.
	code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "arrakis")
	if code != 0 || !reflect.DeepEqual(f.execs, [][]string{{"/bin/mosh", "mosh", "arrakis"}}) {
		t.Fatalf("exit %d stderr %q execs %v", code, stderr, f.execs)
	}
	f.execs = nil
	if code, _, _ = runWith(t, f, f.tools(epoch), "connect", "-s", "main", "arrakis"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !reflect.DeepEqual(f.execs, [][]string{{"/bin/mosh", "mosh", "arrakis", "--", "zmx", "attach", "main"}}) {
		t.Fatalf("execs: %v", f.execs)
	}
	// The remote without a mux gets a note.
	f.runs["ssh -o BatchMode=yes"] = "path:mosh-server=/bin/mosh-server\n"
	f.execs = nil
	code, _, stderr = runWith(t, f, f.tools(epoch.Add(probe.HostTTL+time.Second)), "connect", "--session", "other", "arrakis")
	if code != 0 || !strings.Contains(stderr, "neither zmx nor tmux on arrakis; opening a login shell instead of session other") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	// mosh-mux needs a remote mux, so ssh carries the login shell.
	if !reflect.DeepEqual(f.execs, [][]string{{"/bin/ssh", "ssh", "arrakis"}}) {
		t.Fatalf("execs: %v", f.execs)
	}
}

func TestConnectTailnetOnlyHostTargetsTheMagicDNSName(t *testing.T) {
	isolate(t)
	writeSettings(t, `{"hosts":{"vault":{"user":"ops"}}}`)
	f := connectFake()
	f.env["WEZTERM_PANE"] = "3"
	code, stdout, stderr := runWith(t, f, f.tools(epoch), "connect", "--dry-run", "vault", "--", "true")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	output := decodePlan(t, stdout)
	if output.Host != "vault" || output.Target != "ops@vault.stork-eel.ts.net" || output.Chosen.Tier != "mosh-mux" {
		t.Fatalf("output: %+v", output)
	}
	if got := strings.Join(output.Plan.Command, " "); got != "mosh ops@vault.stork-eel.ts.net -- true" {
		t.Fatalf("command: %s", got)
	}
	if got := strings.Join(output.Alternatives[0].Plan.Command, " "); got != "ssh -t ops@vault.stork-eel.ts.net -- true" {
		t.Fatalf("ssh alternative: %s", got)
	}
	joined := strings.Join(output.Reasons, "\n")
	for _, want := range []string{"transport: ssh to ops@vault.stork-eel.ts.net (tailnet peer, not in ssh config; tailscale ssh advertised)", "native-mux filtered: no native ref (--native)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reasons lack %q:\n%s", want, joined)
		}
	}
	if f.called("ssh -o BatchMode=yes -o ConnectTimeout=5 ops@vault.stork-eel.ts.net") != 1 {
		t.Fatalf("probe target: %v", f.calls)
	}
	if len(f.execs) != 0 || f.called("wezterm cli spawn") != 0 {
		t.Fatalf("dry run acted: %v %v", f.execs, f.calls)
	}
}

func TestConnectOfflinePeerExitsWithoutProbing(t *testing.T) {
	isolate(t)
	f := connectFake()
	code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "lv426")
	if code != 1 || stderr != "tether connect: lv426 is offline on the tailnet (last seen 2026-09-05T06:42:59Z)\n" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if f.called("ssh -o BatchMode=yes") != 0 || len(f.execs) != 0 {
		t.Fatalf("offline peer touched: %v %v", f.calls, f.execs)
	}
}

func TestConnectPinnedButUnavailableExits2(t *testing.T) {
	isolate(t)
	f := connectFake()
	code, stdout, stderr := runWith(t, f, f.tools(epoch), "connect", "--pin", "native-mux", "arrakis")
	if code != 2 || stdout != "" || len(f.execs) != 0 {
		t.Fatalf("exit %d stdout %q execs %v", code, stdout, f.execs)
	}
	if !strings.Contains(stderr, "tether connect: pinned tier native-mux unavailable: no native ref (--native)") || !strings.Contains(stderr, "  native-mux filtered:") {
		t.Fatalf("stderr: %q", stderr)
	}
	code, stdout, _ = runWith(t, f, f.tools(epoch), "connect", "--pin", "native-mux", "--dry-run", "arrakis")
	if code != 2 || decodePlan(t, stdout).Status != "error" {
		t.Fatalf("dry run: %d %s", code, stdout)
	}
}

func TestConnectNoProbeAndUnknownHost(t *testing.T) {
	isolate(t)
	f := connectFake()
	code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "--no-probe", "arrakis")
	if code != 0 || f.called("ssh -o BatchMode=yes") != 0 {
		t.Fatalf("exit %d calls %v", code, f.calls)
	}
	if !strings.Contains(stderr, "tether connect: ssh -> arrakis") || !reflect.DeepEqual(f.execs, [][]string{{"/bin/ssh", "ssh", "arrakis"}}) {
		t.Fatalf("stderr %q execs %v", stderr, f.execs)
	}
	f.execs = nil
	code, _, stderr = runWith(t, f, f.tools(epoch), "connect", "--no-probe", "elsewhere.example.com")
	if code != 0 || !strings.Contains(stderr, "elsewhere.example.com is not in the ssh config and not a tailnet peer; trying ssh anyway") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !reflect.DeepEqual(f.execs, [][]string{{"/bin/ssh", "ssh", "elsewhere.example.com"}}) {
		t.Fatalf("execs %v", f.execs)
	}
}

func TestConnectUsage(t *testing.T) {
	isolate(t)
	f := connectFake()
	if code, _, stderr := runWith(t, f, f.tools(epoch), "connect"); code != 2 || !strings.Contains(stderr, "a host is required") {
		t.Fatalf("no host: %d %q", code, stderr)
	}
	if code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "-Z", "arrakis"); code != 2 || !strings.Contains(stderr, "unknown option -Z") {
		t.Fatalf("unknown option: %d %q", code, stderr)
	}
	if code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "--help"); code != 0 || !strings.Contains(stderr, "tether connect") {
		t.Fatalf("help: %d %q", code, stderr)
	}
	// ssh's shape: options, then the host, then the command; -- is optional.
	code, stdout, _ := runWith(t, f, f.tools(epoch), "connect", "--dry-run", "-s", "s", "arrakis", "htop", "-d", "5")
	if code != 0 {
		t.Fatalf("command after host: %d", code)
	}
	if output := decodePlan(t, stdout); output.Session != "s" || strings.Join(output.Plan.Command, " ") != "mosh arrakis -- htop -d 5" {
		t.Fatalf("plan: %+v", output)
	}
}

func TestHostsTableJSONAndNames(t *testing.T) {
	isolate(t)
	f := connectFake()
	code, stdout, stderr := runWith(t, f, f.tools(epoch), "hosts")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("table:\n%s", stdout)
	}
	var rows []string
	for _, line := range lines[1:] {
		rows = append(rows, strings.Join(strings.Fields(line), " "))
	}
	want := []string{
		"arrakis both arrakis.stork-eel.ts.net yes linux no",
		"lv426 tailnet lv426.stork-eel.ts.net no (seen 2026-09-05) macOS no",
		"vault tailnet vault.stork-eel.ts.net yes linux yes",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("table rows %q, want %q", rows, want)
	}
	code, stdout, _ = runWith(t, f, f.tools(epoch), "hosts", "--json")
	if code != 0 {
		t.Fatalf("json exit %d", code)
	}
	var output hosts.Output
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	if output.Version != hosts.Version || len(output.Hosts) != 3 || output.Hosts[0].Source != hosts.SourceBoth || !output.Hosts[2].Peer.TailscaleSSH {
		t.Fatalf("json: %+v", output)
	}
	code, stdout, _ = runWith(t, f, f.tools(epoch), "hosts", "--names")
	if code != 0 || stdout != "arrakis\nlv426\nvault\n" {
		t.Fatalf("names: %d %q", code, stdout)
	}
	if code, _, stderr := runWith(t, f, f.tools(epoch), "hosts", "extra"); code != 2 || !strings.Contains(stderr, "no positional") {
		t.Fatalf("positional: %d %q", code, stderr)
	}
}
