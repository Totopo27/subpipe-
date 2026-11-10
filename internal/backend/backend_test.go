package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fake struct {
	name   string
	models []Model
	got    Request
}

func (f *fake) Name() string                            { return f.name }
func (f *fake) Models(context.Context) ([]Model, error) { return f.models, nil }
func (f *fake) Complete(_ context.Context, r Request) (Response, error) {
	f.got = r
	return Response{Content: "ok from " + f.name}, nil
}

func TestResolveStripsPrefix(t *testing.T) {
	ag := &fake{name: "antigravity"}
	r, err := NewRegistry(ag, &fake{name: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	b, model, err := r.Resolve("antigravity/gemini-3.8-pro")
	if err != nil {
		t.Fatal(err)
	}
	if b != ag || model != "gemini-3.8-pro" {
		t.Fatalf("Resolve = (%s, %q), want (antigravity, gemini-3.8-pro)", b.Name(), model)
	}
}

func TestResolveKeepsSlashesInModel(t *testing.T) {
	r, _ := NewRegistry(&fake{name: "antigravity"})
	_, model, err := r.Resolve("antigravity/vendor/model-x")
	if err != nil || model != "vendor/model-x" {
		t.Fatalf("Resolve = (%q, %v), want vendor/model-x", model, err)
	}
}

func TestResolveRejectsUnknownOrMissingPrefix(t *testing.T) {
	r, _ := NewRegistry(&fake{name: "antigravity"})
	for _, id := range []string{"gemini-3.8-pro", "openai/gpt-5", "antigravity/", "/gemini", ""} {
		if _, _, err := r.Resolve(id); !errors.Is(err, ErrUnknownModel) {
			t.Errorf("Resolve(%q) err = %v, want ErrUnknownModel", id, err)
		}
	}
}

func TestModelsArePrefixedInRegistrationOrder(t *testing.T) {
	r, _ := NewRegistry(
		&fake{name: "antigravity", models: []Model{{ID: "gemini-3.8-pro"}, {ID: "gemini-3.8-flash"}}},
		&fake{name: "claude", models: []Model{{ID: "sonnet"}}},
	)
	got, err := r.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ID: "antigravity/gemini-3.8-pro", OwnedBy: "antigravity"},
		{ID: "antigravity/gemini-3.8-flash", OwnedBy: "antigravity"},
		{ID: "claude/sonnet", OwnedBy: "claude"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Models = %+v\nwant %+v", got, want)
	}
}

func TestNewRegistryRejectsBadNames(t *testing.T) {
	cases := [][]Backend{
		{&fake{name: "a"}, &fake{name: "a"}},
		{&fake{name: ""}},
		{&fake{name: "a/b"}},
	}
	for i, bs := range cases {
		if _, err := NewRegistry(bs...); err == nil {
			t.Errorf("case %d: NewRegistry succeeded, want error", i)
		}
	}
}
