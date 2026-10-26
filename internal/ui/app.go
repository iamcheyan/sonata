// Package ui renders Sonata's terminal interface.
//
// The layout is a fixed four-zone frame (status bar, list, detail, hint bar)
// sized for a 90x30 grid, which matches the 720x480 landscape panel of the
// handheld target. Screens are drawn by hand on tcell so that a future SDL
// renderer can reuse the same geometry.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/iamcheyan/sonata/internal/library"
	"github.com/iamcheyan/sonata/internal/player"
	"github.com/iamcheyan/sonata/internal/state"
)

// Layout constants (see UI_GUIDELINES.md).
const (
	LeftWidth   = 32
	ContentRows = 28
	MinWidth    = 90
	MinHeight   = 30
)

// Controller is the playback surface the UI drives. Implemented by the app
// service; faked in tests.
type Controller interface {
	PlaySongs(songs []library.Song, start int) error
	Toggle() error
	Next() error
	Prev() error
	Seek(delta float64) error
	Mode() player.Mode
	CycleMode() player.Mode
	Volume() int
	SetVolume(v int) error
	Playback() state.Playback
	Available() bool
}

// PageKind identifies a screen.
type PageKind int

const (
	PageMain PageKind = iota
	PageSongs
	PageArtists
	PageAlbums
	PageFavorites
	PageRecent
	PageNowPlaying
	PageQueue
	PageLock
	PageScreenOff
)

// Item is one selectable row.
type Item struct {
	Label  string
	Song   *library.Song
	Album  *library.Album
	Artist *library.Artist
	// Action overrides the default confirm behaviour (menu entries).
	Action func(*App) error
}

// page is a navigation frame with its own cursor.
type page struct {
	kind  PageKind
	title string
	items []Item
	sel   int
	top   int
}

// App is the UI state machine.
type App struct {
	screen tcell.Screen
	lib    *library.Library
	store  *state.Store
	cfg    state.Config
	fav    state.Favorites
	hist   state.History
	ctl    Controller
	caps   Capabilities

	stack  []*page
	locked bool
	off    bool
	// wakeGuard swallows the first key after the screen comes back on.
	wakeGuard bool
	status    string
	err       error
	quit      bool

	// long-press bookkeeping
	lastKey  tcell.Key
	keyCount int
	lastAt   time.Time
	// lock unlock: q and e pressed within the window
	qAt, eAt time.Time
}

// Capabilities is the subset of device probing the UI needs.
type Capabilities struct {
	BatteryPercent int
	HasBattery     bool
	Backlight      bool
	Clock          func() time.Time
}

// New builds an App and seeds the main page.
func New(screen tcell.Screen, lib *library.Library, store *state.Store, cfg state.Config, fav state.Favorites, hist state.History, ctl Controller, caps Capabilities) *App {
	if caps.Clock == nil {
		caps.Clock = time.Now
	}
	a := &App{screen: screen, lib: lib, store: store, cfg: cfg, fav: fav, hist: hist, ctl: ctl, caps: caps}
	a.stack = []*page{a.mainPage()}
	return a
}

// Quit reports whether the user asked to exit.
func (a *App) Quit() bool { return a.quit }

// Status returns the current transient message.
func (a *App) Status() string { return a.status }

func (a *App) top() *page { return a.stack[len(a.stack)-1] }

func (a *App) push(p *page) {
	a.stack = append(a.stack, p)
}

func (a *App) pop() {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
}

// mainPage builds the root menu.
func (a *App) mainPage() *page {
	c := a.lib.Counts()
	p := &page{kind: PageMain, title: "Music"}
	p.items = []Item{
		{Label: fmt.Sprintf("All Songs (%d)", c.Songs), Action: func(a *App) error { a.push(a.songsPage()); return nil }},
		{Label: fmt.Sprintf("Artists (%d)", c.Artists), Action: func(a *App) error { a.push(a.artistsPage()); return nil }},
		{Label: fmt.Sprintf("Albums (%d)", c.Albums), Action: func(a *App) error { a.push(a.albumsPage()); return nil }},
		{Label: fmt.Sprintf("Favorites (%d)", len(a.fav.Songs)), Action: func(a *App) error { a.push(a.favoritesPage()); return nil }},
		{Label: "Recently Played", Action: func(a *App) error { a.push(a.recentPage()); return nil }},
		{Label: "Now Playing", Action: func(a *App) error { a.push(a.nowPlayingPage()); return nil }},
		{Label: "Queue", Action: func(a *App) error { a.push(a.queuePage()); return nil }},
	}
	return p
}

