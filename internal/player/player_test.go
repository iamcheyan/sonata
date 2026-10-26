package player

import (
	"math/rand"
	"testing"

	"github.com/iamcheyan/sonata/internal/library"
)

func songs(n int) []library.Song {
	out := make([]library.Song, n)
	for i := range out {
		out[i] = library.Song{ID: string(rune('a' + i)), Title: string(rune('A' + i))}
	}
	return out
}

func TestEmptyQueue(t *testing.T) {
	p := New(nil, ModeOrder)
	if _, err := p.Current(); err != ErrEmptyQueue {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := p.Next(); err != ErrEmptyQueue {
		t.Fatalf("next err = %v", err)
	}
	if _, err := p.Prev(); err != ErrEmptyQueue {
		t.Fatalf("prev err = %v", err)
	}
}

func TestOrderModeStopsAtEnd(t *testing.T) {
	p := New(songs(3), ModeOrder)
	seen := []string{}
	for {
		s, _ := p.Current()
		seen = append(seen, s.ID)
		next, finished, err := p.Next()
		if err != nil {
			t.Fatal(err)
		}
		if finished {
			break
		}
		if next.ID == "" {
			t.Fatal("empty next song")
		}
	}
	if len(seen) != 3 {
		t.Fatalf("played %d tracks, want 3 (%v)", len(seen), seen)
	}
}

func TestRepeatAllWraps(t *testing.T) {
	p := New(songs(2), ModeRepeatAll)
	if _, err := p.Select(1); err != nil {
		t.Fatal(err)
	}
	s, finished, err := p.Next()
	if err != nil {
		t.Fatal(err)
	}
	if finished {
		t.Fatal("repeat-all must not finish")
	}
	if s.ID != "a" {
		t.Fatalf("wrapped to %q, want a", s.ID)
	}
}

func TestRepeatOneStays(t *testing.T) {
	p := New(songs(3), ModeRepeatOne)
	if _, err := p.Select(1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		s, finished, err := p.Next()
		if err != nil || finished {
			t.Fatalf("unexpected: %v %v", err, finished)
		}
		if s.ID != "b" {
			t.Fatalf("repeat-one moved to %q", s.ID)
		}
	}
}

func TestShuffleCoversQueueWithoutRepeats(t *testing.T) {
	p := New(songs(6), ModeShuffle)
	p.SetRand(rand.New(rand.NewSource(42)))
	seen := map[string]int{}
	cur, _ := p.Current()
	seen[cur.ID]++
	for i := 0; i < 5; i++ {
		s, _, err := p.Next()
		if err != nil {
			t.Fatal(err)
		}
		seen[s.ID]++
	}
	if len(seen) != 6 {
		t.Fatalf("shuffle visited %d distinct tracks, want 6: %v", len(seen), seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("track %s played %d times in one round", id, n)
		}
	}
}

func TestPrevWrapsInRepeatAll(t *testing.T) {
	p := New(songs(3), ModeRepeatAll)
	s, err := p.Prev()
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "c" {
		t.Fatalf("prev from first = %q, want c", s.ID)
	}
}

func TestModeCycleAndLabels(t *testing.T) {
	m := ModeOrder
	want := []Mode{ModeRepeatOne, ModeRepeatAll, ModeShuffle, ModeOrder}
	for _, w := range want {
		m = m.Cycle()
		if m != w {
			t.Fatalf("cycle = %q, want %q", m, w)
		}
	}
	for _, mode := range Modes {
		if mode.Label() == "" {
			t.Errorf("mode %q has no label", mode)
		}
	}
	if ParseMode("nonsense") != ModeOrder {
		t.Error("unknown mode should fall back to order")
	}
}

func TestReplaceResetsCursor(t *testing.T) {
	p := New(songs(3), ModeOrder)
	if _, err := p.Select(2); err != nil {
		t.Fatal(err)
	}
	p.Replace(songs(2))
	if p.Index() != 0 {
		t.Fatalf("index = %d, want 0", p.Index())
	}
	if p.Len() != 2 {
		t.Fatalf("len = %d", p.Len())
	}
}
