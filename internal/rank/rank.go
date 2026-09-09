// Package rank turns capabilities, a link sample, and preferences into an
// ordered list of tiers. It is a pure function with no I/O.
package rank

import (
	"fmt"
	"strings"
)

// Capability vocabulary. Every gives/loses entry is one of these.
const (
	NativePanes = "native-panes"
	OSC         = "osc"
	ByteClean   = "byte-clean"
	Roaming     = "roaming"
	LocalEcho   = "local-echo"
	Persistence = "persistence"
	Scrollback  = "scrollback"
)

// Tier names.
const (
	NativeMux = "native-mux"
	MoshMux   = "mosh-mux"
	SSHRaw    = "ssh-raw"
	SSH       = "ssh"
)

// Mode orders survivors.
type Mode string

const (
	ModeAuto    Mode = "auto"
	ModeNative  Mode = "native"
	ModeRoam    Mode = "roam"
	ModePersist Mode = "persist"
)

// Tier is one way to carry the inner command, best first by Rank.
type Tier struct {
	Name  string   `json:"tier"`
	Rank  int      `json:"rank"`
	Gives []string `json:"gives"`
	Loses []string `json:"loses"`
}

// Tiers is the fixed table, ordered by rank.
var Tiers = []Tier{
	{Name: NativeMux, Rank: 1,
		Gives: []string{NativePanes, OSC, ByteClean, Scrollback},
		Loses: []string{Roaming, LocalEcho}},
	{Name: MoshMux, Rank: 2,
		Gives: []string{Roaming, LocalEcho, Persistence},
		Loses: []string{NativePanes, OSC, Scrollback}},
	{Name: SSHRaw, Rank: 3,
		Gives: []string{NativePanes, OSC, ByteClean},
		Loses: []string{Roaming, LocalEcho, Persistence}},
	{Name: SSH, Rank: 4,
		Gives: []string{OSC, ByteClean},
		Loses: []string{NativePanes, Roaming, LocalEcho, Persistence}},
}

// TierNamed returns the tier and whether the name is known.
func TierNamed(name string) (Tier, bool) {
	for _, tier := range Tiers {
		if tier.Name == name {
			return tier, true
		}
	}
	return Tier{}, false
}

// Caps is what both ends can do, as the probe layers reported it.
type Caps struct {
	LocalMosh    bool
	LocalWezterm bool
	LocalSSH     bool

	// RemoteKnown is false when no remote probe has run for this host.
	RemoteKnown bool
	// RemoteOK is the round trip result; false with RemoteKnown means offline.
	RemoteOK         bool
	RemoteReason     string
	RemoteMoshServer bool
	RemoteZmx        bool
	RemoteTmux       bool
	RemoteMuxServer  bool

	// RegistryOffline is the node registry saying the peer is down.
	RegistryOffline bool

	// NativeRef is the caller's opaque mux-domain ref; "" means none.
	NativeRef string
	// NativeRawRef is the caller's opaque raw ssh-domain ref; "" means none.
	NativeRawRef string

	// MoshUnfit is why the caller's ssh options cannot ride mosh (a port
	// forward needs the ssh session mosh closes); "" means they can.
	MoshUnfit string
	// NativeUnfit is why the caller's ssh options cannot reach a native
	// domain, which has its own ssh configuration; "" means they can.
	NativeUnfit string
}

// Link is the fresh link sample; Known is false when there is none.
type Link struct {
	Known   bool
	RTTMs   float64
	Loss    float64
	Relayed bool
}

// Prefs are the caller's and the config's choices.
type Prefs struct {
	Mode       Mode
	Pin        string
	FlakyRTTMs float64
	FlakyLoss  float64
}

// Filtered is a tier that a hard requirement removed.
type Filtered struct {
	Tier   Tier
	Reason string
}

// Result is the ranking. Ordered[0] is the chosen tier.
type Result struct {
	Ordered  []Tier
	Filtered []Filtered
	Reasons  []string
	Flaky    bool
	Offline  bool
}

// PinError is a pinned tier the probe says is unavailable. It never falls
// back silently.
type PinError struct {
	Tier   string
	Reason string
}

func (err *PinError) Error() string {
	return fmt.Sprintf("pinned tier %s unavailable: %s", err.Tier, err.Reason)
}

var modeOrder = map[Mode][]string{
	ModeNative:  {NativeMux, SSHRaw, SSH, MoshMux},
	ModeRoam:    {MoshMux, SSH, NativeMux, SSHRaw},
	ModePersist: {NativeMux, MoshMux, SSHRaw, SSH},
}

