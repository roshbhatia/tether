package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoresAreIndependent(t *testing.T) {
	t.Parallel()
	first := Store{Directory: t.TempDir()}
	second := Store{Directory: t.TempDir()}
	record := Host{Version: HostVersion, Host: "same-host", Remote: Remote{OK: true}}
	if err := first.Write(record); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := second.Read(record.Host); err != nil || ok {
		t.Fatalf("other store: found=%v error=%v", ok, err)
	}
	if got, ok, err := first.Read(record.Host); err != nil || !ok || !got.Remote.OK {
		t.Fatalf("written record: %+v %v %v", got, ok, err)
	}
	entries, err := os.ReadDir(first.Directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(first.Path(record.Host)) {
		t.Fatalf("store entries: %v %v", entries, err)
	}
}
