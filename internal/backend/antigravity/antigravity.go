// Package antigravity is the subpipe backend for Google Antigravity. It
// drives the official `agy acp` server (Agent Client Protocol v1 over the
// child's stdin/stdout) as one persistent child process; all network egress
// to Google comes from agy itself. See ADR 0001, revision (2).
package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"subpipe/internal/backend"
)

// ErrAuthRequired means agy has no cached login. subpipe never handles
// credentials; the user signs in with agy itself.
var ErrAuthRequired = errors.New("agy is not signed in: run `agy` interactively or `agy acp --login` once")

// DefaultModels is the catalogue reported until ticket 04 reads it from the
// agent's model selector.
var DefaultModels = []string{"gemini-3.8-pro", "gemini-3.8-flash"}

const protocolVersion = 1

// Config controls how the backend starts agy.
type Config struct {
	// Command is the agent command line; empty means ["agy", "acp"].
	Command []string
	// Env holds extra KEY=VALUE entries added to the child's environment.
	Env []string
	// WorkDir is the child's working directory and the cwd of every ACP
	// session; empty means <os.TempDir()>/subpipe-antigravity.
	WorkDir string
	// Models overrides DefaultModels.
	Models []string
	// Stderr receives the child's stderr (ACP reserves it for logging);
	// nil discards it.
	Stderr io.Writer
}

// Backend implements backend.Backend over a persistent `agy acp` child.
type Backend struct {
	cfg Config

	mu       sync.Mutex
	conn     *conn
	canClose bool // agent advertised sessionCapabilities.close
}

// New returns a backend; the child is started lazily by the first request.
func New(cfg Config) *Backend {
	if len(cfg.Command) == 0 {
		cfg.Command = []string{"agy", "acp"}
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = filepath.Join(os.TempDir(), "subpipe-antigravity")
	}
	if len(cfg.Models) == 0 {
		cfg.Models = DefaultModels
	}
	return &Backend{cfg: cfg}
}

// Name is the routing prefix.
func (b *Backend) Name() string { return "antigravity" }

// Models lists the configured catalogue.
func (b *Backend) Models(context.Context) ([]backend.Model, error) {
	out := make([]backend.Model, len(b.cfg.Models))
	for i, id := range b.cfg.Models {
		out[i] = backend.Model{ID: id, OwnedBy: b.Name()}
	}
	return out, nil
}

// Close stops the child, if any.
func (b *Backend) Close() error {
	b.mu.Lock()
	c := b.conn
	b.conn = nil
	b.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.close(2 * time.Second)
}

