package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/iamcheyan/sonata/internal/library"
	"github.com/iamcheyan/sonata/internal/player"
	"github.com/iamcheyan/sonata/internal/state"
)

// StaticController replays a saved playback state without touching mpv. It is
// used by the --dump screenshot path so documented frames are reproducible on
// machines without audio hardware.
type StaticController struct {
	Pb       state.Playback
	Songs    []library.Song
	PlayMode player.Mode
}

func (s *StaticController) PlaySongs(songs []library.Song, start int) error {
	s.Songs = songs
	if start >= 0 && start < len(songs) {
		song := songs[start]
		s.Pb.SongID = song.ID
		s.Pb.Title = song.Title
		s.Pb.Artist = song.Artist
		s.Pb.Album = song.Album
		s.Pb.Status = state.StatusPlaying
	}
	return nil
}
func (s *StaticController) Toggle() error {
	if s.Pb.Status == state.StatusPlaying {
		s.Pb.Status = state.StatusPaused
	} else {
		s.Pb.Status = state.StatusPlaying
	}
	return nil
}
func (s *StaticController) Next() error        { return nil }
func (s *StaticController) Prev() error        { return nil }
func (s *StaticController) Seek(float64) error { return nil }
func (s *StaticController) Mode() player.Mode  { return s.PlayMode }
func (s *StaticController) CycleMode() player.Mode {
	s.PlayMode = s.PlayMode.Cycle()
	return s.PlayMode
}
func (s *StaticController) Volume() int              { return s.Pb.Volume }
func (s *StaticController) SetVolume(v int) error    { s.Pb.Volume = v; return nil }
func (s *StaticController) Playback() state.Playback { return s.Pb }
func (s *StaticController) Available() bool          { return true }
func (s *StaticController) Queue() []library.Song    { return s.Songs }

// DumpOptions configures frame rendering.
type DumpOptions struct {
	Width, Height int
	Clock         func() int // minutes since midnight, for a fixed status clock
}

// Dump writes one 90x30 text frame per page into dir. It never opens a real
// terminal, so it works in CI and on headless build hosts.
func Dump(dir string, lib *library.Library, store *state.Store, cfg state.Config, fav state.Favorites, hist state.History, ctl Controller, caps Capabilities) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.SetSize(90, 30)

	app := New(screen, lib, store, cfg, fav, hist, ctl, caps)
	frames := []struct {
		name  string
		setup func(*App)
	}{
		{"01-main", func(*App) {}},
		{"02-songs", func(a *App) { a.push(a.songsPage()) }},
		{"03-player", func(a *App) { a.push(a.nowPlayingPage()) }},
		{"04-artists", func(a *App) { a.push(a.artistsPage()) }},
		{"05-albums", func(a *App) { a.push(a.albumsPage()) }},
		{"06-favorites", func(a *App) { a.push(a.favoritesPage()) }},
		{"07-lock", func(a *App) { a.lock() }},
	}
	for _, f := range frames {
		app.stack = []*page{app.mainPage()}
		app.locked = false
		f.setup(app)
		app.Draw()
		text := FrameText(screen)
		path := filepath.Join(dir, f.name+".txt")
		if err := os.WriteFile(path, []byte(text+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// FrameText flattens a screen into newline-separated rows, dropping the
// continuation cells of wide (CJK) glyphs.
func FrameText(screen tcell.Screen) string {
	w, h := screen.Size()
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			mainc, _, _, width := screen.GetContent(x, y)
			if width == 0 {
				continue
			}
			if mainc == 0 {
				mainc = ' '
			}
			b.WriteRune(mainc)
			if width > 1 {
				x += width - 1
			}
		}
		rows[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(rows, "\n")
}

// DumpSummary is a helper for callers that want a one-line report.
func DumpSummary(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Sprintf("dump %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return fmt.Sprintf("wrote %d frames to %s: %s", len(names), dir, strings.Join(names, ", "))
}
