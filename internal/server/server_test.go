package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"subpipe/internal/backend"
)

type fakeBackend struct {
	got  backend.Request
	resp backend.Response
	err  error
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Models(context.Context) ([]backend.Model, error) {
	return []backend.Model{{ID: "m1"}}, nil
}
func (f *fakeBackend) Complete(_ context.Context, r backend.Request) (backend.Response, error) {
	f.got = r
	return f.resp, f.err
}

func newServer(t *testing.T, fb *fakeBackend) http.Handler {
	t.Helper()
	reg, err := backend.NewRegistry(fb)
	if err != nil {
		t.Fatal(err)
	}
	return New(reg)
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	return rec
}

type errorBody struct {
	Error struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param"`
		Code    *string `json:"code"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, rec.Body)
	}
	if e.Error.Message == "" || e.Error.Type == "" {
		t.Fatalf("not an OpenAI error body: %s", rec.Body)
	}
	return e
}

func TestChatCompletionUnary(t *testing.T) {
	fb := &fakeBackend{resp: backend.Response{Content: "hello there", FinishReason: "stop"}}
	rec := post(newServer(t, fb), `{"model":"fake/m1","messages":[
		{"role":"system","content":"be nice"},
		{"role":"user","content":[{"type":"text","text":"hi "},{"type":"text","text":"you"}]}
	],"stream":false}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var got struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.ID, "chatcmpl-") || got.Object != "chat.completion" || got.Created == 0 || got.Model != "fake/m1" {
		t.Fatalf("envelope = %+v", got)
	}
	if len(got.Choices) != 1 || got.Choices[0].Index != 0 || got.Choices[0].Message.Role != "assistant" ||
		got.Choices[0].Message.Content != "hello there" || got.Choices[0].FinishReason != "stop" {
		t.Fatalf("choices = %+v", got.Choices)
	}

	want := backend.Request{Model: "m1", Messages: []backend.Message{
		{Role: "system", Content: "be nice"},
		{Role: "user", Content: "hi you"},
	}}
	if fb.got.Model != want.Model || len(fb.got.Messages) != 2 ||
		fb.got.Messages[0] != want.Messages[0] || fb.got.Messages[1] != want.Messages[1] {
		t.Fatalf("backend got %+v, want %+v", fb.got, want)
	}
}

func TestStreamDefaultsToUnary(t *testing.T) {
	fb := &fakeBackend{resp: backend.Response{Content: "x", FinishReason: "stop"}}
	rec := post(newServer(t, fb), `{"model":"fake/m1","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestUnknownModelIs400(t *testing.T) {
	for _, model := range []string{"openai/gpt-5", "m1", ""} {
		rec := post(newServer(t, &fakeBackend{}), `{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("model %q: status = %d", model, rec.Code)
		}
		e := decodeError(t, rec)
		if e.Error.Type != "invalid_request_error" || e.Error.Code == nil || *e.Error.Code != "model_not_found" ||
			e.Error.Param == nil || *e.Error.Param != "model" {
			t.Fatalf("model %q: error = %s", model, rec.Body)
		}
	}
}

func TestBackendUnknownModelIs400(t *testing.T) {
	fb := &fakeBackend{err: errors.Join(backend.ErrUnknownModel, errors.New("not offered"))}
	rec := post(newServer(t, fb), `{"model":"fake/zzz","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if e := decodeError(t, rec); e.Error.Code == nil || *e.Error.Code != "model_not_found" {
		t.Fatalf("error = %s", rec.Body)
	}
}

func TestStreamTrueIs400UntilTicket03(t *testing.T) {
	rec := post(newServer(t, &fakeBackend{}), `{"model":"fake/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if e := decodeError(t, rec); e.Error.Param == nil || *e.Error.Param != "stream" {
		t.Fatalf("error = %s", rec.Body)
	}
}

func TestBadBodiesAre400(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `{`,
		"no messages":    `{"model":"fake/m1","messages":[]}`,
		"bad role":       `{"model":"fake/m1","messages":[{"role":"tool","content":"x"}]}`,
		"image part":     `{"model":"fake/m1","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`,
		"number content": `{"model":"fake/m1","messages":[{"role":"user","content":5}]}`,
	} {
		rec := post(newServer(t, &fakeBackend{}), body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, body %s", name, rec.Code, rec.Body)
			continue
		}
		decodeError(t, rec)
	}
}

func TestBackendFailureIs502(t *testing.T) {
	fb := &fakeBackend{err: errors.New("agy acp exited")}
	rec := post(newServer(t, fb), `{"model":"fake/m1","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	if e := decodeError(t, rec); !strings.Contains(e.Error.Message, "agy acp exited") {
		t.Fatalf("error = %s", rec.Body)
	}
}

func TestWrongMethodIs405(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t, &fakeBackend{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
