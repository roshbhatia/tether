package command

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/tether/internal/probe"
)

func TestConnectUsesFreshFactsWhenCacheCannotBeSaved(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		t.Run(name, func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("directory permissions do not constrain root")
			}
			isolate(t)
			f := connectFake()
			if warm {
				if code, _, stderr := runWith(t, f, f.tools(epoch), "probe", "--host", "arrakis"); code != 0 {
					t.Fatal(stderr)
				}
				f.runs["ssh -o BatchMode=yes"] = "home:=/home/u\n"
			}
			store := probe.DefaultStore()
			if err := os.MkdirAll(store.Directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(store.Directory, 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(store.Directory, 0700); err != nil {
					t.Error(err)
				}
			})
			f.calls = nil
			code, out, stderr := runWith(t, f, f.tools(epoch.Add(probe.HostTTL+time.Minute)), "connect", "--dry-run", "-s", "main", "arrakis")
			if code != 0 || !strings.Contains(stderr, "could not save probe cache") {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
			want := "mosh-mux"
			if warm {
				want = "ssh"
			}
			if got := decodePlan(t, out).Chosen.Tier; got != want {
				t.Fatalf("tier=%s want=%s: %s", got, want, out)
			}
			if got := f.called("ssh -o BatchMode=yes"); got != 1 {
				t.Fatalf("remote probes=%d", got)
			}
			output := decodePlan(t, out)
			if output.Probe.At == nil || !output.Probe.At.Equal(epoch.Add(probe.HostTTL+time.Minute)) {
				t.Fatalf("not fresh: %+v", output.Probe)
			}
			if warm && !strings.Contains(stderr, "neither zmx nor tmux") {
				t.Fatalf("old session command survived: %s", stderr)
			}
			if code, _, stderr := runWith(t, f, f.tools(epoch.Add(probe.HostTTL+time.Minute)), "probe", "--host", "arrakis"); code != 1 || !strings.Contains(stderr, "create temp host record") {
				t.Fatalf("explicit probe lost persistence error: %d %s", code, stderr)
			}
		})
	}
}

func TestInvalidPreferencesAreRejectedBeforeObservation(t *testing.T) {
	for _, option := range []string{"--mode", "--pin"} {
		for _, command := range []string{"connect", "dry-run", "plan"} {
			t.Run(command+option, func(t *testing.T) {
				isolate(t)
				f := connectFake()
				args := []string{"connect", option, "invalid", "arrakis"}
				wantCode := 2
				if command == "dry-run" {
					args = append([]string{"connect", "--dry-run"}, args[1:]...)
				}
				if command == "plan" {
					args = []string{"plan", "--host", "arrakis", option, "invalid"}
					wantCode = 1
				}
				code, out, stderr := runWith(t, f, f.tools(epoch), args...)
				if code != wantCode {
					t.Fatalf("exit=%d want=%d: %s %s", code, wantCode, out, stderr)
				}
				if len(f.calls) != 0 {
					t.Fatalf("invalid input caused observation: %v", f.calls)
				}
				if command != "connect" {
					output := decodePlan(t, out)
					if output.Status != "error" || !strings.Contains(output.Error, "invalid") || output.Host != "arrakis" || output.Probe.At != nil {
						t.Fatalf("invalid error document: %s", out)
					}
				} else if !strings.Contains(stderr, "invalid") {
					t.Fatalf("missing diagnostic: %s", stderr)
				}
				if _, err := os.Stat(probe.DefaultStore().Directory); !os.IsNotExist(err) {
					t.Fatalf("invalid input touched cache: %v", err)
				}
			})
		}
	}
}

func TestInvalidConfiguredPinIsRejectedBeforeObservation(t *testing.T) {
	isolate(t)
	writeSettings(t, `{"hosts":{"arrakis":{"pin":"invalid"}}}`)
	f := connectFake()
	code, _, stderr := runWith(t, f, f.tools(epoch), "connect", "arrakis")
	if code != 1 || !strings.Contains(stderr, `hosts.arrakis.pin "invalid"`) || len(f.calls) != 0 {
		t.Fatalf("exit=%d stderr=%s calls=%v", code, stderr, f.calls)
	}
}
