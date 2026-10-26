package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/iamcheyan/sonata/internal/player"
	"github.com/iamcheyan/sonata/internal/state"
)

func TestPlayFlowKeepsPlayerPage(t *testing.T) {
	ctl := &fakeController{volume: 70, mode: player.ModeOrder}
	app, _ := newTestApp(t, ctl)
	app.HandleKey(key(tcell.KeyEnter, 0))
	if got := app.top().kind; got != PageSongs {
		t.Fatalf("after open: %v", got)
	}
	app.HandleKey(key(tcell.KeyEnter, 0))
	t.Logf("stack=%d kind=%v status=%q", len(app.stack), app.top().kind, app.status)
	if app.top().kind != PageNowPlaying {
		t.Fatalf("expected player page, got %v (status %q)", app.top().kind, app.status)
	}
	if ctl.pb.Status != state.StatusPlaying {
		t.Fatalf("not playing: %+v", ctl.pb)
	}
}
