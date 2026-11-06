package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/roshbhatia/tether/internal/rank"
)

func TestSchemaMatchesRankingVocabulary(t *testing.T) {
	t.Parallel()
	type property struct {
		Enum []string `json:"enum"`
	}
	var schema struct {
		Properties map[string]property `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]property `json:"properties"`
		} `json:"$defs"`
	}
	data, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]string{
		"default mode": schema.Properties["mode"].Enum,
		"host mode":    schema.Defs["Host"].Properties["mode"].Enum,
	} {
		if !reflect.DeepEqual(got, rank.ModeNames()) {
			t.Fatalf("%s: %v", name, got)
		}
	}
	if got := schema.Defs["Host"].Properties["pin"].Enum; !reflect.DeepEqual(got, rank.TierNames()) {
		t.Fatalf("pins: %v", got)
	}
}

func TestLoadDefaultsWithoutFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TETHER_CONFIG", "")

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Mode != rank.ModeAuto || loaded.Flaky.RTTMs != 60 || loaded.Flaky.Loss != 0 {
		t.Fatalf("unexpected defaults: %+v", loaded)
	}
}

func TestLoadReadsJSONPinsAndModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"mode":"roam","flaky":{"rtt_ms":80,"loss":0.1},"hosts":{"arrakis":{"pin":"mosh-mux","mode":"persist"}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TETHER_CONFIG", path)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Mode != rank.ModeRoam || loaded.Flaky.RTTMs != 80 || loaded.Flaky.Loss != 0.1 {
		t.Fatalf("unexpected config: %+v", loaded)
	}
	if loaded.PinFor("arrakis") != "mosh-mux" || loaded.ModeFor("arrakis") != rank.ModePersist || loaded.ModeFor("other") != rank.ModeRoam {
		t.Fatalf("unexpected host lookup: %+v", loaded.Hosts)
	}
}

func TestLoadRejectsUnknownMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mode":"fast"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TETHER_CONFIG", path)

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an unknown mode")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"modes":"auto"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TETHER_CONFIG", path)

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an unknown field")
	}
}

func TestEnvironmentOverridesFlakyThreshold(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TETHER_CONFIG", "")
	t.Setenv("TETHER_FLAKY_RTT_MS", "120")

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Flaky.RTTMs != 120 {
		t.Fatalf("rtt_ms = %v, want 120", loaded.Flaky.RTTMs)
	}
}

func TestLoadReadsHostUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"hosts":{"vault":{"user":"ops"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TETHER_CONFIG", path)
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UserFor("vault") != "ops" || loaded.UserFor("other") != "" {
		t.Fatalf("loaded: %+v", loaded)
	}
	if loaded.Mode != rank.ModeAuto || loaded.Flaky.RTTMs != 60 {
		t.Fatalf("defaults lost: %+v", loaded)
	}
}
