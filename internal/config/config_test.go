package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWithoutFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TETHER_CONFIG", "")

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Mode != ModeAuto || loaded.Flaky.RTTMs != 60 || loaded.Flaky.Loss != 0 {
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
	if loaded.Mode != ModeRoam || loaded.Flaky.RTTMs != 80 || loaded.Flaky.Loss != 0.1 {
		t.Fatalf("unexpected config: %+v", loaded)
	}
	if loaded.PinFor("arrakis") != "mosh-mux" || loaded.ModeFor("arrakis") != ModePersist || loaded.ModeFor("other") != ModeRoam {
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
