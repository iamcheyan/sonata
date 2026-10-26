// Package state persists user data as versioned JSON files written atomically.
//
// Files are deliberately plain JSON: on a handheld the user (or a rescue shell)
// can inspect and repair them, and no database engine is required.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Version is the current on-disk schema version for every file in this package.
const Version = 1

// ErrNewerFormat means the file was written by a newer Sonata. Callers must
// switch to read-only mode instead of writing back.
var ErrNewerFormat = errors.New("state file was written by a newer version")

// Store reads and writes the four state files under one directory.
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open prepares dir (creating it if needed).
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("empty state directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir reports the backing directory.
func (s *Store) Dir() string { return s.dir }

// ---- config ----------------------------------------------------------------

// Config is user preferences (config.json).
type Config struct {
	Version       int                 `json:"version"`
	MusicDir      string              `json:"musicDir"`
	AutoScreenOff string              `json:"autoScreenOff"`
	Volume        int                 `json:"volume"`
	Input         map[string][]string `json:"input,omitempty"`
}

// DefaultConfig returns the documented defaults.
func DefaultConfig() Config {
	return Config{
		Version:       Version,
		MusicDir:      "/storage/music",
		AutoScreenOff: "1m",
		Volume:        70,
		Input: map[string][]string{
			"confirm":     {"Enter"},
			"back":        {"Escape"},
			"pagePrev":    {"q"},
			"pageNext":    {"e"},
			"playPause":   {" "},
			"quickAction": {"Tab"},
		},
	}
}

// LoadConfig returns config.json, creating it with defaults when absent.
func (s *Store) LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	found, newer, err := s.read("config.json", &cfg)
	if err != nil {
		return cfg, err
	}
	if newer {
		return cfg, ErrNewerFormat
	}
	if !found {
		if err := s.SaveConfig(cfg); err != nil {
			return cfg, err
		}
	}
	if cfg.Version == 0 {
		cfg.Version = Version
	}
	return cfg, nil
}

// SaveConfig writes config.json.
func (s *Store) SaveConfig(cfg Config) error {
	cfg.Version = Version
	return s.write("config.json", cfg)
}

// ---- playback state --------------------------------------------------------

// Status is the playback lifecycle state.
type Status string

const (
	StatusPlaying  Status = "playing"
	StatusPaused   Status = "paused"
	StatusStopped  Status = "stopped"
	StatusDegraded Status = "degraded"
)

// Playback is state.json: the single source of truth for what is playing.
// heya reads this file to render a now-playing line; field names are frozen.
type Playback struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
	Status    Status    `json:"status"`
	SongID    string    `json:"songId,omitempty"`
	Path      string    `json:"path,omitempty"`
	Title     string    `json:"title,omitempty"`
	Artist    string    `json:"artist,omitempty"`
	Album     string    `json:"album,omitempty"`
	Position  float64   `json:"position"`
	Duration  float64   `json:"duration"`
	Mode      string    `json:"mode"`
	Volume    int       `json:"volume"`
	MpvPID    int       `json:"mpvPid,omitempty"`
	Socket    string    `json:"socket,omitempty"`
}

// LoadPlayback reads state.json; a missing file yields a stopped state.
func (s *Store) LoadPlayback() (Playback, error) {
	p := Playback{Version: Version, Status: StatusStopped, Mode: "order"}
	found, newer, err := s.read("state.json", &p)
	if err != nil {
		return p, err
	}
	if newer {
		return p, ErrNewerFormat
	}
	if !found {
		return p, nil
	}
	return p, nil
}

// SavePlayback writes state.json with a fresh timestamp.
func (s *Store) SavePlayback(p Playback) error {
	p.Version = Version
	p.UpdatedAt = time.Now().UTC()
	return s.write("state.json", p)
}

// ---- favorites -------------------------------------------------------------

