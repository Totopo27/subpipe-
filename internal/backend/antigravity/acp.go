package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// ACP error codes used by this client (ACP v1 "Errors").
const (
	codeMethodNotFound = -32601
	codeAuthRequired   = -32000
)

// RPCError is a JSON-RPC error object returned by the agent.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp error %d: %s", e.Code, e.Message) }

// errExited reports that the agent process ended while a call was pending.
var errExited = errors.New("agy acp exited")

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func (m *rpcMessage) hasID() bool { return len(m.ID) > 0 && string(m.ID) != "null" }

// conn is one ACP connection over a child process's stdin/stdout:
// newline-delimited JSON-RPC 2.0, as the ACP stdio transport requires.
type conn struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	wmu sync.Mutex // serialises writes to stdin

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcMessage
	turns   map[string]*turn // live prompt turns by session id
	closed  bool

	done chan struct{} // closed when stdout ends and the child is reaped
}

// turn accumulates the agent's text for one in-flight session/prompt.
type turn struct {
	mu   sync.Mutex
	text []byte
}

func (t *turn) append(s string) {
	t.mu.Lock()
	t.text = append(t.text, s...)
	t.mu.Unlock()
}

func (t *turn) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.text)
}

// startConn launches command with extra environment entries and starts
// reading its stdout.
func startConn(command, env []string, dir string) (*conn, error) {
	if len(command) == 0 {
		return nil, errors.New("empty agy command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	c := &conn{
		cmd:     cmd,
		stdin:   stdin,
		pending: map[int64]chan rpcMessage{},
		turns:   map[string]*turn{},
		done:    make(chan struct{}),
	}
	go c.readLoop(stdout)
	return c, nil
}

// alive reports whether the child is still running.
func (c *conn) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *conn) readLoop(stdout io.Reader) {
	r := bufio.NewReader(stdout)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var msg rpcMessage
			if json.Unmarshal(line, &msg) == nil {
				c.dispatch(&msg)
			}
		}
		if err != nil {
			break
		}
	}
	_ = c.cmd.Wait()
	// Mark the connection dead before waking pending callers, so a caller
	// that sees errExited and retries never gets this conn back as alive.
	c.mu.Lock()
	c.closed = true
	close(c.done)
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

func (c *conn) dispatch(msg *rpcMessage) {
	switch {
	case msg.Method == "" && msg.hasID():
		id, err := strconv.ParseInt(string(msg.ID), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- *msg
		}
	case msg.Method != "" && msg.hasID():
		c.answer(msg)
	case msg.Method == "session/update":
		c.update(msg.Params)
	}
}

// update appends agent_message_chunk text to the session's turn. Every other
// update kind (thoughts, tool calls, plans, usage) is ignored for now.
func (c *conn) update(params json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil || p.Update.SessionUpdate != "agent_message_chunk" || p.Update.Content.Type != "text" {
		return
	}
	c.mu.Lock()
	t := c.turns[p.SessionID]
	c.mu.Unlock()
	if t != nil {
		t.append(p.Update.Content.Text)
	}
}

// answer replies to a request the agent sent us. subpipe exposes no client
// capabilities, so the only request it expects is session/request_permission,
// which it always rejects: the gateway answers chats, it does not let the
// agent act on the machine.
func (c *conn) answer(msg *rpcMessage) {
	if msg.Method != "session/request_permission" {
		_ = c.write(rpcMessage{ID: msg.ID, Error: &RPCError{Code: codeMethodNotFound, Message: "Method not found"}})
		return
	}
	var p struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(msg.Params, &p)
	// Prefer a one-off rejection; with no reject option, "cancelled" is the
	// only outcome that cannot be read as approval.
	outcome := map[string]any{"outcome": "cancelled"}
	if id, ok := findOption(p.Options, "reject_once"); ok {
		outcome = map[string]any{"outcome": "selected", "optionId": id}
	} else if id, ok := findOption(p.Options, "reject_always"); ok {
		outcome = map[string]any{"outcome": "selected", "optionId": id}
	}
	result, _ := json.Marshal(map[string]any{"outcome": outcome})
	_ = c.write(rpcMessage{ID: msg.ID, Result: result})
}

func findOption(options []struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}, kind string) (string, bool) {
	for _, o := range options {
		if o.Kind == kind {
			return o.OptionID, true
		}
	}
	return "", false
}

func (c *conn) write(msg rpcMessage) error {
	msg.JSONRPC = "2.0"
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.stdin.Write(append(raw, '\n'))
	return err
}

// call sends a request and decodes its result into out (which may be nil).
// It returns ctx.Err() if ctx ends first, and errExited if the child dies.
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errExited
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(rpcMessage{ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method, Params: rawParams}); err != nil {
		c.forget(id)
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return fmt.Errorf("%s: %w", method, errExited)
		}
		if msg.Error != nil {
			return msg.Error
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(msg.Result, out)
	case <-ctx.Done():
		c.forget(id)
		return ctx.Err()
	}
}

func (c *conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// notify sends a notification (no reply expected).
func (c *conn) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(rpcMessage{Method: method, Params: raw})
}

func (c *conn) track(session string) *turn {
	t := &turn{}
	c.mu.Lock()
	c.turns[session] = t
	c.mu.Unlock()
	return t
}

func (c *conn) untrack(session string) {
	c.mu.Lock()
	delete(c.turns, session)
	c.mu.Unlock()
}

// close ends stdin (the ACP way to stop an agent) and kills the child if it
// is still running after grace.
func (c *conn) close(grace time.Duration) error {
	_ = c.stdin.Close()
	select {
	case <-c.done:
		return nil
	case <-time.After(grace):
	}
	if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-c.done
	return nil
}
