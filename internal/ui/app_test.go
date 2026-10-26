package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/iamcheyan/sonata/internal/library"
	"github.com/iamcheyan/sonata/internal/player"
	"github.com/iamcheyan/sonata/internal/state"
)

type fakeController struct {
	pb      state.Playback
	mode    player.Mode
	volume  int
	queue   []library.Song
	played  []string
	toggled int
}

func (f *fakeController) PlaySongs(songs []library.Song, start int) error {
	f.queue = songs
	if start < len(songs) {
		s := songs[start]
		f.pb.SongID = s.ID
		f.pb.Title = s.Title
		f.pb.Artist = s.Artist
		f.pb.Album = s.Album
		f.pb.Status = state.StatusPlaying
		f.played = append(f.played, s.ID)
	}
	return nil
}
func (f *fakeController) Toggle() error      { f.toggled++; return nil }
func (f *fakeController) Next() error        { return nil }
func (f *fakeController) Prev() error        { return nil }
func (f *fakeController) Seek(float64) error { return nil }
func (f *fakeController) Mode() player.Mode  { return f.mode }
func (f *fakeController) CycleMode() player.Mode {
	f.mode = f.mode.Cycle()
	return f.mode
}
func (f *fakeController) Volume() int              { return f.volume }
func (f *fakeController) SetVolume(v int) error    { f.volume = v; return nil }
func (f *fakeController) Playback() state.Playback { return f.pb }
func (f *fakeController) Available() bool          { return true }
func (f *fakeController) Queue() []library.Song    { return f.queue }

func testLibrary() *library.Library {
	lib := &library.Library{
		Version: 1,
		Root:    "/music",
		Songs: []library.Song{
			{ID: "s_a", Path: "/music/a.flac", Title: "七里香", Artist: "周杰伦", Album: "七里香", Track: 1, Duration: 272 * time.Second, Format: "FLAC"},
			{ID: "s_b", Path: "/music/b.flac", Title: "我的地盘", Artist: "周杰伦", Album: "七里香", Track: 2, Duration: 251 * time.Second, Format: "FLAC"},
		},
		Albums:  []library.Album{{Name: "七里香", Artist: "周杰伦", SongIDs: []string{"s_a", "s_b"}}},
		Artists: []library.Artist{{Name: "周杰伦", Albums: []string{"七里香"}, SongIDs: []string{"s_a", "s_b"}}},
	}
	return lib
}

func newTestApp(t *testing.T, ctl *fakeController) (*App, tcell.SimulationScreen) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(90, 30)
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := state.DefaultConfig()
	fav := state.Favorites{Songs: []string{}}
	hist := state.History{}
	app := New(screen, testLibrary(), store, cfg, fav, hist, ctl, Capabilities{
		BatteryPercent: 82,
		HasBattery:     true,
		Clock:          func() time.Time { return time.Date(2026, 10, 26, 12, 4, 0, 0, time.UTC) },
	})
	return app, screen
}

// renderLines renders a frame and returns it as plain text rows.
func renderLines(app *App, screen tcell.SimulationScreen) []string {
	app.Draw()
	w, h := screen.Size()
	lines := make([]string, h)
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
				// Wide (CJK) rune: the following cell is part of the glyph.
				x += width - 1
			}
		}
		lines[y] = b.String()
	}
	return lines
}

func TestMainScreenLayout(t *testing.T) {
	ctl := &fakeController{pb: state.Playback{Status: state.StatusPlaying}, volume: 70, mode: player.ModeOrder}
	app, screen := newTestApp(t, ctl)
	lines := renderLines(app, screen)

	if !strings.Contains(lines[0], "Sonata") || !strings.Contains(lines[0], "mpv: playing") {
		t.Errorf("status bar missing: %q", lines[0])
	}
	if !strings.Contains(lines[0], "BAT 82%") || !strings.Contains(lines[0], "12:04") {
		t.Errorf("status bar right side missing: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Music") {
		t.Errorf("list header missing: %q", lines[1])
	}
	if !strings.Contains(lines[2], "> All Songs") {
		t.Errorf("selection marker missing: %q", lines[2])
	}
	if !strings.Contains(lines[29], "[A] Open") {
		t.Errorf("hint bar missing: %q", lines[29])
	}
	if !strings.Contains(lines[5], "│") {
		t.Errorf("vertical separator missing on row 5: %q", lines[5])
	}
	// Four zones: status(1) + content(28) + hint(1)
	if len(lines) != 30 {
		t.Fatalf("height = %d", len(lines))
	}
}

func TestSongsPageAndFavorite(t *testing.T) {
	ctl := &fakeController{volume: 70, mode: player.ModeOrder}
	app, screen := newTestApp(t, ctl)

	app.HandleKey(key(tcell.KeyEnter, 0)) // open All Songs
	lines := renderLines(app, screen)
	if !strings.Contains(lines[0], "All Songs") {
		t.Fatalf("title = %q", lines[0])
	}
	if !strings.Contains(lines[2], "七里香") {
		t.Fatalf("first song missing: %q", lines[2])
	}
	if !strings.Contains(lines[2], "4:32") {
		t.Errorf("duration missing: %q", lines[2])
	}
	// Detail panel should describe the selected song.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "周杰伦") || !strings.Contains(joined, "FLAC") {
		t.Errorf("detail panel incomplete:\n%s", joined)
	}

	app.HandleKey(key(tcell.KeyTab, 0)) // favorite
	if !app.fav.Has("s_a") {
		t.Error("song was not favorited")
	}
}

