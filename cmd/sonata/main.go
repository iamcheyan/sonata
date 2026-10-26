// Command sonata is the music library, playlist and playback controller.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/iamcheyan/sonata/internal/capabilities"
	"github.com/iamcheyan/sonata/internal/library"
	"github.com/iamcheyan/sonata/internal/mpv"
	"github.com/iamcheyan/sonata/internal/player"
	"github.com/iamcheyan/sonata/internal/service"
	"github.com/iamcheyan/sonata/internal/state"
	"github.com/iamcheyan/sonata/internal/ui"
)

const usage = `sonata — music library, playlist and playback controller (mpv-backed)

usage:
  sonata [flags]                 open the terminal UI
  sonata scan [flags]            scan the music directory and write the cache
  sonata status [--json]         print the playback state
  sonata play <file|dir>         play a file or directory immediately
  sonata stop                    stop playback and quit mpv

flags:
  --music <dir>      music directory (default: config musicDir)
  --data-dir <dir>   state directory (default: $XDG_DATA_HOME/sonata)
  --refresh          force a rescan even if the cache is fresh
  --ao <driver>      mpv audio driver (e.g. null for tests)
  --dump <dir>       render UI frames as text (no terminal required)
  -v                 verbose
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sonata:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "ui"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	musicDir := fs.String("music", "", "music directory")
	dataDir := fs.String("data-dir", "", "state directory")
	refresh := fs.Bool("refresh", false, "force rescan")
	ao := fs.String("ao", "", "mpv audio driver")
	jsonOut := fs.Bool("json", false, "machine readable output")
	verbose := fs.Bool("v", false, "verbose logging")
	dumpDir := fs.String("dump", "", "render UI frames as text into this directory")
	// Allow flags after positional arguments (`sonata play song.mp3 --data-dir x`):
	// the flag package stops at the first non-flag token, so split explicitly.
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			fmt.Print(usage)
			return nil
		}
	}
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}

	switch cmd {
	case "ui":
		if *dumpDir != "" {
			return runDump(*musicDir, *dataDir, *refresh, *dumpDir)
		}
		return runUI(*musicDir, *dataDir, *refresh, *ao, *verbose)
	case "scan":
		return runScan(*musicDir, *dataDir, *refresh)
	case "status":
		return runStatus(*dataDir, *jsonOut)
	case "play":
		if fs.NArg() == 0 {
			return fmt.Errorf("play needs a file or directory")
		}
		return runPlay(fs.Arg(0), *musicDir, *dataDir, *ao)
	case "stop":
		return runStop(*dataDir)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// reorderFlags moves flag tokens (and their values) ahead of positional ones.
func reorderFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			// A flag that is not of the -k=v form takes the next token as value,
			// except for the boolean -v/--json style flags.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				if a != "-v" && a != "--v" && a != "--json" && a != "--refresh" && a != "--dump" {
					flags = append(flags, args[i+1])
					i++
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}

// resolveDirs loads the config and applies flag overrides.
func resolveDirs(musicFlag, dataFlag string) (state.Config, *state.Store, string, error) {
	dir := dataFlag
	if dir == "" {
		dir = defaultDataDir()
	}
	store, err := state.Open(dir)
	if err != nil {
		return state.Config{}, nil, "", err
	}
	cfg, err := store.LoadConfig()
	if err != nil && err != state.ErrNewerFormat {
		return cfg, store, dir, err
	}
	if musicFlag != "" {
		cfg.MusicDir = musicFlag
	}
	if cfg.MusicDir == "" {
		cfg.MusicDir = defaultMusicDir()
	}
	return cfg, store, dir, nil
}

func defaultDataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "sonata")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/storage/sonata"
	}
	return filepath.Join(home, ".local", "share", "sonata")
}

func defaultMusicDir() string {
	if st, err := os.Stat("/storage/music"); err == nil && st.IsDir() {
		return "/storage/music"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Music")
}

func runScan(musicFlag, dataFlag string, refresh bool) error {
	cfg, _, dir, err := resolveDirs(musicFlag, dataFlag)
	if err != nil {
		return err
	}
	cache := filepath.Join(dir, "..", "cache", "sonata", "library.json")
	if !refresh {
		if lib, err := library.LoadCache(cache); err == nil && library.Fresh(lib, cfg.MusicDir) {
			c := lib.Counts()
			fmt.Printf("cached: %d songs, %d artists, %d albums (%s)\n", c.Songs, c.Artists, c.Albums, lib.GeneratedAt.Format(time.RFC3339))
			return nil
		}
	}
	start := time.Now()
	lib, err := library.Scan(cfg.MusicDir, library.ScanOptions{Cache: cache})
	if err != nil {
		return err
	}
	c := lib.Counts()
	fmt.Printf("scanned %s in %s\n", cfg.MusicDir, time.Since(start).Round(time.Millisecond))
	fmt.Printf("%d songs, %d artists, %d albums, %d skipped\n", c.Songs, c.Artists, c.Albums, c.Skipped)
	for _, s := range lib.Skipped {
		fmt.Println("  skipped:", s)
	}
	return nil
}

func runStatus(dataFlag string, jsonOut bool) error {
	_, store, _, err := resolveDirs("", dataFlag)
	if err != nil {
		return err
	}
	pb, err := store.LoadPlayback()
	if err != nil && err != state.ErrNewerFormat {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(pb)
	}
	if pb.SongID == "" {
		fmt.Println("stopped")
		return nil
	}
	fmt.Printf("%s — %s\n", pb.Title, pb.Artist)
	fmt.Printf("%s  %s / %s  mode=%s volume=%d%%\n",
		pb.Status,
		service.FormatPosition(pb.Position),
		service.FormatPosition(pb.Duration),
		pb.Mode, pb.Volume)
	if pb.MpvPID > 0 {
		fmt.Printf("mpv pid %d (%s)\n", pb.MpvPID, map[bool]string{true: "alive", false: "dead"}[mpv.Alive(pb.MpvPID)])
	}
	return nil
}

func runPlay(target, musicFlag, dataFlag, ao string) error {
	cfg, store, dir, err := resolveDirs(musicFlag, dataFlag)
	if err != nil {
		return err
	}
	svc := service.New(store, cfg, nil)
	extra := []string{"--ao=" + ao}
	if ao == "" {
		extra = nil
	}
	if err := svc.Connect(service.SocketFor(dir), service.LogPath(dir), extra); err != nil {
		return err
	}
	defer svc.Close()

	st, err := os.Stat(target)
	if err != nil {
		return err
	}
	var songs []library.Song
	if st.IsDir() {
		lib, err := library.Scan(target, library.ScanOptions{})
		if err != nil {
			return err
		}
		songs = lib.Songs
	} else {
		title, track := library.FallbackTitle(filepath.Base(target))
		abs, _ := filepath.Abs(target)
		songs = []library.Song{{
			ID:     library.SongID(filepath.Base(target)),
			Path:   abs,
			Title:  title,
			Track:  track,
			Artist: filepath.Base(filepath.Dir(abs)),
			Album:  filepath.Base(filepath.Dir(abs)),
			Format: strings.ToUpper(strings.TrimPrefix(filepath.Ext(target), ".")),
		}}
	}
	if len(songs) == 0 {
		return fmt.Errorf("no playable audio in %s", target)
	}
	if err := svc.PlaySongs(songs, 0); err != nil {
		return err
	}
	fmt.Printf("playing %s (%d tracks)\n", songs[0].Title, len(songs))
	return nil
}

func runStop(dataFlag string) error {
	_, _, dir, err := resolveDirs("", dataFlag)
	if err != nil {
		return err
	}
	sock := service.SocketFor(dir)
	cl, err := mpv.Dial(sock, 500*time.Millisecond)
	if err != nil {
		fmt.Println("not playing")
		return nil
	}
	_ = cl.Quit()
	_ = cl.Close()
	fmt.Println("stopped")
	return nil
}

func runUI(musicFlag, dataFlag string, refresh bool, ao string, verbose bool) error {
	cfg, store, dir, err := resolveDirs(musicFlag, dataFlag)
	if err != nil {
		return err
	}
	cache := filepath.Join(dir, "..", "cache", "sonata", "library.json")
	var lib *library.Library
	if !refresh {
		if cached, err := library.LoadCache(cache); err == nil && library.Fresh(cached, cfg.MusicDir) {
			lib = cached
		}
	}
	if lib == nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "scanning %s…\n", cfg.MusicDir)
		}
		lib, err = library.Scan(cfg.MusicDir, library.ScanOptions{Cache: cache})
		if err != nil {
			return err
		}
	}

	fav, err := store.LoadFavorites()
	if err != nil && err != state.ErrNewerFormat {
		return err
	}
	hist, err := store.LoadHistory()
	if err != nil && err != state.ErrNewerFormat {
		return err
	}

	svc := service.New(store, cfg, lib)
	extra := []string{"--ao=" + ao}
	if ao == "" {
		extra = nil
	}
	if err := svc.Connect(service.SocketFor(dir), service.LogPath(dir), extra); err != nil {
		// Browsing still works; the UI shows "mpv unavailable".
		fmt.Fprintln(os.Stderr, "warning:", err)
	} else if err := svc.Resume(); err != nil && verbose {
		fmt.Fprintln(os.Stderr, "resume:", err)
	}
	defer svc.Close()

	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.SetStyle(tcell.StyleDefault)

	caps := capabilities.Probe(svc.Available())
	app := ui.New(screen, lib, store, cfg, fav, hist, svc, ui.Capabilities{
		BatteryPercent: caps.BatteryPercent,
		HasBattery:     caps.HasBattery,
		Backlight:      caps.Backlight,
	})

	events := make(chan tcell.Event)
	go screen.ChannelEvents(events, nil)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastInput := time.Now()

	for {
		select {
		case ev := <-events:
			switch e := ev.(type) {
			case *tcell.EventResize:
				screen.Sync()
			case *tcell.EventKey:
				lastInput = time.Now()
				if e.Key() == tcell.KeyCtrlC {
					return nil
				}
				app.HandleKey(e)
			}
			app.Draw()
			if app.Quit() {
				return nil
			}
		case <-ticker.C:
			if screenOffDue(cfg.AutoScreenOff, lastInput) {
				app.ScreenOff()
				lastInput = time.Now()
			}
			app.Draw()
		}
	}
}

// screenOffDue reports whether the idle timeout elapsed.
func screenOffDue(setting string, lastInput time.Time) bool {
	d := parseDuration(setting)
	if d <= 0 {
		return false
	}
	return time.Since(lastInput) > d
}

func parseDuration(s string) time.Duration {
	switch s {
	case "", "never", "Never", "off":
		return 0
	case "30s":
		return 30 * time.Second
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return time.Minute
}

// runDump renders deterministic UI frames for docs and CI. Playback state is
// replayed from state.json, so no audio device or mpv process is required.
func runDump(musicFlag, dataFlag string, refresh bool, outDir string) error {
	cfg, store, dir, err := resolveDirs(musicFlag, dataFlag)
	if err != nil {
		return err
	}
	cache := filepath.Join(dir, "..", "cache", "sonata", "library.json")
	var lib *library.Library
	if !refresh {
		if cached, err := library.LoadCache(cache); err == nil && library.Fresh(cached, cfg.MusicDir) {
			lib = cached
		}
	}
	if lib == nil {
		lib, err = library.Scan(cfg.MusicDir, library.ScanOptions{Cache: cache})
		if err != nil {
			return err
		}
	}
	fav, err := store.LoadFavorites()
	if err != nil && err != state.ErrNewerFormat {
		return err
	}
	hist, err := store.LoadHistory()
	if err != nil && err != state.ErrNewerFormat {
		return err
	}
	pb, err := store.LoadPlayback()
	if err != nil && err != state.ErrNewerFormat {
		return err
	}
	if pb.SongID == "" && len(lib.Songs) > 0 {
		// Seed a representative now-playing state from the library.
		s := lib.Songs[0]
		pos := 72.0
		if dur := s.Duration.Seconds(); dur > 0 && pos > dur*0.6 {
			pos = dur * 0.4
		}
		pb = state.Playback{
			Status: state.StatusPlaying, SongID: s.ID, Path: s.Path,
			Title: s.Title, Artist: s.Artist, Album: s.Album,
			Position: pos, Duration: s.Duration.Seconds(),
			Mode: "order", Volume: cfg.Volume,
		}
	}
	ctl := &ui.StaticController{Pb: pb, Songs: lib.Songs, PlayMode: player.ParseMode(pb.Mode)}
	caps := capabilities.Probe(true)
	clock := time.Date(2026, 10, 26, 21, 45, 0, 0, time.Local)
	if err := ui.Dump(outDir, lib, store, cfg, fav, hist, ctl, ui.Capabilities{
		BatteryPercent: caps.BatteryPercent,
		HasBattery:     caps.HasBattery,
		Backlight:      caps.Backlight,
		Clock:          func() time.Time { return clock },
	}); err != nil {
		return err
	}
	fmt.Println(ui.DumpSummary(outDir))
	return nil
}