func (a *App) songsPage() *page {
	p := &page{kind: PageSongs, title: "All Songs"}
	for i := range a.lib.Songs {
		s := a.lib.Songs[i]
		p.items = append(p.items, Item{Label: songLabel(s), Song: &a.lib.Songs[i]})
	}
	return p
}

func (a *App) artistsPage() *page {
	p := &page{kind: PageArtists, title: "Artists"}
	for i := range a.lib.Artists {
		ar := a.lib.Artists[i]
		p.items = append(p.items, Item{
			Label:  fmt.Sprintf("%s (%d albums)", ar.Name, len(ar.Albums)),
			Artist: &a.lib.Artists[i],
		})
	}
	return p
}

func (a *App) albumsPage() *page {
	p := &page{kind: PageAlbums, title: "Albums"}
	for i := range a.lib.Albums {
		al := a.lib.Albums[i]
		p.items = append(p.items, Item{
			Label: fmt.Sprintf("%-24s %2d trk", truncate(al.Name, 24), len(al.SongIDs)),
			Album: &a.lib.Albums[i],
		})
	}
	return p
}

func (a *App) favoritesPage() *page {
	p := &page{kind: PageFavorites, title: "Favorites"}
	for _, id := range a.fav.Songs {
		if s, ok := a.lib.Song(id); ok {
			song := s
			p.items = append(p.items, Item{Label: songLabel(song), Song: &song})
		}
	}
	return p
}

func (a *App) recentPage() *page {
	p := &page{kind: PageRecent, title: "Recently Played"}
	for _, id := range a.hist.RecentIDs(50) {
		if s, ok := a.lib.Song(id); ok {
			song := s
			p.items = append(p.items, Item{Label: songLabel(song), Song: &song})
		}
	}
	return p
}

func (a *App) nowPlayingPage() *page {
	return &page{kind: PageNowPlaying, title: "Now Playing"}
}

func (a *App) queuePage() *page {
	p := &page{kind: PageQueue, title: "Queue"}
	if a.ctl != nil {
		for i, s := range queueOf(a.ctl) {
			song := s
			p.items = append(p.items, Item{Label: fmt.Sprintf("%02d %s", i+1, songLabel(song)), Song: &song})
		}
	}
	return p
}

func songLabel(s library.Song) string {
	return padRight(truncate(s.Title, 20), 20) + " " + s.DisplayDuration()
}

// queueOf is a small helper so the queue page does not need extra interface
// methods on Controller.
func queueOf(c Controller) []library.Song {
	type queuer interface{ Queue() []library.Song }
	if q, ok := c.(queuer); ok {
		return q.Queue()
	}
	return nil
}

// ---- input -----------------------------------------------------------------

