// Package service wires the queue, the state store and mpv together. It is the
// only place that knows all three, which keeps the UI free of playback details.
package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/iamcheyan/sonata/internal/library"
	"github.com/iamcheyan/sonata/internal/mpv"
	"github.com/iamcheyan/sonata/internal/player"
	"github.com/iamcheyan/sonata/internal/state"
)

// Service owns playback for one Sonata process.
type Service struct {
	store *state.Store
	cfg   state.Config
	lib   *library.Library
	pl    *player.Player

	mu   sync.Mutex
	cl   *mpv.Client
	pb   state.Playback
	sock string
	log  string

	saveMu   sync.Mutex
	lastSave time.Time
	stopPoll chan struct{}
}

// New builds a service. mpv is connected lazily by Connect.
func New(store *state.Store, cfg state.Config, lib *library.Library) *Service {
	s := &Service{store: store, cfg: cfg, lib: lib}
	s.pb = statePlayback(store)
	s.pl = player.New(nil, player.ParseMode(s.pb.Mode))
	// A brand new state file has no volume yet: start from the configured one.
	if s.pb.Volume == 0 && s.pb.SongID == "" {
		s.pb.Volume = cfg.Volume
	}
	return s
}

func statePlayback(store *state.Store) state.Playback {
	pb, err := store.LoadPlayback()
	if err != nil {
		pb = state.Playback{Status: state.StatusStopped, Mode: "order"}
	}
	return pb
}

// Connect attaches to a running mpv or starts a detached one. When mpv is
// missing the service still works for browsing; Available reports false.
func (s *Service) Connect(socket, logPath string, extraArgs []string) error {
	s.mu.Lock()
	s.sock = socket
	s.log = logPath
	s.mu.Unlock()

	cl, proc, err := mpv.Ensure(mpv.Options{
		Socket:    socket,
		Volume:    s.pb.Volume,
		LogPath:   logPath,
		ExtraArgs: extraArgs,
	})
	if err != nil {
		s.pb.Status = state.StatusDegraded
		return err
	}
	s.mu.Lock()
	s.cl = cl
	if proc != nil {
		s.pb.MpvPID = proc.Pid
	}
	s.pb.Socket = socket
	s.mu.Unlock()

	_ = cl.Observe(1, "time-pos")
	go s.eventLoop(cl)
	go s.pollLoop(cl)
	return nil
}

// Available reports whether mpv is connected.
func (s *Service) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cl != nil
}

// Library returns the loaded index.
func (s *Service) Library() *library.Library { return s.lib }

// SetLibrary swaps the index (after a rescan).
func (s *Service) SetLibrary(lib *library.Library) { s.lib = lib }

// PlaySongs replaces the queue and starts at start.
func (s *Service) PlaySongs(songs []library.Song, start int) error {
	if len(songs) == 0 {
		return errors.New("nothing to play")
	}
	s.pl.Replace(songs)
	song, err := s.pl.Select(start)
	if err != nil {
		return err
	}
	return s.playCurrent(song, 0)
}

// Toggle pauses or resumes.
func (s *Service) Toggle() error {
	s.mu.Lock()
	cl, pb := s.cl, s.pb
	s.mu.Unlock()
	if cl == nil {
		return mpv.ErrNotInstalled
	}
	pause := pb.Status == state.StatusPlaying
	if err := cl.Pause(pause); err != nil {
		return err
	}
	if pause {
		s.setStatus(state.StatusPaused)
	} else {
		s.setStatus(state.StatusPlaying)
	}
	return nil
}

// Next advances according to the playback mode.
func (s *Service) Next() error {
	song, finished, err := s.pl.Next()
	if err != nil {
		return err
	}
	if finished {
		return s.stopPlayback()
	}
	return s.playCurrent(song, 0)
}

// Prev goes back one track.
func (s *Service) Prev() error {
	song, err := s.pl.Prev()
	if err != nil {
		return err
	}
	return s.playCurrent(song, 0)
}

// Seek moves relative to the current position.
func (s *Service) Seek(delta float64) error {
	s.mu.Lock()
	cl, pb := s.cl, s.pb
	s.mu.Unlock()
	if cl == nil {
		return mpv.ErrNotInstalled
	}
	target := pb.Position + delta
	if target < 0 {
		target = 0
	}
	if pb.Duration > 0 && target > pb.Duration {
		target = pb.Duration
	}
	if err := cl.Seek(target); err != nil {
		return err
	}
	s.mu.Lock()
	s.pb.Position = target
	s.mu.Unlock()
	s.persist(true)
	return nil
}

// Mode reports the playback mode.
func (s *Service) Mode() player.Mode { return s.pl.Mode() }

// CycleMode advances the mode and stores it.
func (s *Service) CycleMode() player.Mode {
	m := s.pl.CycleMode()
	s.mu.Lock()
	s.pb.Mode = string(m)
	s.mu.Unlock()
	s.persist(true)
	return m
}

// Volume reports the current volume.
func (s *Service) Volume() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pb.Volume
}

// SetVolume clamps and applies the volume.
func (s *Service) SetVolume(v int) error {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	s.mu.Lock()
	cl := s.cl
	s.pb.Volume = v
	s.mu.Unlock()
	if cl != nil {
		if err := cl.SetProperty("volume", v); err != nil {
			return err
		}
	}
	s.persist(true)
	return nil
}

// Playback returns a snapshot of the playback state.
func (s *Service) Playback() state.Playback {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pb
}

// Queue returns the current queue.
func (s *Service) Queue() []library.Song { return s.pl.Queue() }

// AddToQueue appends songs to the end of the queue.
func (s *Service) AddToQueue(songs []library.Song) error {
	s.pl.Append(songs)
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl != nil {
		for _, song := range songs {
			_, _ = cl.Command("playlist-add", song.Path)
		}
	}
	return nil
}