// Rank applies, in order: pin, hard requirements, mode, flakiness, tier number.
func Rank(caps Caps, link Link, prefs Prefs) (Result, error) {
	if prefs.Mode == "" {
		prefs.Mode = ModeAuto
	}
	if _, ok := modeOrder[prefs.Mode]; !ok && prefs.Mode != ModeAuto {
		return Result{}, fmt.Errorf("mode %q is not one of auto, native, roam, persist", prefs.Mode)
	}
	if prefs.Pin != "" {
		if _, ok := TierNamed(prefs.Pin); !ok {
			return Result{}, fmt.Errorf("pin %q is not a tier", prefs.Pin)
		}
	}

	result := Result{}
	result.Offline = caps.RegistryOffline || (caps.RemoteKnown && !caps.RemoteOK)
	available := map[string]Tier{}
	for _, tier := range Tiers {
		if reason := unavailable(tier, caps, result.Offline); reason != "" {
			result.Filtered = append(result.Filtered, Filtered{Tier: tier, Reason: reason})
			result.Reasons = append(result.Reasons, tier.Name+" filtered: "+reason)
			continue
		}
		available[tier.Name] = tier
	}
	if result.Offline {
		result.Reasons = append(result.Reasons, "host offline: "+offlineReason(caps)+"; baseline only, marked stale")
	}

	if prefs.Pin != "" {
		if _, ok := available[prefs.Pin]; !ok {
			return result, &PinError{Tier: prefs.Pin, Reason: filteredReason(result.Filtered, prefs.Pin)}
		}
	}

	result.Flaky, result.Reasons = flaky(link, prefs, result.Reasons)
	order := modeOrder[prefs.Mode]
	switch {
	case prefs.Mode == ModeAuto && result.Flaky:
		order = []string{MoshMux, NativeMux, SSHRaw, SSH}
		result.Reasons = append(result.Reasons, "auto: link flaky, prefer "+MoshMux)
	case prefs.Mode == ModeAuto:
		order = []string{NativeMux, MoshMux, SSHRaw, SSH}
		result.Reasons = append(result.Reasons, "auto: link steady, prefer "+NativeMux)
	default:
		result.Reasons = append(result.Reasons, "mode "+string(prefs.Mode))
	}

	if prefs.Pin != "" {
		result.Ordered = append(result.Ordered, available[prefs.Pin])
		result.Reasons = append(result.Reasons, "pinned to "+prefs.Pin)
	}
	for _, name := range order {
		tier, ok := available[name]
		if !ok || name == prefs.Pin {
			continue
		}
		result.Ordered = append(result.Ordered, tier)
	}
	if len(result.Ordered) == 0 {
		return result, fmt.Errorf("no tier available: %s", strings.Join(result.Reasons, "; "))
	}
	return result, nil
}

func unavailable(tier Tier, caps Caps, offline bool) string {
	var missing []string
	if offline && tier.Name != SSH {
		missing = append(missing, "host offline: "+offlineReason(caps))
	}
	switch tier.Name {
	case NativeMux:
		if caps.NativeUnfit != "" {
			missing = append(missing, caps.NativeUnfit)
		}
		if !caps.LocalWezterm {
			missing = append(missing, "local wezterm absent")
		}
		if !caps.RemoteKnown {
			missing = append(missing, "remote not probed")
		} else if !offline && !caps.RemoteMuxServer {
			missing = append(missing, "remote wezterm-mux-server absent")
		}
		if caps.NativeRef == "" {
			missing = append(missing, "no native ref (--native)")
		}
	case MoshMux:
		if caps.MoshUnfit != "" {
			missing = append(missing, caps.MoshUnfit)
		}
		if !caps.LocalMosh {
			missing = append(missing, "local mosh absent")
		}
		if !caps.RemoteKnown {
			missing = append(missing, "remote not probed")
		} else if !offline {
			if !caps.RemoteMoshServer {
				missing = append(missing, "remote mosh-server absent")
			}
			if !caps.RemoteZmx && !caps.RemoteTmux {
				missing = append(missing, "remote zmx and tmux absent")
			}
		}
	case SSHRaw:
		if caps.NativeUnfit != "" {
			missing = append(missing, caps.NativeUnfit)
		}
		if !caps.LocalSSH {
			missing = append(missing, "local ssh absent")
		}
		if caps.NativeRawRef == "" {
			missing = append(missing, "no raw ssh domain declared (--native-raw)")
		}
	case SSH:
		if !caps.LocalSSH {
			missing = append(missing, "local ssh absent")
		}
	}
	return strings.Join(missing, ", ")
}

func offlineReason(caps Caps) string {
	switch {
	case caps.RegistryOffline:
		return "tailscale reports the peer offline"
	case caps.RemoteReason != "":
		return caps.RemoteReason
	default:
		return "remote probe failed"
	}
}

func filteredReason(filtered []Filtered, name string) string {
	for _, entry := range filtered {
		if entry.Tier.Name == name {
			return entry.Reason
		}
	}
	return "not available"
}

func flaky(link Link, prefs Prefs, reasons []string) (bool, []string) {
	if !link.Known {
		return false, append(reasons, "link: no fresh sample")
	}
	var causes []string
	if link.Relayed {
		causes = append(causes, "relayed via DERP")
	}
	if link.RTTMs > prefs.FlakyRTTMs {
		causes = append(causes, fmt.Sprintf("rtt %.0fms > %.0fms", link.RTTMs, prefs.FlakyRTTMs))
	}
	if link.Loss > prefs.FlakyLoss {
		causes = append(causes, fmt.Sprintf("loss %.0f%% > %.0f%%", link.Loss*100, prefs.FlakyLoss*100))
	}
	if len(causes) > 0 {
		return true, append(reasons, "link flaky: "+strings.Join(causes, ", "))
	}
	return false, append(reasons, fmt.Sprintf("link steady: direct, rtt %.0fms, loss %.0f%%", link.RTTMs, link.Loss*100))
}
