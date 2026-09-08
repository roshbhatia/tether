package rank

import (
	"errors"
	"strings"
	"testing"
)

// full is every capability present on both ends with both native refs.
var full = Caps{
	LocalMosh: true, LocalWezterm: true, LocalSSH: true,
	RemoteKnown: true, RemoteOK: true,
	RemoteMoshServer: true, RemoteZmx: true, RemoteTmux: true, RemoteMuxServer: true,
	NativeRef: "ssh:h", NativeRawRef: "ssh-raw:h",
}

var steady = Link{Known: true, RTTMs: 11, Loss: 0}

func prefs(mode Mode) Prefs { return Prefs{Mode: mode, FlakyRTTMs: 60, FlakyLoss: 0} }

func names(tiers []Tier) string {
	parts := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		parts = append(parts, tier.Name)
	}
	return strings.Join(parts, ",")
}

func with(base Caps, mutate func(*Caps)) Caps {
	mutate(&base)
	return base
}

func TestRankDecisionTable(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name     string
		caps     Caps
		link     Link
		prefs    Prefs
		want     string // ordered tier names
		filtered string // filtered tier names, in rank order
		reason   string // substring that must appear in Reasons
		flaky    bool
		offline  bool
	}{
		// (2) hard requirements
		{name: "everything, auto, steady", caps: full, link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,mosh-mux,ssh-raw,ssh", reason: "auto: link steady"},
		{name: "no local mosh removes mosh-mux",
			caps: with(full, func(c *Caps) { c.LocalMosh = false }), link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,ssh-raw,ssh", filtered: "mosh-mux", reason: "mosh-mux filtered: local mosh absent"},
		{name: "no remote mux-server removes native-mux",
			caps: with(full, func(c *Caps) { c.RemoteMuxServer = false }), link: steady, prefs: prefs(ModeAuto),
			want: "mosh-mux,ssh-raw,ssh", filtered: "native-mux", reason: "remote wezterm-mux-server absent"},
		{name: "no remote mosh-server removes mosh-mux",
			caps: with(full, func(c *Caps) { c.RemoteMoshServer = false }), link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,ssh-raw,ssh", filtered: "mosh-mux", reason: "remote mosh-server absent"},
		{name: "no remote zmx nor tmux removes mosh-mux",
			caps: with(full, func(c *Caps) { c.RemoteZmx = false; c.RemoteTmux = false }), link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,ssh-raw,ssh", filtered: "mosh-mux", reason: "remote zmx and tmux absent"},
		{name: "tmux alone keeps mosh-mux",
			caps: with(full, func(c *Caps) { c.RemoteZmx = false }), link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,mosh-mux,ssh-raw,ssh"},
		{name: "no local wezterm removes native-mux",
			caps: with(full, func(c *Caps) { c.LocalWezterm = false }), link: steady, prefs: prefs(ModeAuto),
			want: "mosh-mux,ssh-raw,ssh", filtered: "native-mux", reason: "local wezterm absent"},
		{name: "no native ref removes native-mux and ssh-raw",
			caps: with(full, func(c *Caps) { c.NativeRef = ""; c.NativeRawRef = "" }), link: steady, prefs: prefs(ModeAuto),
			want: "mosh-mux,ssh", filtered: "native-mux,ssh-raw", reason: "no native ref (--native)"},
		{name: "mux ref alone leaves ssh-raw out",
			caps: with(full, func(c *Caps) { c.NativeRawRef = "" }), link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,mosh-mux,ssh", filtered: "ssh-raw", reason: "no raw ssh domain declared"},
		{name: "remote not probed leaves 3 and 4",
			caps: with(full, func(c *Caps) { c.RemoteKnown = false }), link: Link{}, prefs: prefs(ModeAuto),
			want: "ssh-raw,ssh", filtered: "native-mux,mosh-mux", reason: "remote not probed"},
		{name: "offline by registry leaves a stale baseline",
			caps: with(full, func(c *Caps) { c.RegistryOffline = true }), link: Link{}, prefs: prefs(ModeAuto),
			want: "ssh", filtered: "native-mux,mosh-mux,ssh-raw", reason: "host offline: tailscale reports the peer offline; baseline only, marked stale", offline: true},
		{name: "offline by failed probe leaves a stale baseline",
			caps: with(full, func(c *Caps) { c.RemoteOK = false; c.RemoteReason = "ssh: connect timed out" }), link: Link{}, prefs: prefs(ModeRoam),
			want: "ssh", filtered: "native-mux,mosh-mux,ssh-raw", reason: "host offline: ssh: connect timed out", offline: true},
		// (3) modes
		{name: "native orders 1,3,4,2", caps: full, link: steady, prefs: prefs(ModeNative),
			want: "native-mux,ssh-raw,ssh,mosh-mux", reason: "mode native"},
		{name: "roam orders 2,4,1,3", caps: full, link: steady, prefs: prefs(ModeRoam),
			want: "mosh-mux,ssh,native-mux,ssh-raw", reason: "mode roam"},
		{name: "persist orders process-keeping tiers first", caps: full, link: steady, prefs: prefs(ModePersist),
			want: "native-mux,mosh-mux,ssh-raw,ssh", reason: "mode persist"},
		{name: "auto steady prefers native-mux", caps: full, link: steady, prefs: prefs(ModeAuto),
			want: "native-mux,mosh-mux,ssh-raw,ssh"},
		{name: "empty mode is auto", caps: full, link: steady, prefs: Prefs{FlakyRTTMs: 60},
			want: "native-mux,mosh-mux,ssh-raw,ssh", reason: "auto:"},
		// (4) flaky under auto
		{name: "auto relayed prefers mosh-mux", caps: full, link: Link{Known: true, RTTMs: 20, Relayed: true}, prefs: prefs(ModeAuto),
			want: "mosh-mux,native-mux,ssh-raw,ssh", reason: "link flaky: relayed via DERP", flaky: true},
		{name: "auto rtt over threshold prefers mosh-mux", caps: full, link: Link{Known: true, RTTMs: 61}, prefs: prefs(ModeAuto),
			want: "mosh-mux,native-mux,ssh-raw,ssh", reason: "rtt 61ms > 60ms", flaky: true},
		{name: "auto rtt at threshold is steady", caps: full, link: Link{Known: true, RTTMs: 60}, prefs: prefs(ModeAuto),
			want: "native-mux,mosh-mux,ssh-raw,ssh", reason: "link steady"},
		{name: "auto loss over threshold prefers mosh-mux", caps: full, link: Link{Known: true, RTTMs: 10, Loss: 0.34}, prefs: prefs(ModeAuto),
			want: "mosh-mux,native-mux,ssh-raw,ssh", reason: "loss 34% > 0%", flaky: true},
		{name: "configured threshold moves the line", caps: full, link: Link{Known: true, RTTMs: 90}, prefs: Prefs{Mode: ModeAuto, FlakyRTTMs: 100},
			want: "native-mux,mosh-mux,ssh-raw,ssh", reason: "link steady"},
		{name: "no fresh sample is not flaky", caps: full, link: Link{}, prefs: prefs(ModeAuto),
			want: "native-mux,mosh-mux,ssh-raw,ssh", reason: "link: no fresh sample"},
		{name: "flaky link does not reorder native mode", caps: full, link: Link{Known: true, Relayed: true}, prefs: prefs(ModeNative),
			want: "native-mux,ssh-raw,ssh,mosh-mux", flaky: true},
		{name: "flaky with no mosh falls to native-mux",
			caps: with(full, func(c *Caps) { c.LocalMosh = false }), link: Link{Known: true, Relayed: true}, prefs: prefs(ModeAuto),
			want: "native-mux,ssh-raw,ssh", filtered: "mosh-mux", flaky: true},
		// (1) pins
		{name: "pin wins over mode", caps: full, link: steady, prefs: Prefs{Mode: ModeNative, Pin: MoshMux, FlakyRTTMs: 60},
			want: "mosh-mux,native-mux,ssh-raw,ssh", reason: "pinned to mosh-mux"},
		{name: "pin to baseline wins over auto", caps: full, link: steady, prefs: Prefs{Mode: ModeAuto, Pin: SSH, FlakyRTTMs: 60},
			want: "ssh,native-mux,mosh-mux,ssh-raw"},
		// (5) ties break on tier number: ssh only
		{name: "only ssh present",
			caps: Caps{LocalSSH: true, RemoteKnown: true, RemoteOK: true}, link: steady, prefs: prefs(ModeRoam),
			want: "ssh", filtered: "native-mux,mosh-mux,ssh-raw"},
	}

	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			result, err := Rank(row.caps, row.link, row.prefs)
			if err != nil {
				t.Fatalf("Rank() error: %v", err)
			}
			if got := names(result.Ordered); got != row.want {
				t.Fatalf("ordered = %s, want %s\nreasons: %s", got, row.want, strings.Join(result.Reasons, " | "))
			}
			filtered := make([]Tier, 0, len(result.Filtered))
			for _, entry := range result.Filtered {
				filtered = append(filtered, entry.Tier)
			}
			if got := names(filtered); got != row.filtered {
				t.Fatalf("filtered = %q, want %q", got, row.filtered)
			}
			if row.reason != "" && !strings.Contains(strings.Join(result.Reasons, "\n"), row.reason) {
				t.Fatalf("reasons lack %q:\n%s", row.reason, strings.Join(result.Reasons, "\n"))
			}
			if result.Flaky != row.flaky || result.Offline != row.offline {
				t.Fatalf("flaky=%v offline=%v, want %v %v", result.Flaky, result.Offline, row.flaky, row.offline)
			}
		})
	}
}

func TestRankPinnedButUnavailableIsAnError(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name   string
		caps   Caps
		pin    string
		reason string
	}{
		{name: "pin mosh-mux without local mosh", caps: with(full, func(c *Caps) { c.LocalMosh = false }), pin: MoshMux, reason: "local mosh absent"},
		{name: "pin native-mux without remote mux-server", caps: with(full, func(c *Caps) { c.RemoteMuxServer = false }), pin: NativeMux, reason: "remote wezterm-mux-server absent"},
		{name: "pin native-mux without a ref", caps: with(full, func(c *Caps) { c.NativeRef = "" }), pin: NativeMux, reason: "no native ref"},
		{name: "pin ssh-raw without a raw ref", caps: with(full, func(c *Caps) { c.NativeRawRef = "" }), pin: SSHRaw, reason: "no raw ssh domain declared"},
		{name: "pin mosh-mux while offline", caps: with(full, func(c *Caps) { c.RegistryOffline = true }), pin: MoshMux, reason: "host offline"},
		{name: "pin native-mux before any probe", caps: with(full, func(c *Caps) { c.RemoteKnown = false }), pin: NativeMux, reason: "remote not probed"},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, err := Rank(row.caps, steady, Prefs{Mode: ModeAuto, Pin: row.pin, FlakyRTTMs: 60})
			var pinErr *PinError
			if !errors.As(err, &pinErr) {
				t.Fatalf("Rank() error = %v, want *PinError", err)
			}
			if pinErr.Tier != row.pin || !strings.Contains(pinErr.Reason, row.reason) {
				t.Fatalf("PinError = %+v, want tier %s reason containing %q", pinErr, row.pin, row.reason)
			}
		})
	}
}