// HandleKey processes one key event.
func (a *App) HandleKey(ev *tcell.EventKey) {
	if a.off {
		a.off = false
		a.wakeGuard = true
		a.status = "screen on"
		return
	}
	if a.wakeGuard {
		a.wakeGuard = false
		return
	}

	held := a.trackHold(ev)

	if a.locked {
		a.handleLocked(ev)
		return
	}

	// Long press: Select (Tab) locks, Start (Space) blanks the screen.
	if held >= 8 {
		switch ev.Key() {
		case tcell.KeyTab:
			a.lock()
			return
		case tcell.KeyCtrlSpace:
			a.screenOff()
			return
		}
		if ev.Rune() == ' ' {
			a.screenOff()
			return
		}
	}

	switch ev.Key() {
	case tcell.KeyEscape:
		// B is Back, never Quit: leaving the app is explicit ([X] or Ctrl-C),
		// so a stray B press on the root page cannot drop the user to a shell.
		a.pop()
	case tcell.KeyUp:
		a.move(-1)
	case tcell.KeyDown:
		a.move(1)
	case tcell.KeyPgUp:
		a.move(-10)
	case tcell.KeyPgDn:
		a.move(10)
	case tcell.KeyEnter:
		a.confirm()
	case tcell.KeyLeft:
		a.seek(-10)
	case tcell.KeyRight:
		a.seek(10)
	case tcell.KeyTab:
		a.toggleFavorite()
	case tcell.KeyCtrlL:
		a.lock()
	default:
		switch ev.Rune() {
		case 'k':
			a.move(-1)
		case 'j':
			a.move(1)
		case 'q':
			a.pageJump(-1)
		case 'e':
			a.pageJump(1)
		case ' ':
			a.togglePlay()
		case 'l':
			a.lock()
		case 'o':
			a.screenOff()
		case 'm':
			if a.ctl != nil {
				a.status = "mode: " + string(a.ctl.CycleMode())
			}
		case '=', '+':
			a.changeVolume(5)
		case '-', '_':
			a.changeVolume(-5)
		case 'x':
			a.quit = true
		}
	}
}

// trackHold counts rapid repeats of the same key, approximating a long press.
func (a *App) trackHold(ev *tcell.EventKey) int {
	now := time.Now()
	if ev.Key() == a.lastKey && now.Sub(a.lastAt) < 250*time.Millisecond {
		a.keyCount++
	} else {
		a.keyCount = 1
		a.lastKey = ev.Key()
	}
	a.lastAt = now
	return a.keyCount
}

func (a *App) handleLocked(ev *tcell.EventKey) {
	now := time.Now()
	switch ev.Key() {
	case tcell.KeyCtrlSpace:
		a.togglePlay()
		return
	case tcell.KeyEscape:
		a.eAt = now
	default:
		switch ev.Rune() {
		case 'q':
			a.qAt = now
		case 'e':
			a.eAt = now
		case ' ':
			a.togglePlay()
			return
		}
	}

	// Unlock: L1+R1 (q and e) pressed within 500ms of each other. This is a
	// two-handed gesture so a pocket press cannot unlock the screen.
	if !a.qAt.IsZero() && !a.eAt.IsZero() && a.qAt.Sub(a.eAt).Abs() < 500*time.Millisecond {
		a.locked = false
		a.qAt, a.eAt = time.Time{}, time.Time{}
		a.status = "unlocked"
		return
	}

	switch ev.Rune() {
	case 'q':
		a.prev()
	case 'e':
		a.next()
	}
}

func (a *App) lock() {
	a.locked = true
	a.qAt, a.eAt = time.Time{}, time.Time{}
	a.status = "locked"
}

func (a *App) screenOff() {
	a.off = true
}

func (a *App) move(delta int) {
	p := a.top()
	if len(p.items) == 0 {
		return
	}
	p.sel += delta
	if p.sel < 0 {
		p.sel = 0
	}
	if p.sel >= len(p.items) {
		p.sel = len(p.items) - 1
	}
}

func (a *App) confirm() {
	p := a.top()
	if p.kind == PageNowPlaying || p.kind == PageQueue {
		a.togglePlay()
		return
	}
	if len(p.items) == 0 {
		return
	}
	it := p.items[p.sel]
	if it.Action != nil {
		if err := it.Action(a); err != nil {
			a.err = err
			a.status = err.Error()
		}
		return
	}
	switch {
	case it.Song != nil:
		if a.ctl == nil {
			a.status = "mpv unavailable"
			return
		}
		songs := a.songsForPage(p)
		if err := a.ctl.PlaySongs(songs, p.sel); err != nil {
			a.err = err
			a.status = err.Error()
			return
		}
		a.hist.RecordPlay(it.Song.ID, time.Now().UTC())
		_ = a.store.SaveHistory(a.hist)
		a.push(a.nowPlayingPage())
	case it.Album != nil:
		songs := a.lib.AlbumSongs(it.Album.Name, it.Album.Artist)
		if a.ctl != nil && len(songs) > 0 {
			if err := a.ctl.PlaySongs(songs, 0); err != nil {
				a.status = err.Error()
				return
			}
			a.push(a.nowPlayingPage())
		}
	case it.Artist != nil:
		var songs []library.Song
		for _, name := range it.Artist.Albums {
			songs = append(songs, a.lib.AlbumSongs(name, it.Artist.Name)...)
		}
		if a.ctl != nil && len(songs) > 0 {
			if err := a.ctl.PlaySongs(songs, 0); err != nil {
				a.status = err.Error()
				return
			}
			a.push(a.nowPlayingPage())
		}
	}
}

