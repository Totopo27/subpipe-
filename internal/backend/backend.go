// Package backend defines the vendor-neutral contract every subpipe backend
// implements, and the Registry that routes "<backend>/<model>" ids to them.
//
// A backend drives one vendor's official local client (see ADR 0002). Nothing
// in this package knows about any specific vendor.
package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownModel means a model id has no "<backend>/" prefix, names an
// unregistered backend, or has an empty model part.
var ErrUnknownModel = errors.New("unknown model")

// Message is one chat turn. Role is "system", "user" or "assistant".
type Message struct {
	Role    string
	Content string
}

// Request is a vendor-neutral chat completion request. Model is the bare
// model id, already stripped of its backend prefix.
type Request struct {
	Model    string
	Messages []Message
}

// Response is the result of a unary completion. FinishReason uses OpenAI
// values: "stop", "length" or "content_filter".
type Response struct {
	Content      string
	FinishReason string
}

// StreamChunk is one incremental event yielded during streaming. Content
// holds a partial text fragment. FinishReason is set only on the terminal
// chunk ("stop", "length", "content_filter"); it is empty while streaming.
type StreamChunk struct {
	Content      string
	FinishReason string
}

// Model is one catalogue entry.
type Model struct {
	ID      string
	OwnedBy string
}

// Health holds status information for one backend.
type Health struct {
	Alive         bool   `json:"alive"`
	Initialized   bool   `json:"initialized"`
	Authenticated bool   `json:"authenticated"`
	Details       string `json:"details,omitempty"`
}

// Backend drives one vendor's official local client.
type Backend interface {
	// Name is the routing prefix; it must be non-empty and contain no "/".
	Name() string
	// Models lists bare model ids this backend serves.
	Models(ctx context.Context) ([]Model, error)
	// Complete runs a unary chat completion.
	Complete(ctx context.Context, req Request) (Response, error)
	// Stream runs a streaming chat completion, calling onChunk for each
	// incoming token fragment and once at the end with FinishReason.
	Stream(ctx context.Context, req Request, onChunk func(StreamChunk) error) error
	// Health reports the current status of the backend.
	Health(ctx context.Context) (Health, error)
}

// Registry routes prefixed model ids to registered backends.
type Registry struct {
	order  []Backend
	byName map[string]Backend
}

// NewRegistry registers backends in the given order, which is also the order
// of the merged model catalogue.
func NewRegistry(backends ...Backend) (*Registry, error) {
	r := &Registry{byName: make(map[string]Backend, len(backends))}
	for _, b := range backends {
		name := b.Name()
		if name == "" || strings.Contains(name, "/") {
			return nil, fmt.Errorf("invalid backend name %q", name)
		}
		if _, dup := r.byName[name]; dup {
			return nil, fmt.Errorf("duplicate backend %q", name)
		}
		r.byName[name] = b
		r.order = append(r.order, b)
	}
	return r, nil
}

// Resolve splits "<backend>/<model>" and returns the backend and bare model.
func (r *Registry) Resolve(id string) (Backend, string, error) {
	name, model, ok := strings.Cut(id, "/")
	if !ok || name == "" || model == "" {
		return nil, "", fmt.Errorf("%w %q: expected <backend>/<model>", ErrUnknownModel, id)
	}
	b, found := r.byName[name]
	if !found {
		return nil, "", fmt.Errorf("%w %q: no backend %q", ErrUnknownModel, id, name)
	}
	return b, model, nil
}

// Models returns every backend's catalogue with ids prefixed by backend name.
func (r *Registry) Models(ctx context.Context) ([]Model, error) {
	var out []Model
	for _, b := range r.order {
		ms, err := b.Models(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: models: %w", b.Name(), err)
		}
		for _, m := range ms {
			out = append(out, Model{ID: b.Name() + "/" + m.ID, OwnedBy: b.Name()})
		}
	}
	return out, nil
}

// Health returns health status for all registered backends.
func (r *Registry) Health(ctx context.Context) (map[string]Health, error) {
	out := make(map[string]Health, len(r.order))
	var errs []error
	for _, b := range r.order {
		h, err := b.Health(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", b.Name(), err))
			if h.Details == "" {
				h.Details = err.Error()
			}
		}
		out[b.Name()] = h
	}
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}
