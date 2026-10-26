// Package library builds an immutable index of the local music collection.
//
// Scanning never mutates files and never requires network access. Tag parsing
// is best-effort: every field has a deterministic fallback so a library with
// broken or missing metadata still renders and plays.
package library

import (
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// audioExts is the set of extensions we index. Anything else is ignored.
var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true,
	".ogg": true, ".opus": true, ".wav": true,
}

// Song is one playable track.
type Song struct {
	ID       string        `json:"id"`
	Path     string        `json:"path"`
	Title    string        `json:"title"`
	Artist   string        `json:"artist"`
	Album    string        `json:"album"`
	Track    int           `json:"track,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	Format   string        `json:"format,omitempty"`
}

// DisplayDuration renders mm:ss, or --:-- when the duration is unknown.
func (s Song) DisplayDuration() string {
	if s.Duration <= 0 {
		return "--:--"
	}
	d := s.Duration
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// Album groups songs that share an album title and artist.
type Album struct {
	Name    string   `json:"name"`
	Artist  string   `json:"artist"`
	Year    int      `json:"year,omitempty"`
	SongIDs []string `json:"songIds"`
}

// Artist groups albums.
type Artist struct {
	Name    string   `json:"name"`
	Albums  []string `json:"albums"`
	SongIDs []string `json:"songIds"`
}

// Library is the scanned index. It is treated as immutable once built.
type Library struct {
	Version     int         `json:"version"`
	Root        string      `json:"root"`
	GeneratedAt time.Time   `json:"generatedAt"`
	Fingerprint Fingerprint `json:"fingerprint"`
	Songs       []Song      `json:"songs"`
	Albums      []Album     `json:"albums"`
	Artists     []Artist    `json:"artists"`
	Skipped     []string    `json:"skipped,omitempty"`

	byID map[string]*Song
}

// Fingerprint describes the directory state a scan was based on.
type Fingerprint struct {
	Files       int   `json:"files"`
	Bytes       int64 `json:"bytes"`
	NewestMtime int64 `json:"newestMtime"`
}

// Counts is a small summary used by the UI and `sonata scan`.
type Counts struct {
	Songs   int
	Artists int
	Albums  int
	Skipped int
}

// Counts summarises the library.
func (l *Library) Counts() Counts {
	return Counts{Songs: len(l.Songs), Artists: len(l.Artists), Albums: len(l.Albums), Skipped: len(l.Skipped)}
}

// Song looks a song up by ID.
func (l *Library) Song(id string) (Song, bool) {
	if l.byID == nil {
		l.index()
	}
	s, ok := l.byID[id]
	if !ok {
		return Song{}, false
	}
	return *s, true
}

// SongsOf returns the songs referenced by ids, preserving order.
func (l *Library) SongsOf(ids []string) []Song {
	out := make([]Song, 0, len(ids))
	for _, id := range ids {
		if s, ok := l.Song(id); ok {
			out = append(out, s)
		}
	}
	return out
}

// AlbumSongs returns the songs of one album, ordered by track number.
func (l *Library) AlbumSongs(name, artist string) []Song {
	for _, a := range l.Albums {
		if a.Name == name && (artist == "" || a.Artist == artist) {
			return l.SongsOf(a.SongIDs)
		}
	}
	return nil
}

func (l *Library) index() {
	l.byID = make(map[string]*Song, len(l.Songs))
	for i := range l.Songs {
		l.byID[l.Songs[i].ID] = &l.Songs[i]
	}
}

// SongID derives a stable identifier from the path relative to the library
// root, so moving the whole collection keeps the same IDs.
func SongID(rel string) string {
	sum := sha1.Sum([]byte(filepath.ToSlash(rel)))
	return "s_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:7]))
}

// FallbackTitle strips the extension and any leading track number.
func FallbackTitle(name string) (title string, track int) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.TrimSpace(base)
	// "01 - Title", "01. Title", "01 Title", "01_Title"
	if len(base) > 1 {
		i := 0
		for i < len(base) && base[i] >= '0' && base[i] <= '9' && i < 3 {
			i++
		}
		if i > 0 && i < len(base) {
			rest := base[i:]
			trimmed := strings.TrimLeft(rest, " -._")
			if trimmed != "" && trimmed != rest {
				if n, err := strconv.Atoi(base[:i]); err == nil {
					track = n
				}
				return strings.TrimSpace(trimmed), track
			}
		}
	}
	return base, 0
}

// sortSongs orders by artist, album, track, title, path — deterministic so
// tests and cache files stay stable.
func sortSongs(songs []Song) {
	sort.SliceStable(songs, func(i, j int) bool {
		a, b := songs[i], songs[j]
		if a.Artist != b.Artist {
			return strings.ToLower(a.Artist) < strings.ToLower(b.Artist)
		}
		if a.Album != b.Album {
			return strings.ToLower(a.Album) < strings.ToLower(b.Album)
		}
		if a.Track != b.Track {
			return a.Track < b.Track
		}
		if a.Title != b.Title {
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		}
		return a.Path < b.Path
	})
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
