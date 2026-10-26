package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
)

// ScanOptions controls a scan.
type ScanOptions struct {
	// Workers is the number of concurrent tag readers. Zero means a small
	// default capped at 4, which keeps SD-card random reads reasonable.
	Workers int
	// Cache, when non-empty, is where the resulting index is written.
	Cache string
}

// Scan walks root and builds the index. Individual unreadable files are
// collected in Library.Skipped instead of aborting the whole scan.
func Scan(root string, opts ScanOptions) (*Library, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("music directory %s: %w", abs, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("music directory %s is not a directory", abs)
	}

	type job struct {
		path string
		rel  string
		size int64
	}
	var jobs []job
	fp := Fingerprint{}

	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, don't abort
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			rel = d.Name()
		}
		fp.Files++
		fp.Bytes += info.Size()
		if info.ModTime().Unix() > fp.NewestMtime {
			fp.NewestMtime = info.ModTime().Unix()
		}
		jobs = append(jobs, job{path: path, rel: rel, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 4
	}
	if workers > 4 {
		workers = 4
	}

	results := make([]Song, len(jobs))
	var skipped []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	ch := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				song, err := readSong(abs, jobs[i].path, jobs[i].rel)
				if err != nil {
					mu.Lock()
					skipped = append(skipped, jobs[i].rel)
					mu.Unlock()
					continue
				}
				results[i] = song
			}
		}()
	}
	for i := range jobs {
		ch <- i
	}
	close(ch)
	wg.Wait()

	songs := make([]Song, 0, len(results))
	for _, s := range results {
		if s.ID != "" {
			songs = append(songs, s)
		}
	}
	sortSongs(songs)

	lib := &Library{
		Version:     CacheVersion,
		Root:        abs,
		GeneratedAt: time.Now().UTC(),
		Fingerprint: fp,
		Songs:       songs,
		Skipped:     skipped,
	}
	sort.Strings(lib.Skipped)
	lib.buildGroups()
	lib.index()

	if opts.Cache != "" {
		if err := SaveCache(opts.Cache, lib); err != nil {
			return lib, fmt.Errorf("write cache: %w", err)
		}
	}
	return lib, nil
}

// readSong parses one file, applying fallbacks when tags are missing.
func readSong(root, path, rel string) (Song, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Song{}, err
	}
	song := Song{
		ID:     SongID(rel),
		Path:   path,
		Format: strings.ToUpper(strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")),
	}

	f, err := os.Open(path)
	if err != nil {
		return Song{}, err
	}
	defer f.Close()

	if m, err := tag.ReadFrom(f); err == nil {
		song.Title = strings.TrimSpace(m.Title())
		song.Artist = strings.TrimSpace(m.Artist())
		song.Album = strings.TrimSpace(m.Album())
		song.Track, _ = m.Track()
		// Format stays extension-based: tag.Format is a type enum, and the
		// extension is what the user sees on the card.
	}
	// Untagged files are common on handhelds and are not an error.

	dir := filepath.Dir(rel)
	parts := strings.Split(filepath.ToSlash(dir), "/")
	if dir == "." {
		parts = nil
	}

	fallbackTitle, fallbackTrack := FallbackTitle(filepath.Base(path))
	if song.Title == "" {
		song.Title = fallbackTitle
	}
	if song.Track == 0 {
		song.Track = fallbackTrack
	}
	if song.Album == "" && len(parts) > 0 {
		song.Album = parts[len(parts)-1]
	}
	if song.Artist == "" {
		switch {
		case len(parts) >= 2:
			song.Artist = parts[len(parts)-2]
		case len(parts) == 1:
			song.Artist = parts[0]
		default:
			song.Artist = "Unknown Artist"
		}
	}
	if song.Album == "" {
		song.Album = "Unknown Album"
	}
	if song.Title == "" {
		song.Title = info.Name()
	}
	if song.Duration <= 0 {
		song.Duration = probeDuration(path)
	}
	return song, nil
}

// buildGroups derives artists and albums from songs.
func (l *Library) buildGroups() {
	albumKey := func(s Song) string { return strings.ToLower(s.Artist) + "\x00" + strings.ToLower(s.Album) }
	albums := map[string]*Album{}
	albumOrder := []string{}
	for _, s := range l.Songs {
		k := albumKey(s)
		a, ok := albums[k]
		if !ok {
			a = &Album{Name: s.Album, Artist: s.Artist}
			albums[k] = a
			albumOrder = append(albumOrder, k)
		}
		a.SongIDs = append(a.SongIDs, s.ID)
	}

	artistAlbums := map[string][]string{}
	artistSongs := map[string][]string{}
	artistOrder := []string{}
	seenArtist := map[string]bool{}
	for _, s := range l.Songs {
		if !seenArtist[s.Artist] {
			seenArtist[s.Artist] = true
			artistOrder = append(artistOrder, s.Artist)
		}
		artistSongs[s.Artist] = append(artistSongs[s.Artist], s.ID)
	}
	for _, k := range albumOrder {
		a := albums[k]
		artistAlbums[a.Artist] = append(artistAlbums[a.Artist], a.Name)
	}

	sort.SliceStable(albumOrder, func(i, j int) bool {
		a, b := albums[albumOrder[i]], albums[albumOrder[j]]
		if !strings.EqualFold(a.Artist, b.Artist) {
			return strings.ToLower(a.Artist) < strings.ToLower(b.Artist)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	l.Albums = make([]Album, 0, len(albumOrder))
	for _, k := range albumOrder {
		l.Albums = append(l.Albums, *albums[k])
	}

	sort.SliceStable(artistOrder, func(i, j int) bool {
		return strings.ToLower(artistOrder[i]) < strings.ToLower(artistOrder[j])
	})
	l.Artists = make([]Artist, 0, len(artistOrder))
	for _, name := range artistOrder {
		l.Artists = append(l.Artists, Artist{Name: name, Albums: artistAlbums[name], SongIDs: artistSongs[name]})
	}
}

// ErrStaleCache is returned when the cache does not match the directory.
var ErrStaleCache = errors.New("library cache is stale")
