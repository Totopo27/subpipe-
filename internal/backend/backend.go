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

// Response is the result of a unary completion.
type Response struct {
	Content string
}

// Model is one catalogue entry.
type Model struct {
	ID      string
	OwnedBy string
}

// Backend drives one vendor's official local client.
type Backend interface {
	// Name is the routing prefix; it must be non-empty and contain no "/".
	Name() string
	// Models lists bare model ids this backend serves.
	Models(ctx context.Context) ([]Model, error)
	// Complete runs a unary chat completion.
	Complete(ctx context.Context, req Request) (Response, error)
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