func TestRankRejectsUnknownPinAndMode(t *testing.T) {
	t.Parallel()

	if _, err := Rank(full, steady, Prefs{Mode: ModeAuto, Pin: "telnet"}); err == nil || strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("unknown pin error = %v", err)
	}
	if _, err := Rank(full, steady, Prefs{Mode: "fast"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestRankNothingAvailableIsAnError(t *testing.T) {
	t.Parallel()

	_, err := Rank(Caps{}, Link{}, prefs(ModeAuto))
	if err == nil || !strings.Contains(err.Error(), "no tier available") {
		t.Fatalf("Rank() error = %v", err)
	}
}

func TestTiersUseTheFixedVocabulary(t *testing.T) {
	t.Parallel()

	vocabulary := map[string]struct{}{
		NativePanes: {}, OSC: {}, ByteClean: {}, Roaming: {}, LocalEcho: {}, Persistence: {}, Scrollback: {},
	}
	for index, tier := range Tiers {
		if tier.Rank != index+1 {
			t.Fatalf("tier %s has rank %d at index %d", tier.Name, tier.Rank, index)
		}
		seen := map[string]struct{}{}
		for _, capability := range append(append([]string{}, tier.Gives...), tier.Loses...) {
			if _, ok := vocabulary[capability]; !ok {
				t.Fatalf("tier %s uses %q outside the vocabulary", tier.Name, capability)
			}
			if _, dup := seen[capability]; dup {
				t.Fatalf("tier %s both gives and loses %q", tier.Name, capability)
			}
			seen[capability] = struct{}{}
		}
	}
}