func (a *App) songsForPage(p *page) []library.Song {
	out := make([]library.Song, 0, len(p.items))
	for _, it := range p.items {
		if it.Song != nil {
			out = append(out, *it.Song)
		}
	}
	return out
}

func (a *App) togglePlay() {
	if a.ctl == nil {
		a.status = "mpv unavailable"
		return
	}
	if err := a.ctl.Toggle(); err != nil {
		a.status = err.Error()
	}
}

func (a *App) next() {
	if a.ctl != nil {
		if err := a.ctl.Next(); err != nil {
			a.status = err.Error()
		}
	}
}

func (a *App) prev() {
	if a.ctl != nil {
		if err := a.ctl.Prev(); err != nil {
			a.status = err.Error()
		}
	}
}

func (a *App) seek(delta float64) {
	if a.ctl != nil {
		if err := a.ctl.Seek(delta); err != nil {
			a.status = err.Error()
		}
	}
}

func (a *App) changeVolume(delta int) {
	if a.ctl == nil {
		return
	}
	v := a.ctl.Volume() + delta
	if err := a.ctl.SetVolume(v); err != nil {
		a.status = err.Error()
		return
	}
	a.status = fmt.Sprintf("volume %d%%", v)
}

func (a *App) toggleFavorite() {
	p := a.top()
	if p.kind == PageNowPlaying {
		pb := a.ctl.Playback()
		if pb.SongID == "" {
			return
		}
		on := a.fav.ToggleFavorite(pb.SongID)
		_ = a.store.SaveFavorites(a.fav)
		if on {
			a.status = "favorited"
		} else {
			a.status = "unfavorited"
		}
		return
	}
	if len(p.items) == 0 || p.items[p.sel].Song == nil {
		return
	}
	on := a.fav.ToggleFavorite(p.items[p.sel].Song.ID)
	_ = a.store.SaveFavorites(a.fav)
	if on {
		a.status = "favorited"
	} else {
		a.status = "unfavorited"
	}
}

// pageJump implements L1/R1: previous/next page inside the current list.
func (a *App) pageJump(dir int) {
	p := a.top()
	switch p.kind {
	case PageSongs, PageAlbums, PageArtists, PageFavorites, PageRecent, PageQueue:
		if dir > 0 {
			a.next()
		} else {
			a.prev()
		}
		return
	}
	if dir > 0 {
		a.confirm()
	}
}

// ---- rendering -------------------------------------------------------------

// Draw renders the current frame.
func (a *App) Draw() {
	w, h := a.screen.Size()
	a.screen.Clear()

	styles := newStyles()
	if a.off {
		a.screen.Show()
		return
	}
	if a.locked {
		a.drawLocked(w, h, styles)
		a.screen.Show()
		return
	}

	a.fill(0, 0, w, h, styles.bg)
	a.drawStatus(w, styles)

	p := a.top()
	leftW := LeftWidth
	if w < MinWidth {
		leftW = w // compact mode: list only
	}
	a.drawList(p, leftW, h, styles)
	if w >= MinWidth {
		a.drawDetail(p, leftW+1, w, h, styles)
	}
	a.drawHints(p, w, h, styles)

	a.screen.Show()
}

func (a *App) fill(x, y, w, h int, st tcell.Style) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			a.screen.SetContent(i, j, ' ', nil, st)
		}
	}
}

