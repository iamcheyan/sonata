package mpv

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeServer mimics mpv's JSON IPC: one JSON object per line, replies carry
// request_id, events are pushed asynchronously.
func fakeServer(t *testing.T, sock string, handle func(req map[string]any, enc *json.Encoder)) net.Listener {
	t.Helper()
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		enc := json.NewEncoder(conn)
		for sc.Scan() {
			var req map[string]any
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				continue
			}
			handle(req, enc)
		}
	}()
	return ln
}

func TestCommandRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "mpv.sock")
	ln := fakeServer(t, sock, func(req map[string]any, enc *json.Encoder) {
		id := req["request_id"].(float64)
		cmd := req["command"].([]any)
		switch cmd[0] {
		case "get_property":
			_ = enc.Encode(map[string]any{"data": 42.5, "error": "success", "request_id": id})
		case "set_property":
			_ = enc.Encode(map[string]any{"error": "success", "request_id": id})
		default:
			_ = enc.Encode(map[string]any{"error": "invalid parameter", "request_id": id})
		}
	})
	defer ln.Close()

	c, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	v, err := c.GetProperty("time-pos")
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := v.(float64); !ok || f != 42.5 {
		t.Fatalf("value = %#v", v)
	}
	if err := c.Pause(true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Command("nonsense"); err == nil {
		t.Fatal("expected an error for an invalid command")
	}
}

func TestEvents(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "mpv.sock")
	ln := fakeServer(t, sock, func(req map[string]any, enc *json.Encoder) {
		id := req["request_id"].(float64)
		_ = enc.Encode(map[string]any{"error": "success", "request_id": id})
		_ = enc.Encode(map[string]any{"event": "file-loaded"})
		_ = enc.Encode(map[string]any{"event": "end-file", "reason": "eof"})
	})
	defer ln.Close()

	c, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.LoadFile("/tmp/a.mp3"); err != nil {
		t.Fatal(err)
	}
	var got []Event
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case ev := <-c.Events():
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("only received %d events: %+v", len(got), got)
		}
	}
	if got[0].Event != "file-loaded" || got[1].Event != "end-file" || got[1].Reason != "eof" {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestCommandTimeout(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "mpv.sock")
	ln := fakeServer(t, sock, func(req map[string]any, enc *json.Encoder) {
		// never answer
	})
	defer ln.Close()

	c, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.CommandTimeout(150*time.Millisecond, "get_property", "time-pos"); err != ErrTimeout {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := SocketPath("/data"); got != "/run/user/1000/sonata/mpv.sock" {
		t.Fatalf("socket = %q", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got := SocketPath("/data"); got == "" {
		t.Fatal("empty socket path")
	}
}

func TestClampVolume(t *testing.T) {
	cases := map[int]int{-5: 0, 0: 0, 70: 70, 100: 100, 150: 100}
	for in, want := range cases {
		if got := clampVolume(in); got != want {
			t.Errorf("clampVolume(%d) = %d, want %d", in, got, want)
		}
	}
}