// Favorites is favorites.json.
type Favorites struct {
	Version   int       `json:"version"`
	Songs     []string  `json:"songs"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// LoadFavorites reads favorites.json.
func (s *Store) LoadFavorites() (Favorites, error) {
	f := Favorites{Version: Version, Songs: []string{}}
	_, newer, err := s.read("favorites.json", &f)
	if err != nil {
		return f, err
	}
	if newer {
		return f, ErrNewerFormat
	}
	if f.Songs == nil {
		f.Songs = []string{}
	}
	return f, nil
}

// SaveFavorites writes favorites.json.
func (s *Store) SaveFavorites(f Favorites) error {
	f.Version = Version
	f.UpdatedAt = time.Now().UTC()
	if f.Songs == nil {
		f.Songs = []string{}
	}
	return s.write("favorites.json", f)
}

// ToggleFavorite adds or removes a song ID and reports the new state.
func (f *Favorites) ToggleFavorite(id string) bool {
	for i, s := range f.Songs {
		if s == id {
			f.Songs = append(f.Songs[:i], f.Songs[i+1:]...)
			return false
		}
	}
	f.Songs = append(f.Songs, id)
	return true
}

// Has reports whether id is favorited.
func (f Favorites) Has(id string) bool {
	for _, s := range f.Songs {
		if s == id {
			return true
		}
	}
	return false
}

// ---- history ---------------------------------------------------------------

// HistoryEntry is one recorded play.
type HistoryEntry struct {
	SongID string    `json:"songId"`
	At     time.Time `json:"at"`
}

// History is history.json.
type History struct {
	Version        int            `json:"version"`
	RecentlyPlayed []HistoryEntry `json:"recentlyPlayed"`
	RecentlyAdded  []HistoryEntry `json:"recentlyAdded"`
}

// MaxHistory bounds each list so the file stays small on a handheld.
const MaxHistory = 500

// LoadHistory reads history.json.
func (s *Store) LoadHistory() (History, error) {
	h := History{Version: Version}
	_, newer, err := s.read("history.json", &h)
	if err != nil {
		return h, err
	}
	if newer {
		return h, ErrNewerFormat
	}
	return h, nil
}

// SaveHistory writes history.json, trimming to MaxHistory.
func (s *Store) SaveHistory(h History) error {
	h.Version = Version
	h.RecentlyPlayed = trimHistory(h.RecentlyPlayed)
	h.RecentlyAdded = trimHistory(h.RecentlyAdded)
	return s.write("history.json", h)
}

// RecordPlay moves id to the front of RecentlyPlayed.
func (h *History) RecordPlay(id string, at time.Time) {
	h.RecentlyPlayed = pushFront(h.RecentlyPlayed, HistoryEntry{SongID: id, At: at})
}

func pushFront(list []HistoryEntry, e HistoryEntry) []HistoryEntry {
	out := []HistoryEntry{e}
	for _, item := range list {
		if item.SongID == e.SongID {
			continue
		}
		out = append(out, item)
	}
	return trimHistory(out)
}

func trimHistory(list []HistoryEntry) []HistoryEntry {
	if len(list) <= MaxHistory {
		return list
	}
	return list[:MaxHistory]
}

// RecentIDs returns up to n song IDs, most recent first.
func (h History) RecentIDs(n int) []string {
	if n <= 0 || n > len(h.RecentlyPlayed) {
		n = len(h.RecentlyPlayed)
	}
	out := make([]string, 0, n)
	for _, e := range h.RecentlyPlayed[:n] {
		out = append(out, e.SongID)
	}
	return out
}

// Playlist is one named playlist (playlists/<id>.json).
type Playlist struct {
	Version   int           `json:"version"`
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	SongIDs   []string      `json:"songIds"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// LoadPlaylist reads one playlist by id.
func (s *Store) LoadPlaylist(id string) (Playlist, error) {
	p := Playlist{Version: Version, ID: id}
	found, newer, err := s.read(filepath.Join("playlists", id+".json"), &p)
	if err != nil {
		return p, err
	}
	if newer {
		return p, ErrNewerFormat
	}
	if !found {
		return p, ErrPlaylistNotFound
	}
	if p.SongIDs == nil {
		p.SongIDs = []string{}
	}
	return p, nil
}

// SavePlaylist writes playlists/<id>.json.
func (s *Store) SavePlaylist(p Playlist) error {
	p.Version = Version
	p.UpdatedAt = time.Now().UTC()
	if p.ID == "" {
		return errors.New("playlist id is empty")
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.SongIDs == nil {
		p.SongIDs = []string{}
	}
	return s.write(filepath.Join("playlists", p.ID+".json"), p)
}

// DeletePlaylist removes one playlist.
func (s *Store) DeletePlaylist(id string) error {
	path := filepath.Join(s.dir, "playlists", id+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrPlaylistNotFound
		}
		return err
	}
	return nil
}

// ListPlaylists returns every playlist, newest first.
func (s *Store) ListPlaylists() ([]Playlist, error) {
	dir := filepath.Join(s.dir, "playlists")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Playlist
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		p, err := s.LoadPlaylist(id)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// ErrPlaylistNotFound is returned when a playlist does not exist.
var ErrPlaylistNotFound = errors.New("playlist not found")

// read decodes path into v. found reports whether the file existed; newer
// reports whether the file's version is greater than Version.
func (s *Store) read(name string, v any) (found, newer bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, name)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		// Corrupt file: keep a copy for forensics, fall back to defaults.
		backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		_ = os.WriteFile(backup, data, 0o644)
		return false, false, fmt.Errorf("%s: %w", name, err)
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.Version > Version {
		return true, true, nil
	}
	return true, false, nil
}

func (s *Store) write(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(filepath.Join(s.dir, name), data)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// SortedFavorites returns favorites in a stable order (by ID) for tests.
func SortedFavorites(f Favorites) []string {
	out := append([]string(nil), f.Songs...)
	sort.Strings(out)
	return out
}
