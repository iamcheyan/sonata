package service

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iamcheyan/sonata/internal/library"
	"github.com/iamcheyan/sonata/internal/mpv"
	"github.com/iamcheyan/sonata/internal/state"
)

// writeWAV writes a small mono 8kHz sine-free silence WAV. Real audio content
// does not matter: mpv only has to accept the container.
func writeWAV(t *testing.T, path string, seconds int) {
	t.Helper()
	const rate = 8000
	samples := rate * seconds
	data := make([]byte, samples*2)
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(data)))
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], 1) // mono
	binary.LittleEndian.PutUint32(hdr[24:], rate)
	binary.LittleEndian.PutUint32(hdr[28:], rate*2)
	binary.LittleEndian.PutUint16(hdr[32:], 2)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(hdr, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := state.DefaultConfig()
	lib := &library.Library{Version: 1, Root: dir}
	return New(store, cfg, lib), dir
}

// TestPlaybackIntegration drives a real mpv instance with --ao=null. It is
// skipped when mpv is not installed so the unit suite stays portable.
func TestPlaybackIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if !mpv.Available() {
		t.Skip("mpv not installed")
	}
	svc, dir := newService(t)
	sock := filepath.Join(dir, "mpv.sock")
	if err := svc.Connect(sock, filepath.Join(dir, "mpv.log"), []string{"--ao=null", "--volume=0"}); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	wav := filepath.Join(dir, "tone.wav")
	writeWAV(t, wav, 3)
	songs := []library.Song{{ID: "s_test", Path: wav, Title: "Tone", Artist: "Test", Album: "Test", Duration: 3 * time.Second}}
	if err := svc.PlaySongs(songs, 0); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var pb state.Playback
	for time.Now().Before(deadline) {
		pb = svc.Playback()
		if pb.Status == state.StatusPlaying && pb.Position > 0.2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pb.Status != state.StatusPlaying {
		t.Fatalf("status = %q", pb.Status)
	}
	if pb.Position <= 0 {
		t.Fatalf("position did not advance: %+v", pb)
	}
	if pb.Title != "Tone" || pb.SongID != "s_test" {
		t.Fatalf("state not updated: %+v", pb)
	}

	// Pause must be reflected in state.
	if err := svc.Toggle(); err != nil {
		t.Fatal(err)
	}
	if got := svc.Playback().Status; got != state.StatusPaused {
		t.Fatalf("status after toggle = %q", got)
	}

	// State file must contain the same track.
	stored, err := svc.store.LoadPlayback()
	if err != nil {
		t.Fatal(err)
	}
	if stored.SongID != "s_test" {
		t.Fatalf("persisted song = %q", stored.SongID)
	}

	if err := svc.SetVolume(35); err != nil {
		t.Fatal(err)
	}
	if svc.Volume() != 35 {
		t.Fatalf("volume = %d", svc.Volume())
	}
}

func TestModeAndQueue(t *testing.T) {
	svc, _ := newService(t)
	songs := []library.Song{{ID: "s_a", Title: "A"}, {ID: "s_b", Title: "B"}}
	svc.pl.Replace(songs)
	if svc.Mode() != "order" {
		t.Fatalf("mode = %q", svc.Mode())
	}
	if got := svc.CycleMode(); got != "repeat-one" {
		t.Fatalf("cycled to %q", got)
	}
	if len(svc.Queue()) != 2 {
		t.Fatalf("queue = %d", len(svc.Queue()))
	}
}

func TestPlaySongsWithoutMpvFails(t *testing.T) {
	svc, _ := newService(t)
	err := svc.PlaySongs([]library.Song{{ID: "s_a", Path: "/nope.mp3"}}, 0)
	if err == nil {
		t.Fatal("expected an error when mpv is not connected")
	}
}
