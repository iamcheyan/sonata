package library

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// probeDuration asks ffprobe for the track length. dhowden/tag does not expose
// duration, and decoding audio ourselves is explicitly out of scope, so when
// ffprobe is unavailable we return 0 and the UI shows "--:--" instead of a
// guessed number.
func probeDuration(path string) time.Duration {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second)).Round(time.Second)
}
