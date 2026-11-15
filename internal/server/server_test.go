package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"subpipe/internal/backend"
)

type fakeBackend struct {
	got    backend.Request
	resp   backend.Response
	err    error
	stream []backend.StreamChunk
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Models(context.Context) ([]backend.Model, error) {
	return []backend.Model{{ID: "m1"}}, nil
}
func (f *fakeBackend) Complete(_ context.Context, r backend.Request) (backend.Response, error) {
	f.got = r
	return f.resp, f.err
}
func (f *fakeBackend) Stream(_ context.Context, r backend.Request, onChunk func(backend.StreamChunk) error) error {
	f.got = r
	if f.err != nil {
		return f.err
	}
	for _, c := range f.stream {
		if err := onChunk(c); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeBackend) Health(_ context.Context) (backend.Health, error) {
	if f.err != nil {
		return backend.Health{Alive: false, Details: f.err.Error()}, f.err
	}
	return backend.Health{Alive: true, Initialized: true, Authenticated: true}, nil
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

func TestUnsupportedToolFieldsAre400(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tt := range []struct {
			name  string
			field string
			value string
		}{
			{"tools catalogue", "tools", `[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]`},
			{"empty tools", "tools", `[]`},
			{"null tools", "tools", `null`},
			{"empty string tools", "tools", `""`},
			{"automatic choice", "tool_choice", `"auto"`},
			{"disabled choice", "tool_choice", `"none"`},
			{"named choice", "tool_choice", `{"type":"function","function":{"name":"lookup"}}`},
			{"null choice", "tool_choice", `null`},
			{"empty string choice", "tool_choice", `""`},
		} {
			t.Run(fmt.Sprintf("%s/stream=%t", tt.name, stream), func(t *testing.T) {
				fb := &fakeBackend{resp: backend.Response{Content: "must not run"}}
				body := fmt.Sprintf(`{"model":"fake/m1","messages":[{"role":"user","content":"hi"}],"stream":%t,%q:%s}`, stream, tt.field, tt.value)
				rec := post(newServer(t, fb), body)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				e := decodeError(t, rec)
				if e.Error.Type != "invalid_request_error" || e.Error.Param == nil || *e.Error.Param != tt.field ||
					e.Error.Message != tt.field+" is not supported" || e.Error.Code != nil {
					t.Errorf("error = %s", rec.Body)
				}
				if fb.got.Model != "" || fb.got.Messages != nil {
					t.Errorf("backend called with %+v", fb.got)
				}
			})
		}
	}
}

func TestUnrelatedUnknownFieldsRemainIgnored(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			fb := &fakeBackend{
				resp:   backend.Response{Content: "hello", FinishReason: "stop"},
				stream: []backend.StreamChunk{{Content: "hello"}, {FinishReason: "stop"}},
			}
			body := fmt.Sprintf(`{"model":"fake/m1","messages":[{"role":"developer","content":"be nice"},{"role":"user","content":"hi"}],"stream":%t,"client_metadata":{"trace":"local"}}`, stream)
			rec := post(newServer(t, fb), body)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello") {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if fb.got.Model != "m1" || len(fb.got.Messages) != 2 ||
				fb.got.Messages[0] != (backend.Message{Role: "system", Content: "be nice"}) ||
				fb.got.Messages[1] != (backend.Message{Role: "user", Content: "hi"}) {
				t.Errorf("backend request = %+v", fb.got)
			}
			if stream {
				if rec.Header().Get("Content-Type") != "text/event-stream" || !strings.HasSuffix(rec.Body.String(), "data: [DONE]\n\n") {
					t.Errorf("invalid SSE response: %s", rec.Body)
				}
			} else if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestToolsReportedBeforeToolChoice(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			fb := &fakeBackend{}
			body := fmt.Sprintf(`{"model":"fake/m1","messages":[{"role":"user","content":"hi"}],"stream":%t,"tool_choice":"none","tools":[]}`, stream)
			rec := post(newServer(t, fb), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			e := decodeError(t, rec)
			if e.Error.Param == nil || *e.Error.Param != "tools" || e.Error.Message != "tools is not supported" {
				t.Errorf("error = %s", rec.Body)
			}
			if fb.got.Model != "" || fb.got.Messages != nil {
				t.Errorf("backend called with %+v", fb.got)
			}
		})
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

func TestChatCompletionStreamSuccess(t *testing.T) {
	fb := &fakeBackend{
		stream: []backend.StreamChunk{
			{Content: "hello "},
			{Content: "world"},
			{FinishReason: "stop"},
		},
	}
	rec := post(newServer(t, fb), `{"model":"fake/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}

	lines := strings.Split(rec.Body.String(), "\n")
	var dataLines []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(l, "data: "))
		}
	}
	if len(dataLines) < 3 {
		t.Fatalf("not enough data lines (%d): %q", len(dataLines), rec.Body.String())
	}
	if dataLines[len(dataLines)-1] != "[DONE]" {
		t.Fatalf("last data line = %q, want [DONE]", dataLines[len(dataLines)-1])
	}

	// Verify chunks before [DONE]
	var combinedText strings.Builder
	var gotFinish string
	for _, raw := range dataLines[:len(dataLines)-1] {
		var chunk struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			t.Fatalf("chunk is not JSON: %v, raw: %s", err, raw)
		}
		if !strings.HasPrefix(chunk.ID, "chatcmpl-") || chunk.Object != "chat.completion.chunk" || chunk.Model != "fake/m1" {
			t.Fatalf("unexpected chunk envelope: %+v", chunk)
		}
		if len(chunk.Choices) > 0 {
			combinedText.WriteString(chunk.Choices[0].Delta.Content)
			if chunk.Choices[0].FinishReason != nil {
				gotFinish = *chunk.Choices[0].FinishReason
			}
		}
	}
	if combinedText.String() != "hello world" {
		t.Fatalf("combined text = %q, want 'hello world'", combinedText.String())
	}
	if gotFinish != "stop" {
		t.Fatalf("finish_reason = %q, want 'stop'", gotFinish)
	}
}

func TestChatCompletionStreamBackendErrorReturns502(t *testing.T) {
	fb := &fakeBackend{err: errors.New("backend failed")}
	rec := post(newServer(t, fb), `{"model":"fake/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body %s", rec.Code, rec.Body)
	}
	e := decodeError(t, rec)
	if !strings.Contains(e.Error.Message, "backend failed") {
		t.Fatalf("error message = %q", e.Error.Message)
	}
}

func TestModelsEndpoint(t *testing.T) {
	fb := &fakeBackend{}
	h := newServer(t, fb)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var res struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Object != "list" || len(res.Data) != 1 {
		t.Fatalf("unexpected models response: %+v", res)
	}
	if res.Data[0].ID != "fake/m1" || res.Data[0].Object != "model" || res.Data[0].OwnedBy != "fake" || res.Data[0].Created == 0 {
		t.Fatalf("model entry = %+v", res.Data[0])
	}
}

func TestModelsEndpointBackendErrorReturns502(t *testing.T) {
	fb := &fakeBackend{err: errors.New("cannot list models")}
	// override Models to return error
	fbModelsErr := &fakeBackendModelsErr{fakeBackend: fb}
	reg, err := backend.NewRegistry(fbModelsErr)
	if err != nil {
		t.Fatal(err)
	}
	h := New(reg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	e := decodeError(t, rec)
	if !strings.Contains(e.Error.Message, "cannot list models") {
		t.Fatalf("error message = %q", e.Error.Message)
	}
}

type fakeBackendModelsErr struct {
	*fakeBackend
}

func (f *fakeBackendModelsErr) Models(context.Context) ([]backend.Model, error) {
	return nil, errors.New("cannot list models")
}

func TestHealthzEndpointHealthy(t *testing.T) {
	fb := &fakeBackend{}
	h := newServer(t, fb)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var res struct {
		Status   string                    `json:"status"`
		Backends map[string]backend.Health `json:"backends"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" || !res.Backends["fake"].Alive {
		t.Fatalf("unexpected healthz response: %+v", res)
	}
}

func TestHealthzEndpointDegradedReturns503(t *testing.T) {
	fb := &fakeBackend{err: errors.New("subprocess died")}
	h := newServer(t, fb)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body %s", rec.Code, rec.Body)
	}
	var res struct {
		Status   string                    `json:"status"`
		Backends map[string]backend.Health `json:"backends"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "degraded" || res.Backends["fake"].Alive {
		t.Fatalf("unexpected degraded response: %+v", res)
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
