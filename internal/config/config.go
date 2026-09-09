// Package config loads the tether configuration: pins, mode, flaky thresholds.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	goconfig "github.com/roshbhatia/go-utils/config"
	"github.com/roshbhatia/go-utils/paths"
)

const (
	Name      = "tether"
	EnvPrefix = "TETHER"
	Version   = "tether.config/v1"
)

// Mode orders the surviving tiers.
type Mode string

const (
	ModeAuto    Mode = "auto"
	ModeNative  Mode = "native"
	ModeRoam    Mode = "roam"
	ModePersist Mode = "persist"
)

var modes = map[Mode]struct{}{ModeAuto: {}, ModeNative: {}, ModeRoam: {}, ModePersist: {}}

// Valid reports whether the mode is one tether knows.
func (mode Mode) Valid() bool {
	_, ok := modes[mode]
	return ok
}

// Flaky holds the link thresholds that make auto mode prefer the roaming tier.
type Flaky struct {
	RTTMs float64 `json:"rtt_ms" yaml:"rtt_ms" jsonschema:"description=Round-trip time in milliseconds above which the link is flaky"`
	Loss  float64 `json:"loss" yaml:"loss" jsonschema:"description=Loss fraction (0-1) above which the link is flaky"`
}

// Host holds per-host overrides.
type Host struct {
	Pin  string `json:"pin,omitempty" yaml:"pin" jsonschema:"enum=native-mux,enum=mosh-mux,enum=ssh-raw,enum=ssh,description=Tier that always wins; an unavailable pin is an error"`
	Mode Mode   `json:"mode,omitempty" yaml:"mode" jsonschema:"enum=auto,enum=native,enum=roam,enum=persist,description=Mode for this host; overrides the top-level mode"`
	User string `json:"user,omitempty" yaml:"user" jsonschema:"description=Remote user; prepended as user@ to the ssh target"`
}

// Defaults are the values connect uses when a flag is absent.
type Defaults struct {
	Session string `json:"session,omitempty" yaml:"session" jsonschema:"description=Session connect attaches when --session is absent; empty means a login shell"`
}

// Config is the whole configuration file.
type Config struct {
	Mode     Mode            `json:"mode" yaml:"mode" jsonschema:"enum=auto,enum=native,enum=roam,enum=persist,description=Default ordering mode"`
	Flaky    Flaky           `json:"flaky" yaml:"flaky"`
	Defaults Defaults        `json:"defaults" yaml:"defaults"`
	Hosts    map[string]Host `json:"hosts,omitempty" yaml:"hosts"`
}

// Default is the configuration with no file present.
func Default() Config {
	return Config{Mode: ModeAuto, Flaky: Flaky{RTTMs: 60, Loss: 0}}
}

// Path is the configuration file location: $TETHER_CONFIG, else
// $XDG_CONFIG_HOME/tether/config.json.
func Path() string {
	if override := os.Getenv(EnvPrefix + "_CONFIG"); override != "" {
		return override
	}
	return filepath.Join(paths.ConfigHome(), Name, "config.json")
}

// Load reads the configuration file over the defaults and validates it.
func Load() (Config, error) {
	loaded, err := goconfig.Load(Default(), goconfig.Options{Name: Name, EnvPrefix: EnvPrefix, Path: Path()})
	if err != nil {
		return loaded, err
	}
	return loaded, loaded.Validate()
}

// Validate rejects an unknown mode.
func (config Config) Validate() error {
	if !config.Mode.Valid() {
		return fmt.Errorf("mode %q is not one of auto, native, roam, persist", config.Mode)
	}
	for name, host := range config.Hosts {
		if host.Mode != "" && !host.Mode.Valid() {
			return fmt.Errorf("hosts.%s.mode %q is not one of auto, native, roam, persist", name, host.Mode)
		}
	}
	return nil
}

// ModeFor returns the host's mode, else the default.
func (config Config) ModeFor(host string) Mode {
	if entry, ok := config.Hosts[host]; ok && entry.Mode != "" {
		return entry.Mode
	}
	return config.Mode
}

// PinFor returns the host's pinned tier, or "".
func (config Config) PinFor(host string) string {
	return config.Hosts[host].Pin
}

// UserFor returns the host's configured remote user, or "".
func (config Config) UserFor(host string) string {
	return config.Hosts[host].User
}

// Schema returns the JSON Schema for the configuration file.
func Schema() ([]byte, error) {
	return goconfig.Schema[Config](Version)
}
