package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// displayWidth returns the terminal cell width of s. CJK characters occupy two
// cells, which is why every layout calculation must go through this helper
// instead of len() or a rune count.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	w := runewidth.RuneWidth(r)
	if w <= 0 {
		w = 1
	}
	return w
}

// truncate cuts s to at most max cells, appending an ellipsis when it had to
// shorten the string.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if displayWidth(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := runeWidth(r)
		if used+w > max-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// padRight pads s with spaces to exactly width cells (truncating if needed).
func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = truncate(s, width)
	return s + strings.Repeat(" ", width-displayWidth(s))
}
