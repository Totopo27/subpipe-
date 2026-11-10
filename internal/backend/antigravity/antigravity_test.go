package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"subpipe/internal/backend"
)

// newFake returns a Backend whose child is this test binary acting as a fake
// `agy acp` in the given mode, plus the path of the fake's event log.
func newFake(t *testing.T, mode string) (*Backend, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.log")
	b := New(Config{
		Command: []string{os.Args[0], "-test.run=^$"},
		Env:     []string{"SUBPIPE_FAKE_ACP=" + mode, "SUBPIPE_FAKE_ACP_LOG=" + logPath},
		WorkDir: dir,
	})
	t.Cleanup(func() { _ = b.Close() })
	return b, logPath
}

func userMsg(text string) []backend.Message {
	return []backend.Message{{Role: "user", Content: text}}
}

// waitEvent polls the fake's log until event appears or the deadline passes.
func waitEvent(t *testing.T, logPath, event string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countEvent(logPath, event) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	t.Fatalf("event %q never logged; log:\n%s", event, raw)
}

func countEvent(logPath, event string) int {
	raw, _ := os.ReadFile(logPath)
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == event {
			n++
		}
	}
	return n
}

func TestNameIsAntigravity(t *testing.T) {
	if got := New(Config{}).Name(); got != "antigravity" {
		t.Fatalf("Name = %q, want antigravity", got)
	}
}

func TestCompleteReturnsAgentTextForDefaultModel(t *testing.T) {
	b, _ := newFake(t, "ok")
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "model=gemini-3.8-pro;prompt=hello" || resp.FinishReason != "stop" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestCompleteSelectsGroupedModel(t *testing.T) {
	b, _ := newFake(t, "ok")
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-flash", Messages: userMsg("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "model=gemini-3.8-flash;prompt=hi" {
		t.Fatalf("Content = %q", resp.Content)
	}
}

func TestCompleteRejectsModelTheAgentDoesNotOffer(t *testing.T) {
	b, logPath := newFake(t, "ok")
	_, err := b.Complete(context.Background(), backend.Request{Model: "gpt-5", Messages: userMsg("hi")})
	if !errors.Is(err, backend.ErrUnknownModel) {
		t.Fatalf("err = %v, want ErrUnknownModel", err)
	}
	if countEvent(logPath, "prompt sess_1") != 0 {
		t.Fatal("prompt was sent for an unknown model")
	}
}

func TestCompleteFlattensHistory(t *testing.T) {
	b, _ := newFake(t, "ok")
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: []backend.Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "2+2?"},
		{Role: "assistant", Content: "4"},
		{Role: "user", Content: "and 3+3?"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "model=gemini-3.8-pro;prompt=[system]\nbe brief\n\n[user]\n2+2?\n\n[assistant]\n4\n\n[user]\nand 3+3?"
	if resp.Content != want {
		t.Fatalf("Content = %q\nwant      %q", resp.Content, want)
	}
}

func TestCompleteReusesOneProcessAndClosesSessions(t *testing.T) {
	b, logPath := newFake(t, "ok")
	for i := 0; i < 2; i++ {
		if _, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("x")}); err != nil {
			t.Fatal(err)
		}
	}
	waitEvent(t, logPath, "close sess_1")
	waitEvent(t, logPath, "close sess_2")
	if n := countEvent(logPath, "start"); n != 1 {
		t.Fatalf("agent started %d times, want 1", n)
	}
}

func TestConcurrentCompletionsKeepTheirOwnText(t *testing.T) {
	b, _ := newFake(t, "ok")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := strings.Repeat("q", i+1)
			resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg(text)})
			if err == nil && resp.Content != "model=gemini-3.8-pro;prompt="+text {
				err = errors.New("crossed turns: got " + resp.Content + " for " + text)
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestCompleteRejectsPermissionRequests(t *testing.T) {
	b, _ := newFake(t, "permission")
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("write a file")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "outcome=selected/reject" {
		t.Fatalf("Content = %q, want the reject option selected", resp.Content)
	}
}

func TestCompleteRefusesAgentFileReads(t *testing.T) {
	b, _ := newFake(t, "fsread")
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("read a file")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "fs=-32601" {
		t.Fatalf("Content = %q, want Method not found (-32601)", resp.Content)
	}
}

func TestCompleteMapsMaxTokensToLength(t *testing.T) {
	b, _ := newFake(t, "maxtokens")
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("x")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "partial" || resp.FinishReason != "length" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestCompleteReportsAuthRequired(t *testing.T) {
	b, _ := newFake(t, "auth")
	_, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("x")})
	if !errors.Is(err, ErrAuthRequired) || !strings.Contains(err.Error(), "agy") {
		t.Fatalf("err = %v, want ErrAuthRequired mentioning agy", err)
	}
}

func TestCancelledContextSendsSessionCancel(t *testing.T) {
	b, logPath := newFake(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := b.Complete(ctx, backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("x")})
		done <- err
	}()
	waitEvent(t, logPath, "prompt sess_1")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Complete did not return after cancel")
	}
	waitEvent(t, logPath, "cancel sess_1")
}

func TestDeadChildIsRespawnedOnNextRequest(t *testing.T) {
	b, logPath := newFake(t, "die-once")
	if _, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("x")}); err == nil {
		t.Fatal("first Complete succeeded, want an error from the dying child")
	}
	resp, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("y")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "model=gemini-3.8-pro;prompt=y" {
		t.Fatalf("Content = %q", resp.Content)
	}
	if n := countEvent(logPath, "start"); n != 2 {
		t.Fatalf("agent started %d times, want 2", n)
	}
}

func TestMissingBinaryIsAnError(t *testing.T) {
	b := New(Config{Command: []string{filepath.Join(t.TempDir(), "no-such-agy")}, WorkDir: t.TempDir()})
	defer b.Close()
	if _, err := b.Complete(context.Background(), backend.Request{Model: "gemini-3.8-pro", Messages: userMsg("x")}); err == nil {
		t.Fatal("Complete succeeded without a binary")
	}
}

func TestModelsListsDefaultCatalogue(t *testing.T) {
	ms, err := New(Config{}).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].ID != "gemini-3.8-pro" || ms[1].ID != "gemini-3.8-flash" {
		t.Fatalf("Models = %+v", ms)
	}
}
