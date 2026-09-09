package command

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseTSHReadsTheSSHShape(t *testing.T) {
	t.Parallel()
	rows := []struct {
		args    []string
		want    tshOptions
		wantErr string
	}{
		{args: []string{"arrakis"}, want: tshOptions{host: "arrakis"}},
		{args: []string{"u@arrakis", "--", "true"}, want: tshOptions{host: "arrakis", user: "u", command: []string{"true"}}},
		{args: []string{"arrakis", "htop", "-d", "5"}, want: tshOptions{host: "arrakis", command: []string{"htop", "-d", "5"}}},
		{args: []string{"-p", "2222", "-l", "ops", "-tq", "arrakis"}, want: tshOptions{host: "arrakis", user: "ops", quiet: true,
			options: []sshOption{{'p', "2222"}, {'t', ""}}}},
		{args: []string{"-p2222", "-L8080:localhost:80", "-s", "main", "-o", "StrictHostKeyChecking=no", "arrakis"}, want: tshOptions{host: "arrakis", session: "main",
			options: []sshOption{{'p', "2222"}, {'L', "8080:localhost:80"}, {'o', "StrictHostKeyChecking=no"}}}},
		{args: []string{"--session=main", "--pin", "ssh", "--mode", "roam", "--dry-run", "--no-probe", "--quiet", "arrakis"}, want: tshOptions{host: "arrakis", session: "main", pin: "ssh", mode: "roam", dryRun: true, noProbe: true, quiet: true}},
		// -l wins over user@host; -- before the host ends the options.
		{args: []string{"-l", "ops", "--", "u@arrakis"}, want: tshOptions{host: "arrakis", user: "ops"}},
		{args: []string{"--login=ops", "--port", "2222", "arrakis"}, want: tshOptions{host: "arrakis", user: "ops", options: []sshOption{{'p', "2222"}}}},
		{args: []string{"--help"}, want: tshOptions{help: true}},
		{args: []string{"-h"}, want: tshOptions{help: true}},
		{args: []string{"--version"}, want: tshOptions{version: true}},
		{args: []string{}, wantErr: "a host is required"},
		{args: []string{"-p"}, wantErr: "-p needs a value"},
		{args: []string{"--session"}, wantErr: "--session needs a value"},
		{args: []string{"-Z", "arrakis"}, wantErr: "unknown option -Z"},
		{args: []string{"--bogus", "arrakis"}, wantErr: "unknown option --bogus"},
	}
	for _, row := range rows {
		got, err := parseTSH(row.args)
		if row.wantErr != "" {
			if err == nil || err.Error() != row.wantErr {
				t.Fatalf("%v: err %v, want %q", row.args, err, row.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%v: %v", row.args, err)
		}
		if !reflect.DeepEqual(got, row.want) {
			t.Fatalf("%v:\n got %+v\nwant %+v", row.args, got, row.want)
		}
	}
}

func TestCarryMapsSSHOptionsPerTier(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name        string
		options     []sshOption
		user        bool
		ssh         []string
		mosh        []string
		tty         bool
		moshUnfit   string
		nativeUnfit string
	}{
		{name: "nothing"},
		{name: "port and identity ride mosh through --ssh",
			options: []sshOption{{'p', "2222"}, {'i', "key"}},
			ssh:     []string{"-p", "2222", "-i", "key"}, mosh: []string{"--ssh=ssh -p 2222 -i key"},
			nativeUnfit: "caller ssh options (-p, -i) do not reach a wezterm domain"},
		{name: "-t is implied by mosh and harmless to a domain",
			options: []sshOption{{'t', ""}}, ssh: []string{"-t"}, tty: true},
		{name: "-T contradicts mosh",
			options: []sshOption{{'T', ""}}, ssh: []string{"-T"}, tty: true,
			moshUnfit:   "-T (no tty) contradicts mosh, which always allocates one",
			nativeUnfit: "caller ssh options (-T) do not reach a wezterm domain"},
		{name: "a forward filters mosh with the reason and still rides ssh",
			options: []sshOption{{'L', "8080:localhost:80"}, {'C', ""}},
			ssh:     []string{"-L", "8080:localhost:80", "-C"}, mosh: []string{"--ssh=ssh -C"},
			moshUnfit:   "-L port forwarding needs the ssh session mosh closes after bootstrap",
			nativeUnfit: "caller ssh options (-L, -C) do not reach a wezterm domain"},
		{name: "a user override alone makes a domain unfit",
			user: true, nativeUnfit: "caller ssh options (user) do not reach a wezterm domain"},
	}
	for _, row := range rows {
		got := carry(row.options, row.user)
		want := carried{ssh: row.ssh, mosh: row.mosh, tty: row.tty, moshUnfit: row.moshUnfit, nativeUnfit: row.nativeUnfit}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s:\n got %+v\nwant %+v", row.name, got, want)
		}
	}
	if withUser("ops", "u@h") != "ops@h" || withUser("ops", "h") != "ops@h" || withUser("", "u@h") != "u@h" {
		t.Fatal("withUser")
	}
}