// MoveQueue reorders the queue: from -> to.
func (s *Service) MoveQueue(from, to int) error {
	s.pl.Move(from, to)
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl != nil {
		return cl.PlaylistMove(from, to)
	}
	return nil
}

// RemoveFromQueue removes one entry.
func (s *Service) RemoveFromQueue(index int) error {
	s.pl.Remove(index)
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl != nil {
		return cl.PlaylistDelete(index)
	}
	return nil
}

// ClearQueue empties the queue.
func (s *Service) ClearQueue() error {
	s.pl.Clear()
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl != nil {
		return cl.PlaylistClear()
	}
	return nil
}

// Stop stops playback and quits mpv.
func (s *Service) Stop() error {
	if err := s.stopPlayback(); err != nil {
		return err
	}
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl != nil {
		_ = cl.Quit()
		_ = cl.Close()
	}
	return nil
}

// Close releases resources without stopping background playback.
func (s *Service) Close() {
	s.persist(true)
	s.mu.Lock()
	cl := s.cl
	stop := s.stopPoll
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	if cl != nil {
		_ = cl.Close() // mpv keeps playing: this is what "background" means
	}
}

// Resume restores the last track without starting playback when the user had
// left it paused/stopped.
func (s *Service) Resume() error {
	s.mu.Lock()
	pb := s.pb
	cl := s.cl
	s.mu.Unlock()
	if cl == nil || pb.Path == "" {
		return nil
	}
	if s.pl.Len() == 0 && s.lib != nil {
		if song, ok := s.lib.Song(pb.SongID); ok {
			s.pl.Replace([]library.Song{song})
		}
	}
	if err := cl.LoadFile(pb.Path); err != nil {
		return err
	}
	if pb.Position > 0 {
		_ = cl.Seek(pb.Position)
	}
	if pb.Status != state.StatusPlaying {
		_ = cl.Pause(true)
	}
	return nil
}

func (s *Service) playCurrent(song library.Song, position float64) error {
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl == nil {
		return mpv.ErrNotInstalled
	}
	if err := cl.LoadFile(song.Path); err != nil {
		return err
	}
	if position > 0 {
		_ = cl.Seek(position)
	}
	s.mu.Lock()
	s.pb.SongID = song.ID
	s.pb.Path = song.Path
	s.pb.Title = song.Title
	s.pb.Artist = song.Artist
	s.pb.Album = song.Album
	s.pb.Duration = song.Duration.Seconds()
	s.pb.Position = position
	s.pb.Status = state.StatusPlaying
	s.pb.Mode = string(s.pl.Mode())
	s.pb.Socket = s.sock
	s.mu.Unlock()
	s.persist(true)
	return nil
}

func (s *Service) stopPlayback() error {
	s.mu.Lock()
	cl := s.cl
	s.mu.Unlock()
	if cl != nil {
		_ = cl.SetProperty("pause", true)
	}
	s.setStatus(state.StatusStopped)
	return nil
}

func (s *Service) setStatus(st state.Status) {
	s.mu.Lock()
	s.pb.Status = st
	s.mu.Unlock()
	s.persist(true)
}

// persist writes state.json, throttled unless force is set.
func (s *Service) persist(force bool) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if !force && time.Since(s.lastSave) < 500*time.Millisecond {
		return
	}
	s.lastSave = time.Now()
	pb := s.Playback()
	if err := s.store.SavePlayback(pb); err != nil {
		// Persisting is best-effort: playback must not stop because the SD
		// card is full or read-only.
		return
	}
}

func (s *Service) eventLoop(cl *mpv.Client) {
	for ev := range cl.Events() {
		switch ev.Event {
		case "file-loaded":
			s.mu.Lock()
			s.pb.Status = state.StatusPlaying
			s.mu.Unlock()
			s.persist(true)
		case "end-file":
			switch ev.Reason {
			case "eof":
				if err := s.Next(); err != nil {
					s.setStatus(state.StatusStopped)
				}
			case "error":
				s.mu.Lock()
				s.pb.Status = state.StatusDegraded
				s.mu.Unlock()
				s.persist(true)
			}
		}
	}
}

func (s *Service) pollLoop(cl *mpv.Client) {
	s.mu.Lock()
	stop := make(chan struct{})
	s.stopPoll = stop
	s.mu.Unlock()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if v, err := cl.GetProperty("time-pos"); err == nil {
				if f, ok := v.(float64); ok {
					s.mu.Lock()
					s.pb.Position = f
					s.mu.Unlock()
					s.persist(false)
				}
			}
			if v, err := cl.GetProperty("duration"); err == nil {
				if f, ok := v.(float64); ok && f > 0 {
					s.mu.Lock()
					s.pb.Duration = f
					s.mu.Unlock()
				}
			}
			if v, err := cl.GetProperty("pause"); err == nil {
				paused, _ := v.(bool)
				s.mu.Lock()
				if paused && s.pb.Status == state.StatusPlaying {
					s.pb.Status = state.StatusPaused
				} else if !paused && s.pb.Status == state.StatusPaused {
					s.pb.Status = state.StatusPlaying
				}
				s.mu.Unlock()
			}
		}
	}
}

// SocketFor returns the socket path for a data directory.
func SocketFor(dataDir string) string { return mpv.SocketPath(dataDir) }

// LogPath returns the mpv log location for a data directory.
func LogPath(dataDir string) string {
	base := filepath.Dir(filepath.Dir(filepath.Clean(dataDir)))
	return filepath.Join(base, "logs", "sonata", "mpv.log")
}

// FormatPosition renders a position for CLI output.
func FormatPosition(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	t := int(sec)
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}
