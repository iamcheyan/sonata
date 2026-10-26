package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFixture creates an untagged audio tree: the scanner must fall back to
// directory and file names, which is the common case on a handheld SD card.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"周杰伦/七里香/01 - 七里香.mp3":             "",
		"周杰伦/七里香/02 我的地盘.flac":             "",
		"周杰伦/依然范特西/01 夜的第七章.mp3":           "",
		"Aimer/walpurgis/01 walpurgis.mp3": "",
		"loose-track.mp3":                  "",
		"notes.txt":                        "not audio",
	}
	for rel := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(files[rel]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestScanFallbacks(t *testing.T) {
	root := writeFixture(t)
	lib, err := Scan(root, ScanOptions{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lib.Songs); got != 5 {
		t.Fatalf("songs = %d, want 5", got)
	}
	byTitle := map[string]Song{}
	for _, s := range lib.Songs {
		byTitle[s.Title] = s
	}
	song, ok := byTitle["七里香"]
	if !ok {
		t.Fatalf("missing 七里香, got %v", keys(byTitle))
	}
	if song.Artist != "周杰伦" {
		t.Errorf("artist = %q, want 周杰伦", song.Artist)
	}
	if song.Album != "七里香" {
		t.Errorf("album = %q, want 七里香", song.Album)
	}
	if song.Track != 1 {
		t.Errorf("track = %d, want 1", song.Track)
	}

	loose := byTitle["loose-track"]
	if loose.Artist != "Unknown Artist" || loose.Album != "Unknown Album" {
		t.Errorf("loose fallback = %q/%q", loose.Artist, loose.Album)
	}
	if loose.DisplayDuration() != "--:--" {
		t.Errorf("unknown duration should render --:--, got %q", loose.DisplayDuration())
	}
}

func TestScanGroups(t *testing.T) {
	root := writeFixture(t)
	lib, err := Scan(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Artists) != 3 { // 周杰伦, Aimer, Unknown Artist
		t.Fatalf("artists = %d (%v)", len(lib.Artists), artistNames(lib))
	}
	if len(lib.Albums) != 4 { // 七里香, 依然范特西, walpurgis, Unknown Album
		t.Fatalf("albums = %d", len(lib.Albums))
	}
	// Album songs must be ordered by track number.
	songs := lib.AlbumSongs("七里香", "周杰伦")
	if len(songs) != 2 || songs[0].Track != 1 || songs[1].Track != 2 {
		t.Fatalf("album order wrong: %+v", songs)
	}
}

func TestSongIDStable(t *testing.T) {
	a := SongID("周杰伦/七里香/01 七里香.mp3")
	b := SongID("周杰伦/七里香/01 七里香.mp3")
	if a != b {
		t.Fatalf("unstable id: %s != %s", a, b)
	}
	if a == SongID("周杰伦/七里香/02 我的地盘.mp3") {
		t.Fatal("different paths must produce different ids")
	}
	if len(a) < 4 || a[:2] != "s_" {
		t.Fatalf("id prefix wrong: %s", a)
	}
}

func TestFallbackTitle(t *testing.T) {
	cases := []struct {
		in    string
		title string
		track int
	}{
		{"01 - Song.mp3", "Song", 1},
		{"02 Song.flac", "Song", 2},
		{"3. Track.m4a", "Track", 3},
		{"Track.mp3", "Track", 0},
		{"123 - Long.mp3", "Long", 123},
		{"01 - 七里香.mp3", "七里香", 1},
	}
	for _, c := range cases {
		title, track := FallbackTitle(c.in)
		if title != c.title || track != c.track {
			t.Errorf("FallbackTitle(%q) = (%q,%d), want (%q,%d)", c.in, title, track, c.title, c.track)
		}
	}
}

func TestCacheRoundTrip(t *testing.T) {
	root := writeFixture(t)
	cache := filepath.Join(t.TempDir(), "cache", "library.json")
	lib, err := Scan(root, ScanOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCache(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Songs) != len(lib.Songs) {
		t.Fatalf("cached songs = %d, want %d", len(loaded.Songs), len(lib.Songs))
	}
	if !Fresh(loaded, root) {
		t.Error("cache should be fresh right after scanning")
	}
	// Adding a file invalidates the fingerprint.
	if err := os.WriteFile(filepath.Join(root, "new.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Fresh(loaded, root) {
		t.Error("cache should be stale after adding a file")
	}
}

func TestLoadCacheRejectsNewerVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "library.json")
	if err := os.WriteFile(p, []byte(`{"version": 99, "songs":[{"id":"s_1"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(p); err != ErrStaleCache {
		t.Fatalf("err = %v, want ErrStaleCache", err)
	}
}

func TestScanMissingDir(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope"), ScanOptions{}); err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestBackoffUntil(t *testing.T) {
	now := time.Unix(1000, 0)
	if !BackoffUntil(0, now).Equal(now) {
		t.Error("zero attempts should not back off")
	}
	if d := BackoffUntil(1, now).Sub(now); d != 30*time.Second {
		t.Errorf("attempt 1 backoff = %v", d)
	}
	if d := BackoffUntil(5, now).Sub(now); d != 10*time.Minute {
		t.Errorf("attempt 5 backoff = %v", d)
	}
}

func keys(m map[string]Song) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func artistNames(l *Library) []string {
	out := make([]string, 0, len(l.Artists))
	for _, a := range l.Artists {
		out = append(out, a.Name)
	}
	return out
}