func runTSH(t *testing.T, f *fake, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	streams := Streams{Stdout: &stdout, Stderr: &stderr, Version: "test", Tools: f.tools(epoch), SSHConfig: os.Getenv("TETHER_TEST_SSH_CONFIG")}
	streams.Getenv = func(key string) string { return f.env[key] }
	streams.Exec = func(path string, argv []string, _ []string) error {
		f.execs = append(f.execs, append([]string{path}, argv...))
		return nil
	}
	return RunTSH(args, streams), stdout.String(), stderr.String()
}

func TestTSHCarriesFlagsOnTheWinningTier(t *testing.T) {
	isolate(t)
	f := connectFake()
	f.env["WEZTERM_PANE"] = "3"

	// -p rides mosh through --ssh; the port makes the wezterm domain unfit,
	// so mosh-mux wins even inside WezTerm.
	code, _, stderr := runTSH(t, f, "-p", "22", "arrakis", "--", "true")
	if code != 0 || !strings.Contains(stderr, "tsh: mosh-mux -> arrakis") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if want := [][]string{{"/bin/mosh", "mosh", "--ssh=ssh -p 22", "arrakis", "--", "true"}}; !reflect.DeepEqual(f.execs, want) {
		t.Fatalf("execs %v, want %v", f.execs, want)
	}

	// A forward cannot ride mosh: ssh wins, verbatim flags, and the reason is
	// in the plan.
	f.execs = nil
	code, stdout, stderr := runTSH(t, f, "--dry-run", "-L", "8080:localhost:80", "-l", "ops", "arrakis")
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	output := decodePlan(t, stdout)
	if output.Chosen.Tier != "ssh" || strings.Join(output.Plan.Command, " ") != "ssh -L 8080:localhost:80 ops@arrakis" {
		t.Fatalf("plan: %+v", output)
	}
	if !strings.Contains(strings.Join(output.Reasons, "\n"), "mosh-mux filtered: -L port forwarding needs the ssh session mosh closes after bootstrap") {
		t.Fatalf("reasons: %v", output.Reasons)
	}

	// A pinned mosh with an unfit flag refuses; nothing is dropped silently.
	code, _, stderr = runTSH(t, f, "--pin", "mosh-mux", "-L", "8080:localhost:80", "arrakis")
	if code != 2 || !strings.Contains(stderr, "tsh: pinned tier mosh-mux unavailable: -L port forwarding") || len(f.execs) != 0 {
		t.Fatalf("exit %d stderr %q execs %v", code, stderr, f.execs)
	}

	// Plain: native-mux inside WezTerm, like tether connect.
	code, _, stderr = runTSH(t, f, "arrakis")
	if code != 0 || !strings.Contains(stderr, "tsh: native-mux -> arrakis in ssh:arrakis, pane 7") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if code, stdout, _ := runTSH(t, f, "--version"); code != 0 || stdout != "test\n" {
		t.Fatalf("version: %d %q", code, stdout)
	}
	if code, _, stderr := runTSH(t, f, "-h"); code != 0 || !strings.Contains(stderr, "tsh [ssh options]") {
		t.Fatalf("help: %d %q", code, stderr)
	}
}