// connection returns the live, initialized child, starting a new one when
// there is none or the previous one exited.
func (b *Backend) connection(ctx context.Context) (*conn, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil && b.conn.alive() {
		return b.conn, b.canClose, nil
	}
	b.conn = nil
	abs, err := filepath.Abs(b.cfg.WorkDir)
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, false, err
	}
	b.cfg.WorkDir = abs
	c, err := startConn(b.cfg.Command, b.cfg.Env, abs, b.cfg.Stderr)
	if err != nil {
		return nil, false, err
	}
	var init struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			SessionCapabilities struct {
				Close *json.RawMessage `json:"close"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
	}
	err = c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		// No fs or terminal: the agent must not read or write through us.
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "subpipe", "version": "0.1.0"},
	}, &init)
	if err == nil && init.ProtocolVersion != protocolVersion {
		err = fmt.Errorf("agy acp speaks protocol version %d, want %d", init.ProtocolVersion, protocolVersion)
	}
	if err != nil {
		_ = c.close(time.Second)
		return nil, false, fmt.Errorf("initialize: %w", err)
	}
	b.conn = c
	b.canClose = init.AgentCapabilities.SessionCapabilities.Close != nil
	return c, b.canClose, nil
}

// configOption is the subset of an ACP SessionConfigOption subpipe reads.
// Select values come either flat or in groups.
type configOption struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	Type         string `json:"type"`
	CurrentValue any    `json:"currentValue"`
	Options      []struct {
		Value   string `json:"value"`
		Options []struct {
			Value string `json:"value"`
		} `json:"options"`
	} `json:"options"`
}

func (o configOption) offers(value string) bool {
	for _, v := range o.Options {
		if v.Value == value {
			return true
		}
		for _, g := range v.Options {
			if g.Value == value {
				return true
			}
		}
	}
	return false
}

// modelOption finds the model selector: category "model", else id "model".
func modelOption(opts []configOption) (configOption, bool) {
	for _, o := range opts {
		if o.Category == "model" && o.Type == "select" {
			return o, true
		}
	}
	for _, o := range opts {
		if o.ID == "model" && o.Type == "select" {
			return o, true
		}
	}
	return configOption{}, false
}

// Complete runs one prompt turn in a fresh ACP session.
func (b *Backend) Complete(ctx context.Context, req backend.Request) (backend.Response, error) {
	c, canClose, err := b.connection(ctx)
	if err != nil {
		return backend.Response{}, err
	}

	var sess struct {
		SessionID     string         `json:"sessionId"`
		ConfigOptions []configOption `json:"configOptions"`
	}
	err = c.call(ctx, "session/new", map[string]any{"cwd": b.cfg.WorkDir, "mcpServers": []any{}}, &sess)
	if err != nil {
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == codeAuthRequired {
			return backend.Response{}, fmt.Errorf("%w (%v)", ErrAuthRequired, err)
		}
		return backend.Response{}, fmt.Errorf("session/new: %w", err)
	}
	if canClose {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = c.call(closeCtx, "session/close", map[string]any{"sessionId": sess.SessionID}, nil)
		}()
	}

	if err := b.selectModel(ctx, c, sess.SessionID, sess.ConfigOptions, req.Model); err != nil {
		return backend.Response{}, err
	}

	t := c.track(sess.SessionID)
	defer c.untrack(sess.SessionID)
	var result struct {
		StopReason string `json:"stopReason"`
	}
	err = c.call(ctx, "session/prompt", map[string]any{
		"sessionId": sess.SessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": flatten(req.Messages)}},
	}, &result)
	if err != nil {
		if ctx.Err() != nil {
			_ = c.notify("session/cancel", map[string]any{"sessionId": sess.SessionID})
			return backend.Response{}, ctx.Err()
		}
		return backend.Response{}, fmt.Errorf("session/prompt: %w", err)
	}
	finish, err := finishReason(result.StopReason)
	if err != nil {
		return backend.Response{}, err
	}
	return backend.Response{Content: t.String(), FinishReason: finish}, nil
}

func (b *Backend) selectModel(ctx context.Context, c *conn, session string, opts []configOption, model string) error {
	opt, ok := modelOption(opts)
	if !ok {
		return fmt.Errorf("agy acp exposes no model selector; cannot select %q", model)
	}
	if !opt.offers(model) {
		return fmt.Errorf("%w %q: not offered by agy for this account", backend.ErrUnknownModel, model)
	}
	if opt.CurrentValue == model {
		return nil
	}
	err := c.call(ctx, "session/set_config_option", map[string]any{
		"sessionId": session, "configId": opt.ID, "value": model,
	}, nil)
	if err != nil {
		return fmt.Errorf("select model %q: %w", model, err)
	}
	return nil
}

// flatten turns the OpenAI history into one text block. A lone user message
// is sent as is; anything else is labelled by role so the agent sees the
// whole conversation in order.
func flatten(msgs []backend.Message) string {
	if len(msgs) == 1 && msgs[0].Role == "user" {
		return msgs[0].Content
	}
	parts := make([]string, len(msgs))
	for i, m := range msgs {
		parts[i] = "[" + m.Role + "]\n" + m.Content
	}
	return strings.Join(parts, "\n\n")
}

// finishReason maps an ACP stop reason to its OpenAI finish_reason. The turn
// did complete, so a reason this client does not know yet (a newer ACP
// version) keeps the text and reports "stop".
func finishReason(stop string) (string, error) {
	switch stop {
	case "end_turn":
		return "stop", nil
	case "max_tokens", "max_turn_requests":
		return "length", nil
	case "refusal":
		return "content_filter", nil
	case "cancelled":
		return "", errors.New("agy cancelled the turn")
	default:
		return "stop", nil
	}
}
