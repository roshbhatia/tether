package command

import (
	"bytes"
	"reflect"
	"testing"
)

func TestInteractiveNativeConnect(t *testing.T) {
	for _, tc := range []struct {
		args                          []string
		interactive, live, wantNative bool
	}{
		{[]string{"connect", "arrakis"}, true, true, true},
		{[]string{"connect", "arrakis"}, true, false, false},
		{[]string{"connect", "arrakis"}, false, true, false},
		{[]string{"connect", "--foreground", "arrakis"}, true, true, false},
		{[]string{"connect", "--mode", "roam", "arrakis"}, true, true, false},
		{[]string{"connect", "arrakis", "--", "htop"}, true, true, false},
		{[]string{"attach", "arrakis"}, false, false, true},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			path := isolate(t)
			f := newFake()
			f.runs["wezterm cli --no-auto-start list-clients --format json"] = "[{}]"
			f.runs["wezterm cli --no-auto-start list --format json"] = `[{"pane_id":99}]`
			if tc.live {
				f.runs["wezterm cli --no-auto-start list --format json"] = `[{"pane_id":12}]`
			}
			var stdout, stderr bytes.Buffer
			streams := Streams{Stdout: &stdout, Stderr: &stderr, SSHConfig: path, Tools: f.tools(epoch),
				Interactive: func() bool { return tc.interactive },
				Getenv:      func(string) string { return "12" },
				Exec: func(path string, argv []string, _ []string) error {
					f.execs = append(f.execs, append([]string{path}, argv...))
					return nil
				},
			}
			if status := Run(tc.args, streams); status != 0 || len(f.execs) != 1 {
				t.Fatalf("status %d execs %v %s", status, f.execs, stderr.String())
			}
			want := []string{"/bin/wezterm", "wezterm", "start", "--new-tab", "--attach", "--domain", "ssh:arrakis"}
			if tc.wantNative && !reflect.DeepEqual(f.execs[0], want) || !tc.wantNative && f.execs[0][0] != "/bin/ssh" {
				t.Fatalf("wrong realized route: %v", f.execs)
			}
		})
	}
}
