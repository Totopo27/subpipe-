// Package server is subpipe's vendor-neutral HTTP ingress: the OpenAI wire
// format on one side, backend.Registry on the other. It never imports a
// vendor package (ADR 0002).
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"subpipe/internal/backend"
)

// maxBody bounds a request body; chat histories are large but not unbounded.
const maxBody = 16 << 20

// New returns the HTTP handler serving the OpenAI-compatible API.
func New(reg *backend.Registry) http.Handler {
	s := &server{reg: reg, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.chatCompletions)
	return mux
}

type server struct {
	reg *backend.Registry
	now func() time.Time
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// text returns the message content, which OpenAI sends either as a string or
// as an array of parts. Only text parts are supported.
func (m chatMessage) text() (string, error) {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return "", errors.New("content must be a string or an array of text parts")
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type != "text" {
			return "", fmt.Errorf("content part type %q is not supported", p.Type)
		}
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

func (s *server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error(), "", "")
		return
	}
	if req.Stream {
		writeError(w, http.StatusBadRequest, "stream: true is not supported yet", "stream", "")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages must not be empty", "messages", "")
		return
	}
	msgs := make([]backend.Message, len(req.Messages))
	for i, m := range req.Messages {
		switch m.Role {
		case "system", "developer", "user", "assistant":
		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("messages[%d]: role %q is not supported", i, m.Role), "messages", "")
			return
		}
		text, err := m.text()
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("messages[%d]: %v", i, err), "messages", "")
			return
		}
		role := m.Role
		if role == "developer" {
			role = "system"
		}
		msgs[i] = backend.Message{Role: role, Content: text}
	}

	b, model, err := s.reg.Resolve(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "model", "model_not_found")
		return
	}
	resp, err := b.Complete(r.Context(), backend.Request{Model: model, Messages: msgs})
	switch {
	case errors.Is(err, backend.ErrUnknownModel):
		writeError(w, http.StatusBadRequest, err.Error(), "model", "model_not_found")
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, b.Name()+": "+err.Error(), "", "")
		return
	}

	finish := resp.FinishReason
	if finish == "" {
		finish = "stop"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-" + randomID(),
		"object":  "chat.completion",
		"created": s.now().Unix(),
		"model":   req.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": resp.Content},
			"finish_reason": finish,
		}},
	})
}

// writeError writes an OpenAI-style error body. Empty param or code are
// encoded as null, as OpenAI does.
func writeError(w http.ResponseWriter, status int, msg, param, code string) {
	typ := "invalid_request_error"
	if status >= 500 {
		typ = "api_error"
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"message": msg,
		"type":    typ,
		"param":   nullable(param),
		"code":    nullable(code),
	}})
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
