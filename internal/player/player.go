// Package player owns the playback queue and the playback-mode state machine.
//
// It is pure logic: it never touches the filesystem or mpv directly. The caller
// translates its decisions into mpv commands, which keeps the ordering rules
// (repeat, shuffle, end-of-queue) fully testable.
package player

import (
	"errors"
	"math/rand"

	"github.com/iamcheyan/sonata/internal/library"
)

// Mode is the playback mode.
type Mode string

const (
	ModeOrder     Mode = "order"
	ModeRepeatOne Mode = "repeat-one"
	ModeRepeatAll Mode = "repeat-all"
	ModeShuffle   Mode = "shuffle"
)

// Modes lists the supported modes in cycle order.
var Modes = []Mode{ModeOrder, ModeRepeatOne, ModeRepeatAll, ModeShuffle}

// Cycle returns the next mode.
func (m Mode) Cycle() Mode {
	for i, candidate := range Modes {
		if candidate == m {
			return Modes[(i+1)%len(Modes)]
		}
	}
	return ModeOrder
}

// Label is the human-readable name used by the UI.
func (m Mode) Label() string {
	switch m {
	case ModeRepeatOne:
		return "单曲循环"
	case ModeRepeatAll:
		return "列表循环"
	case ModeShuffle:
		return "随机播放"
	default:
		return "顺序播放"
	}
}

// ParseMode accepts a stored mode string.
func ParseMode(s string) Mode {
	for _, m := range Modes {
		if string(m) == s {
			return m
		}
	}
	return ModeOrder
}

// ErrEmptyQueue is returned when an operation needs a current track but the
// queue is empty.
var ErrEmptyQueue = errors.New("queue is empty")

// Player is the queue plus a cursor.
type Player struct {
	queue   []library.Song
	index   int
	mode    Mode
	rng     *rand.Rand
	shuffle []int // precomputed shuffle order (indices into queue)
	shufPos int
	played  map[string]bool
}

// New builds a player over songs. A nil rng uses a time-seeded generator.
func New(songs []library.Song, mode Mode) *Player {
	p := &Player{queue: append([]library.Song(nil), songs...), index: 0, mode: mode, played: map[string]bool{}}
	p.rng = rand.New(rand.NewSource(1))
	if p.mode == "" {
		p.mode = ModeOrder
	}
	p.rebuildShuffle()
	return p
}

// SetRand lets tests inject a deterministic source.
func (p *Player) SetRand(r *rand.Rand) {
	p.rng = r
	p.rebuildShuffle()
}

// Queue returns the current queue (copy).
func (p *Player) Queue() []library.Song {
	return append([]library.Song(nil), p.queue...)
}

// Len reports the queue length.
func (p *Player) Len() int { return len(p.queue) }

// Index reports the cursor position.
func (p *Player) Index() int { return p.index }

// Mode reports the playback mode.
func (p *Player) Mode() Mode { return p.mode }

// SetMode switches modes and resets the shuffle order when needed.
func (p *Player) SetMode(m Mode) {
	if m == p.mode {
		return
	}
	p.mode = m
	p.rebuildShuffle()
}

// CycleMode advances to the next mode.
func (p *Player) CycleMode() Mode {
	p.SetMode(p.mode.Cycle())
	return p.mode
}

// Replace swaps the queue and selects the first song.
func (p *Player) Replace(songs []library.Song) {
	p.queue = append([]library.Song(nil), songs...)
	p.index = 0
	p.played = map[string]bool{}
	p.rebuildShuffle()
}

// Current returns the song under the cursor.
func (p *Player) Current() (library.Song, error) {
	if len(p.queue) == 0 {
		return library.Song{}, ErrEmptyQueue
	}
	if p.index < 0 || p.index >= len(p.queue) {
		p.index = 0
	}
	return p.queue[p.index], nil
}

// Select moves the cursor to i and returns that song.
func (p *Player) Select(i int) (library.Song, error) {
	if len(p.queue) == 0 {
		return library.Song{}, ErrEmptyQueue
	}
	if i < 0 {
		i = 0
	}
	if i >= len(p.queue) {
		i = len(p.queue) - 1
	}
	p.index = i
	return p.queue[p.index], nil
}

