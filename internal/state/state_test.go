package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != Version {
		t.Errorf("version = %d", cfg.Version)
	}
	if cfg.Volume != 70 || cfg.AutoScreenOff != "1m" {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Errorf("config.json not created: %v", err)
	}
}

func TestFavoritesToggle(t *testing.T) {
	f := Favorites{Songs: []string{}}
	if !f.ToggleFavorite("s_a") {
		t.Fatal("first toggle should add")
	}
	if !f.Has("s_a") {
		t.Fatal("s_a should be favorited")
	}
	if f.ToggleFavorite("s_a") {
		t.Fatal("second toggle should remove")
	}
	if f.Has("s_a") {
		t.Fatal("s_a should be removed")
	}
	if got := len(f.Songs); got != 0 {
		t.Fatalf("songs = %d", got)
	}
}

func TestPlaybackRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := Playback{
		Status: StatusPlaying, SongID: "s_a", Path: "/m/a.mp3",
		Title: "A", Artist: "X", Album: "Y", Position: 12.5, Duration: 200,
		Mode: "repeat-all", Volume: 55, MpvPID: 1234,
	}
	if err := s.SavePlayback(in); err != nil {
		t.Fatal(err)
	}
	out, err := s.LoadPlayback()
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != in.Status || out.SongID != in.SongID || out.Position != in.Position || out.Volume != in.Volume {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	if out.Version != Version {
		t.Errorf("version = %d", out.Version)
	}
	if out.UpdatedAt.IsZero() {
		t.Error("updatedAt should be stamped")
	}
}

func TestPlaybackMissingFileIsStopped(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pb, err := s.LoadPlayback()
	if err != nil {
		t.Fatal(err)
	}
	if pb.Status != StatusStopped {
		t.Fatalf("status = %q, want stopped", pb.Status)
	}
}

func TestNewerVersionIsRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"version": 99, "status":"playing"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadPlayback(); err != ErrNewerFormat {
		t.Fatalf("err = %v, want ErrNewerFormat", err)
	}
}

func TestCorruptFileIsBackedUp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "favorites.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadFavorites(); err == nil {
		t.Fatal("expected a parse error")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "favorites.json.corrupt-*"))
	if len(matches) != 1 {
		t.Fatalf("corrupt backup count = %d", len(matches))
	}
}

func TestHistoryTrimAndRecord(t *testing.T) {
	h := History{}
	now := time.Now()
	for i := 0; i < MaxHistory+25; i++ {
		h.RecordPlay("s_"+string(rune('a'+i%26))+time.Duration(i).String(), now)
	}
	if len(h.RecentlyPlayed) > MaxHistory {
		t.Fatalf("history not trimmed: %d", len(h.RecentlyPlayed))
	}
	h2 := History{}
	h2.RecordPlay("s_a", now)
	h2.RecordPlay("s_b", now)
	h2.RecordPlay("s_a", now.Add(time.Second))
	if h2.RecentlyPlayed[0].SongID != "s_a" {
		t.Fatal("most recent play should be first")
	}
	if len(h2.RecentlyPlayed) != 2 {
		t.Fatalf("duplicate entries: %d", len(h2.RecentlyPlayed))
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			t.Errorf("leftover file: %s", e.Name())
		}
	}
}