func (a *App) put(x, y int, s string, st tcell.Style) {
	for _, r := range s {
		if r == '\n' {
			continue
		}
		a.screen.SetContent(x, y, r, nil, st)
		x += runeWidth(r)
	}
}

func (a *App) drawStatus(w int, st styles) {
	pb := state.Playback{}
	if a.ctl != nil {
		pb = a.ctl.Playback()
	}
	left := "♪ Sonata   " + a.top().title + "   mpv: " + string(pb.Status)
	right := ""
	if a.caps.HasBattery {
		right = fmt.Sprintf("BAT %d%%", a.caps.BatteryPercent)
	}
	clock := a.caps.Clock().Format("15:04")
	right = strings.TrimSpace(right + "  " + clock)
	a.fill(0, 0, w, 1, st.status)
	a.put(0, 0, truncate(left, w-displayWidth(right)-2), st.status)
	a.put(w-displayWidth(right), 0, right, st.status)
}

func (a *App) drawList(p *page, width, h int, st styles) {
	// Row 1 is the list header; rows 2..h-2 are items.
	a.put(0, 1, truncate(p.title, width), st.header)
	rows := h - 3
	if rows < 1 {
		rows = 1
	}
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+rows {
		p.top = p.sel - rows + 1
	}
	for i := 0; i < rows; i++ {
		idx := p.top + i
		if idx >= len(p.items) {
			break
		}
		label := "  " + p.items[idx].Label
		style := st.text
		if idx == p.sel {
			label = "> " + p.items[idx].Label
			style = st.selected
		}
		a.put(0, 2+i, truncate(label, width), style)
	}
	// Vertical separator.
	if width < a.screenWidth() {
		for y := 1; y < h-1; y++ {
			a.screen.SetContent(width, y, '│', nil, st.dim)
		}
	}
}

func (a *App) screenWidth() int {
	w, _ := a.screen.Size()
	return w
}

func (a *App) drawDetail(p *page, x, w, h int, st styles) {
	lines := a.detailLines(p)
	for i, line := range lines {
		if 1+i >= h-1 {
			break
		}
		a.put(x+1, 1+i, truncate(line, w-x-2), st.text)
	}
}

func (a *App) detailLines(p *page) []string {
	if p.kind == PageNowPlaying {
		return a.nowPlayingLines()
	}
	if p.kind == PageMain {
		c := a.lib.Counts()
		lines := []string{
			"Library",
			"",
			fmt.Sprintf("%d songs · %d artists · %d albums", c.Songs, c.Artists, c.Albums),
		}
		if !a.lib.GeneratedAt.IsZero() {
			lines = append(lines, "Scanned "+a.lib.GeneratedAt.Local().Format("2006-01-02 15:04"))
		}
		if c.Skipped > 0 {
			lines = append(lines, fmt.Sprintf("%d files skipped", c.Skipped))
		}
		lines = append(lines, "", a.lib.Root)
		if recent := a.hist.RecentIDs(3); len(recent) > 0 {
			lines = append(lines, "", "Last played")
			for _, id := range recent {
				if s, ok := a.lib.Song(id); ok {
					lines = append(lines, "  ♫ "+s.Artist+" — "+s.Title)
				}
			}
		}
		return lines
	}
	if len(p.items) == 0 {
		return []string{"Nothing here yet."}
	}
	it := p.items[p.sel]
	switch {
	case it.Song != nil:
		s := it.Song
		lines := []string{s.Title, "", s.Artist + " · " + s.Album}
		if s.Track > 0 {
			lines = append(lines, fmt.Sprintf("Track %02d", s.Track))
		}
		lines = append(lines, "", "Length: "+s.DisplayDuration(), "Format: "+s.Format, s.Path)
		return lines
	case it.Album != nil:
		lines := []string{it.Album.Name, it.Album.Artist, "", "Tracks"}
		for i, s := range a.lib.AlbumSongs(it.Album.Name, it.Album.Artist) {
			lines = append(lines, fmt.Sprintf("%02d %-24s %s", i+1, truncate(s.Title, 24), s.DisplayDuration()))
		}
		return lines
	case it.Artist != nil:
		lines := []string{it.Artist.Name, fmt.Sprintf("%d albums · %d tracks", len(it.Artist.Albums), len(it.Artist.SongIDs)), "", "Albums"}
		for _, name := range it.Artist.Albums {
			lines = append(lines, "  "+name)
		}
		return lines
	}
	return []string{it.Label}
}

