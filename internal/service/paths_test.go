package service

import (
	"path/filepath"
	"testing"
)

func TestLogPathUsesSiblingLogsTree(t *testing.T) {
	data := "/storage/heya/data/sonata"
	want := filepath.Clean("/storage/heya/logs/sonata/mpv.log")
	if got := filepath.Clean(LogPath(data)); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}
