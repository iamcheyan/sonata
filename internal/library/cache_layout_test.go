package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTripEmptyLibrary(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(t.TempDir(), "library.json")
	lib := &Library{Version: CacheVersion, Root: root, Songs: []Song{}}
	if err := SaveCache(cache, lib); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCache(cache)
	if err != nil {
		t.Fatalf("empty library cache should load: %v", err)
	}
	if !Fresh(got, root) {
		t.Fatal("empty cache should be fresh")
	}
}

func TestFreshNormalizesRootAndExtensionCase(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "track.MP3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Songs) != 1 {
		t.Fatalf("songs = %d, want 1", len(lib.Songs))
	}
	if !Fresh(lib, filepath.Join(root, ".")) {
		t.Fatal("clean-equivalent root should remain fresh")
	}
}
