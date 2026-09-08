package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/roshbhatia/go-utils/paths"
)

// Dir is where host records live: $XDG_STATE_HOME/tether/hosts.
func Dir() string {
	return filepath.Join(paths.StateHome(), "tether", "hosts")
}

// Path is the record file for one host.
func Path(host string) string {
	return filepath.Join(Dir(), safeName(host)+".json")
}

func safeName(host string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, host)
}

// Read loads the record. A missing file or a version mismatch is a miss, not
// an error.
func Read(host string) (Host, bool, error) {
	raw, err := os.ReadFile(Path(host))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Host{}, false, nil
		}
		return Host{}, false, fmt.Errorf("read host record: %w", err)
	}
	var record Host
	if err := json.Unmarshal(raw, &record); err != nil {
		return Host{}, false, fmt.Errorf("decode %s: %w", Path(host), err)
	}
	if record.Version != HostVersion {
		return Host{}, false, nil
	}
	if record.Remote.Versions == nil {
		record.Remote.Versions = map[string]string{}
	}
	return record, true, nil
}

// Write stores the record with an atomic rename, so a reader never sees a
// partial file.
func Write(record Host) error {
	path := Path(record.Host)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create host record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode host record: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp host record: %w", err)
	}
	tempName := temp.Name()
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return fmt.Errorf("write temp host record: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("close temp host record: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("replace host record: %w", err)
	}
	return nil
}

// List returns every readable record, sorted by host. Records of another
// version are skipped, like Read.
func List() ([]Host, error) {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list host records: %w", err)
	}
	var records []Host
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		record, ok, err := Read(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Host < records[j].Host })
	return records, nil
}
