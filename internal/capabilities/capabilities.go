// Package capabilities probes what the current device can do, so the UI can
// hide or grey out features instead of branching on a device model name.
package capabilities

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Set is a snapshot of probed capabilities.
type Set struct {
	BatteryPercent int
	HasBattery     bool
	Backlight      bool
	Audio          bool
}

// Probe reads sysfs and PATH once at startup. Missing data is reported as
// absent; nothing here invents a value.
func Probe(audioAvailable bool) Set {
	s := Set{Audio: audioAvailable}
	if pct, ok := batteryPercent(); ok {
		s.BatteryPercent = pct
		s.HasBattery = true
	}
	if paths, _ := filepath.Glob("/sys/class/backlight/*"); len(paths) > 0 {
		s.Backlight = true
	}
	return s
}

func batteryPercent() (int, bool) {
	entries, err := filepath.Glob("/sys/class/power_supply/*")
	if err != nil {
		return 0, false
	}
	for _, dir := range entries {
		name := filepath.Base(dir)
		if !strings.HasPrefix(name, "BAT") && !strings.HasPrefix(name, "battery") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "capacity"))
		if err != nil {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}

// SetBacklight powers the panel backlight off/on. It returns false when the
// device has no writable backlight node, in which case the UI falls back to a
// black overlay.
func SetBacklight(on bool) bool {
	paths, _ := filepath.Glob("/sys/class/backlight/*/bl_power")
	for _, p := range paths {
		v := []byte("4")
		if on {
			v = []byte("0")
		}
		if err := os.WriteFile(p, v, 0o644); err == nil {
			return true
		}
	}
	return false
}