func TestNowPlayingShowsProgress(t *testing.T) {
	ctl := &fakeController{
		pb: state.Playback{
			Status: state.StatusPlaying, SongID: "s_a", Title: "七里香", Artist: "周杰伦",
			Album: "七里香", Position: 72, Duration: 272, Mode: "order", Volume: 70,
		},
		volume: 70, mode: player.ModeOrder,
	}
	app, screen := newTestApp(t, ctl)
	app.push(app.nowPlayingPage())
	lines := renderLines(app, screen)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"七里香", "周杰伦", "1:12 / 4:32", "█"} {
		if !strings.Contains(joined, want) {
			t.Errorf("now playing missing %q:\n%s", want, joined)
		}
	}
}

func TestLockScreenBlocksNavigation(t *testing.T) {
	ctl := &fakeController{pb: state.Playback{Status: state.StatusPlaying, Title: "七里香"}, volume: 70, mode: player.ModeOrder}
	app, screen := newTestApp(t, ctl)

	app.HandleKey(key(0, 'l'))
	if !app.locked {
		t.Fatal("expected locked state")
	}
	before := app.top().sel
	app.HandleKey(key(tcell.KeyDown, 0))
	app.HandleKey(key(tcell.KeyEnter, 0))
	if app.top().sel != before {
		t.Error("lock screen must ignore navigation")
	}
	if len(app.stack) != 1 {
		t.Error("lock screen must ignore confirm")
	}
	lines := renderLines(app, screen)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "LOCKED") || !strings.Contains(joined, "Hold L1 + R1 to unlock") {
		t.Errorf("lock screen content missing:\n%s", joined)
	}

	// Play/pause still works while locked.
	app.HandleKey(key(0, ' '))
	if ctl.toggled != 1 {
		t.Errorf("play/pause ignored while locked: %d", ctl.toggled)
	}
	// L1+R1 unlocks.
	app.HandleKey(key(0, 'q'))
	app.HandleKey(key(0, 'e'))
	if app.locked {
		t.Error("L1+R1 should unlock")
	}
}

func TestScreenOffSwallowsFirstKey(t *testing.T) {
	ctl := &fakeController{volume: 70, mode: player.ModeOrder}
	app, _ := newTestApp(t, ctl)
	app.ScreenOff()
	if !app.off {
		t.Fatal("screen should be off")
	}
	app.HandleKey(key(tcell.KeyDown, 0))
	if app.off {
		t.Error("first key should only wake the screen")
	}
	if app.top().sel != 0 {
		t.Error("wake key must not move the cursor")
	}
}

func TestCompactModeHidesDetail(t *testing.T) {
	ctl := &fakeController{volume: 70, mode: player.ModeOrder}
	app, screen := newTestApp(t, ctl)
	screen.SetSize(70, 20)
	lines := renderLines(app, screen)
	if len(lines) != 20 {
		t.Fatalf("height = %d", len(lines))
	}
	for _, l := range lines {
		if strings.Contains(l, "│") {
			t.Errorf("compact mode should not draw the separator: %q", l)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(0, 0, 10); strings.Count(got, "░") != 9 {
		t.Errorf("unknown duration bar = %q", got)
	}
	if got := progressBar(50, 100, 10); !strings.HasPrefix(got, "[████") {
		t.Errorf("half bar = %q", got)
	}
	if got := mmss(72); got != "1:12" {
		t.Errorf("mmss(72) = %q", got)
	}
}

func key(k tcell.Key, r rune) *tcell.EventKey {
	if r != 0 {
		return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
	}
	return tcell.NewEventKey(k, 0, tcell.ModNone)
}