// Next advances according to the mode. finished reports that playback reached
// the end of the queue and should stop (only in order mode).
func (p *Player) Next() (song library.Song, finished bool, err error) {
	if len(p.queue) == 0 {
		return library.Song{}, false, ErrEmptyQueue
	}
	if p.mode == ModeRepeatOne {
		return p.queue[p.index], false, nil
	}
	if p.mode == ModeShuffle {
		p.shufPos++
		if p.shufPos >= len(p.shuffle) {
			// One full round done: reshuffle. The new permutation starts with
			// the track that just played, so advance past it to avoid playing
			// the same song twice in a row.
			p.rebuildShuffle()
			p.shufPos = 1
			if p.shufPos >= len(p.shuffle) {
				p.shufPos = 0
			}
		}
		p.index = p.shuffle[p.shufPos]
		return p.queue[p.index], false, nil
	}
	if p.index+1 >= len(p.queue) {
		if p.mode == ModeRepeatAll {
			p.index = 0
			return p.queue[p.index], false, nil
		}
		p.index = len(p.queue) - 1
		return p.queue[p.index], true, nil
	}
	p.index++
	return p.queue[p.index], false, nil
}

// Prev moves backwards, wrapping in the repeat-all mode.
func (p *Player) Prev() (library.Song, error) {
	if len(p.queue) == 0 {
		return library.Song{}, ErrEmptyQueue
	}
	if p.mode == ModeShuffle {
		if p.shufPos > 0 {
			p.shufPos--
		}
		p.index = p.shuffle[p.shufPos]
		return p.queue[p.index], nil
	}
	if p.index == 0 {
		if p.mode == ModeRepeatAll {
			p.index = len(p.queue) - 1
			return p.queue[p.index], nil
		}
		return p.queue[0], nil
	}
	p.index--
	return p.queue[p.index], nil
}

// rebuildShuffle creates a fresh permutation of queue indices.
func (p *Player) rebuildShuffle() {
	if len(p.queue) == 0 {
		p.shuffle = nil
		p.shufPos = 0
		return
	}
	perm := make([]int, len(p.queue))
	for i := range perm {
		perm[i] = i
	}
	if p.rng == nil {
		p.rng = rand.New(rand.NewSource(1))
	}
	p.rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
	// The round starts with the current track, so every song plays exactly once
	// per cycle and Next() never returns the current song again immediately.
	if len(perm) > 1 && perm[0] != p.index {
		for i, idx := range perm {
			if idx == p.index {
				perm[0], perm[i] = perm[i], perm[0]
				break
			}
		}
	}
	p.shuffle = perm
	p.shufPos = 0
}

// Append adds songs to the end of the queue without moving the cursor.
func (p *Player) Append(songs []library.Song) {
	p.queue = append(p.queue, songs...)
	p.rebuildShuffle()
}

// Move reorders the queue: from -> to.
func (p *Player) Move(from, to int) {
	if from < 0 || from >= len(p.queue) || to < 0 || to >= len(p.queue) || from == to {
		return
	}
	song := p.queue[from]
	p.queue = append(p.queue[:from], p.queue[from+1:]...)
	p.queue = append(p.queue[:to], append([]library.Song{song}, p.queue[to:]...)...)
	p.index = to
	p.rebuildShuffle()
}

// Remove drops one entry from the queue.
func (p *Player) Remove(index int) {
	if index < 0 || index >= len(p.queue) {
		return
	}
	p.queue = append(p.queue[:index], p.queue[index+1:]...)
	if p.index >= len(p.queue) {
		p.index = len(p.queue) - 1
	}
	if p.index < 0 {
		p.index = 0
	}
	p.rebuildShuffle()
}

// Clear empties the queue.
func (p *Player) Clear() {
	p.queue = nil
	p.index = 0
	p.played = map[string]bool{}
	p.rebuildShuffle()
}
