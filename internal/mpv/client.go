// Package mpv drives an external mpv process over its JSON IPC socket.
//
// Sonata never decodes audio: mpv owns playback, this package owns the control
// channel (load, pause, seek, volume, progress events).
package mpv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// ErrTimeout is returned when mpv does not answer within the command timeout.
var ErrTimeout = errors.New("mpv command timed out")

// Response is one reply from mpv.
type Response struct {
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	RequestID int64           `json:"request_id"`
	Event     string          `json:"event"`
	Reason    string          `json:"reason"`
	Name      string          `json:"name"`
}

// Event is a property/file event pushed by mpv.
type Event struct {
	Event  string
	Reason string
	Name   string
	Data   any
}

// Client is a connected mpv IPC client. It is safe for concurrent use.
type Client struct {
	conn net.Conn

	writeMu sync.Mutex
	reqID   int64

	pendingMu sync.Mutex
	pending   map[int64]chan Response

	events chan Event
	closed chan struct{}
	once   sync.Once
	err    error
}

// Dial connects to an existing mpv socket.
func Dial(sock string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	conn, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    conn,
		pending: make(map[int64]chan Response),
		events:  make(chan Event, 64),
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Events returns the event stream. It is closed when the connection drops.
func (c *Client) Events() <-chan Event { return c.events }

// Err reports the terminal read error, if any.
func (c *Client) Err() error {
	select {
	case <-c.closed:
		return c.err
	default:
		return nil
	}
}

// Close shuts the connection down.
func (c *Client) Close() error {
	var err error
	c.once.Do(func() {
		close(c.closed)
		err = c.conn.Close()
	})
	return err
}

// Command sends a command and returns its data payload.
func (c *Client) Command(args ...any) (json.RawMessage, error) {
	return c.CommandTimeout(2*time.Second, args...)
}

// CommandTimeout sends a command with an explicit timeout.
func (c *Client) CommandTimeout(timeout time.Duration, args ...any) (json.RawMessage, error) {
	c.writeMu.Lock()
	c.reqID++
	id := c.reqID
	ch := make(chan Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	payload := map[string]any{"command": args, "request_id": id}
	line, err := json.Marshal(payload)
	if err != nil {
		c.forget(id)
		c.writeMu.Unlock()
		return nil, err
	}
	line = append(line, '\n')
	if _, err := c.conn.Write(line); err != nil {
		c.forget(id)
		c.writeMu.Unlock()
		return nil, err
	}
	c.writeMu.Unlock()

	select {
	case resp := <-ch:
		if resp.Error != "" && resp.Error != "success" {
			return resp.Data, fmt.Errorf("mpv: %s", resp.Error)
		}
		return resp.Data, nil
	case <-time.After(timeout):
		c.forget(id)
		return nil, ErrTimeout
	case <-c.closed:
		return nil, errors.New("mpv connection closed")
	}
}

// SetProperty sets an mpv property.
func (c *Client) SetProperty(name string, value any) error {
	_, err := c.Command("set_property", name, value)
	return err
}

// GetProperty reads an mpv property as a decoded value.
func (c *Client) GetProperty(name string) (any, error) {
	data, err := c.Command("get_property", name)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// Observe subscribes to a property; events carry the observation ID.
func (c *Client) Observe(id int, name string) error {
	_, err := c.Command("observe_property", id, name)
	return err
}

// LoadFile replaces the current file.
func (c *Client) LoadFile(path string) error {
	_, err := c.Command("loadfile", path, "replace")
	return err
}

// Seek jumps to an absolute position in seconds.
func (c *Client) Seek(seconds float64) error {
	_, err := c.Command("seek", seconds, "absolute+exact")
	return err
}

// Pause sets the pause property.
func (c *Client) Pause(paused bool) error { return c.SetProperty("pause", paused) }

// Quit asks mpv to exit.
func (c *Client) Quit() error {
	_, err := c.CommandTimeout(time.Second, "quit")
	return err
}

func (c *Client) forget(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.Event != "" {
			c.dispatchEvent(resp)
			continue
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[resp.RequestID]
		delete(c.pending, resp.RequestID)
		c.pendingMu.Unlock()
		if ok {
			ch <- resp
		}
	}
	c.once.Do(func() {
		c.err = scanner.Err()
		close(c.closed)
		close(c.events)
	})
}

func (c *Client) dispatchEvent(resp Response) {
	var data any
	if len(resp.Data) > 0 {
		_ = json.Unmarshal(resp.Data, &data)
	}
	select {
	case c.events <- Event{Event: resp.Event, Reason: resp.Reason, Name: resp.Name, Data: data}:
	default:
		// Slow consumer: drop the event rather than blocking the reader. The UI
		// polls properties on a timer anyway, so dropped progress ticks are safe.
	}
}