func (a *App) nowPlayingLines() []string {
	if a.ctl == nil {
		return []string{"mpv unavailable", "", "Install mpv to enable playback."}
	}
	pb := a.ctl.Playback()
	if pb.SongID == "" {
		return []string{"Nothing playing", "", "Pick a song from All Songs."}
	}
	bar := progressBar(pb.Position, pb.Duration, 34)
	lines := []string{
		pb.Title,
		"",
		pb.Artist + " · " + pb.Album,
		"",
		bar,
		fmt.Sprintf("%s / %s", mmss(pb.Position), mmss(pb.Duration)),
		"",
		"Mode: " + player.ParseMode(pb.Mode).Label(),
		fmt.Sprintf("Volume: %d%%", pb.Volume),
	}
	if a.fav.Has(pb.SongID) {
		lines = append(lines, "", "★ Favorite")
	}
	return lines
}

func progressBar(pos, dur float64, width int) string {
	if width < 4 {
		width = 4
	}
	filled := 0
	if dur > 0 {
		filled = int(float64(width) * (pos / dur))
		if filled < 0 {
			filled = 0
		}
		if filled > width {
			filled = width
		}
	}
	return "[" + strings.Repeat("█", filled) + "●" + strings.Repeat("░", width-filled-1) + "]"
}

func mmss(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	t := int(sec)
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

func (a *App) drawHints(p *page, w, h int, st styles) {
	hint := "[A] Play  [B] Back  [D-PAD] Move  [START] Play/Pause  [L1/R1] Prev/Next  [SELECT] ★"
	switch p.kind {
	case PageMain:
		hint = "[A] Open  [B] Back  [D-PAD] Move  [START] Play/Pause  [L] Lock  [O] Screen off"
	case PageNowPlaying:
		hint = "[A] Play/Pause  [B] Back  [←/→] Seek 10s  [M] Mode  [+/-] Volume  [SELECT] ★"
	case PageQueue:
		hint = "[A] Play  [B] Back  [D-PAD] Move  [L1/R1] Prev/Next  [START] Pause"
	}
	if a.status != "" {
		hint = a.status
	}
	a.fill(0, h-1, w, 1, st.hint)
	a.put(0, h-1, truncate(hint, w), st.hint)
}

func (a *App) drawLocked(w, h int, st styles) {
	a.fill(0, 0, w, h, st.lockBg)
	a.put(2, 1, "🔒 LOCKED", st.lockTitle)
	clock := a.caps.Clock().Format("15:04")
	a.put(w-displayWidth(clock)-2, 1, clock, st.lockTitle)

	pb := state.Playback{}
	if a.ctl != nil {
		pb = a.ctl.Playback()
	}
	mid := h / 2
	center := func(y int, s string, style tcell.Style) {
		x := (w - displayWidth(s)) / 2
		if x < 0 {
			x = 0
		}
		a.put(x, y, s, style)
	}
	center(mid-4, pb.Title, st.lockTitle)
	center(mid-3, pb.Artist, st.text)
	center(mid-1, progressBar(pb.Position, pb.Duration, 34), st.text)
	center(mid, fmt.Sprintf("%s / %s", mmss(pb.Position), mmss(pb.Duration)), st.dim)
	center(mid+2, "[ ◀◀ ]      [ ▶ / ⏸ ]      [ ▶▶ ]", st.text)
	center(h-3, "Hold L1 + R1 to unlock", st.dim)
}

// ScreenOff blanks the panel (auto screen-off timer entry point).
func (a *App) ScreenOff() { a.screenOff() }
