package mpv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// ErrNotInstalled means mpv is not on PATH.
var ErrNotInstalled = errors.New("mpv is not installed (apt install mpv / opkg install mpv)")

// SocketPath returns the control socket path for a data directory.
// It prefers XDG_RUNTIME_DIR because the socket is not useful across reboots.
func SocketPath(dataDir string) string {
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "sonata", "mpv.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("sonata-%d", os.Getuid()), "mpv.sock")
}

// Options configures mpv startup.
type Options struct {
	Socket string
	Volume int
	// AudioDevice is passed as --audio-device when non-empty.
	AudioDevice string
	// LogPath receives mpv stderr; empty discards it.
	LogPath string
	// ExtraArgs are appended last, mainly for tests (--ao=null).
	ExtraArgs []string
}

// Available reports whether mpv can be found.
func Available() bool {
	_, err := exec.LookPath("mpv")
	return err == nil
}

// Ensure connects to a running mpv, starting one when necessary. It returns a
// client plus the process it started (nil when an existing instance was
// reused), which lets the caller keep background playback alive.
func Ensure(opts Options) (*Client, *os.Process, error) {
	if opts.Socket == "" {
		return nil, nil, errors.New("mpv: empty socket path")
	}
	if c, err := Dial(opts.Socket, 300*time.Millisecond); err == nil {
		return c, nil, nil
	}
	// Stale socket from a dead process: safe to remove, nothing is listening.
	_ = os.Remove(opts.Socket)

	if !Available() {
		return nil, nil, ErrNotInstalled
	}
	proc, err := Start(opts)
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := Dial(opts.Socket, 300*time.Millisecond)
		if err == nil {
			return c, proc, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	_ = proc.Kill()
	return nil, proc, fmt.Errorf("mpv socket not ready: %w", lastErr)
}

// Start launches mpv in idle mode with the IPC socket enabled. The process is
// detached (own session) so it survives Sonata exiting: that is what makes
// background playback work.
func Start(opts Options) (*os.Process, error) {
	bin, err := exec.LookPath("mpv")
	if err != nil {
		return nil, ErrNotInstalled
	}
	if err := os.MkdirAll(filepath.Dir(opts.Socket), 0o755); err != nil {
		return nil, err
	}
	args := []string{
		"--idle=yes",
		"--no-video",
		"--no-terminal",
		"--gapless-audio=yes",
		"--input-ipc-server=" + opts.Socket,
		"--volume=" + strconv.Itoa(clampVolume(opts.Volume)),
	}
	if opts.AudioDevice != "" {
		args = append(args, "--audio-device="+opts.AudioDevice)
	}
	args = append(args, opts.ExtraArgs...)

	cmd := exec.Command(bin, args...)
	if opts.LogPath != "" {
		if f, err := os.OpenFile(opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }() // reap; mpv outlives us when detached
	return cmd.Process, nil
}

func clampVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Alive reports whether a pid is still running.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
