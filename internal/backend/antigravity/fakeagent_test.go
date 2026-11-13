package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestMain doubles as a fake `agy acp` when SUBPIPE_FAKE_ACP names a mode, so
// the backend can be exercised end to end without the real binary.
//
// The fake is written independently of the production ACP client (raw maps,
// its own read loop) so a protocol mistake cannot cancel itself out.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SUBPIPE_FAKE_ACP"); mode != "" {
		fmt.Fprintln(os.Stderr, "fake agent: diagnostics on stderr")
		f := &fakeAgent{
			mode:     mode,
			log:      os.Getenv("SUBPIPE_FAKE_ACP_LOG"),
			out:      os.Stdout,
			sessions: map[string]string{},
			cancels:  map[string]chan struct{}{},
			replies:  map[string]chan map[string]any{},
		}
		f.run(os.Stdin)
		return
	}
	os.Exit(m.Run())
}

// Fake modes:
//
//	ok         echo "model=<m>;" and "prompt=<text>" as two chunks, end_turn
//	auth       session/new fails with -32000 Authentication required
//	permission ask session/request_permission, echo the outcome, end_turn
//	fsread     ask fs/read_text_file, echo the error code, end_turn
//	hang       never finish the turn until session/cancel, then "cancelled"
//	maxtokens  one chunk, then stopReason max_tokens
//	die-once   exit on the first prompt of the first process, then behave as ok
//	oddstop    one chunk, then a stop reason this client does not know
//	stringids  behave as ok, but echo request ids back as JSON strings
//
// Every mode writes one line to stderr at start.
type fakeAgent struct {
	mode string
	log  string
	out  io.Writer

	wmu      sync.Mutex
	mu       sync.Mutex
	nextSess int
	sessions map[string]string // session id -> current model
	cancels  map[string]chan struct{}
	replies  map[string]chan map[string]any
	nextReq  int
}

var fakeModels = []string{"gemini-3.8-pro", "gemini-3.8-flash"}

func (f *fakeAgent) record(event string) {
	if f.log == "" {
		return
	}
	fh, err := os.OpenFile(f.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer fh.Close()
	fmt.Fprintln(fh, event)
}

func (f *fakeAgent) logged(event string) bool {
	raw, _ := os.ReadFile(f.log)
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == event {
			return true
		}
	}
	return false
}

func (f *fakeAgent) send(msg map[string]any) {
	msg["jsonrpc"] = "2.0"
	raw, _ := json.Marshal(msg)
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_, _ = f.out.Write(append(raw, '\n'))
}

func (f *fakeAgent) reply(id any, result any) {
	if f.mode == "stringids" {
		id = fmt.Sprint(id)
	}
	f.send(map[string]any{"id": id, "result": result})
}

func (f *fakeAgent) fail(id any, code int, msg string) {
	f.send(map[string]any{"id": id, "error": map[string]any{"code": code, "message": msg}})
}

func (f *fakeAgent) chunk(sess, text string) {
	f.send(map[string]any{"method": "session/update", "params": map[string]any{
		"sessionId": sess,
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": text},
		},
	}})
}

func (f *fakeAgent) run(in io.Reader) {
	f.record("start")
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var msg map[string]any
			if json.Unmarshal(line, &msg) != nil {
				f.send(map[string]any{"id": nil, "error": map[string]any{"code": -32700, "message": "Parse error"}})
			} else {
				f.dispatch(msg)
			}
		}
		if err != nil {
			return
		}
	}
}

func (f *fakeAgent) dispatch(msg map[string]any) {
	method, _ := msg["method"].(string)
	id, hasID := msg["id"]
	params, _ := msg["params"].(map[string]any)
	switch {
	case method == "" && hasID: // response to one of our requests
		key := fmt.Sprint(id)
		f.mu.Lock()
		ch := f.replies[key]
		delete(f.replies, key)
		f.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	case method == "session/cancel":
		sess, _ := params["sessionId"].(string)
		f.record("cancel " + sess)
		f.mu.Lock()
		if ch, ok := f.cancels[sess]; ok {
			close(ch)
			delete(f.cancels, sess)
		}
		f.mu.Unlock()
	case hasID:
		go f.request(id, method, params)
	}
}

func (f *fakeAgent) request(id any, method string, params map[string]any) {
	switch method {
	case "initialize":
		caps, _ := params["clientCapabilities"].(map[string]any)
		fs, _ := caps["fs"].(map[string]any)
		if params["protocolVersion"] != float64(1) || caps["terminal"] == true ||
			fs["readTextFile"] == true || fs["writeTextFile"] == true {
			f.fail(id, -32602, fmt.Sprintf("bad initialize params: %v", params))
			return
		}
		f.reply(id, map[string]any{
			"protocolVersion":   1,
			"agentCapabilities": map[string]any{"sessionCapabilities": map[string]any{"close": map[string]any{}}},
			"authMethods":       []any{},
		})
	case "session/new":
		if f.mode == "auth" {
			f.fail(id, -32000, "Authentication required")
			return
		}
		cwd, _ := params["cwd"].(string)
		if _, isArray := params["mcpServers"].([]any); !isArray || !filepath.IsAbs(cwd) {
			f.fail(id, -32602, fmt.Sprintf("bad session/new params: %v", params))
			return
		}
		f.mu.Lock()
		f.nextSess++
		sess := fmt.Sprintf("sess_%d", f.nextSess)
		f.sessions[sess] = fakeModels[0]
		f.mu.Unlock()
		f.reply(id, map[string]any{"sessionId": sess, "configOptions": f.options(fakeModels[0])})
	case "session/set_config_option":
		sess, _ := params["sessionId"].(string)
		value, _ := params["value"].(string)
		f.mu.Lock()
		_, known := f.sessions[sess]
		f.mu.Unlock()
		if !known || params["configId"] != "model" || (value != fakeModels[0] && value != fakeModels[1]) {
			f.fail(id, -32602, "Invalid params")
			return
		}
		f.mu.Lock()
		f.sessions[sess] = value
		f.mu.Unlock()
		f.reply(id, map[string]any{"configOptions": f.options(value)})
	case "session/prompt":
		f.prompt(id, params)
	case "session/close":
		sess, _ := params["sessionId"].(string)
		f.record("close " + sess)
		f.reply(id, map[string]any{})
	default:
		f.fail(id, -32601, "Method not found")
	}
}

// options lists the model selector with one flat option and one group, the
// two shapes ACP allows for select values.
func (f *fakeAgent) options(current string) []any {
	return []any{
		map[string]any{"id": "thinking", "name": "Thinking level", "type": "select", "currentValue": "medium",
			"options": []any{map[string]any{"value": "medium", "name": "Medium"}}},
		map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": current,
			"options": []any{
				map[string]any{"value": fakeModels[0], "name": "Gemini 3.8 Pro"},
				map[string]any{"group": "fast", "name": "Fast", "options": []any{
					map[string]any{"value": fakeModels[1], "name": "Gemini 3.8 Flash"},
				}},
			}},
	}
}

func (f *fakeAgent) prompt(id any, params map[string]any) {
	sess, _ := params["sessionId"].(string)
	f.mu.Lock()
	model, known := f.sessions[sess]
	f.mu.Unlock()
	if !known {
		f.fail(id, -32002, "Resource not found")
		return
	}
	f.record("prompt " + sess)
	var text strings.Builder
	blocks, _ := params["prompt"].([]any)
	for _, b := range blocks {
		if blk, _ := b.(map[string]any); blk["type"] == "text" {
			s, _ := blk["text"].(string)
			text.WriteString(s)
		}
	}

	switch f.mode {
	case "die-once":
		if !f.logged("died") {
			f.record("died")
			os.Exit(1)
		}
	case "hang":
		ch := make(chan struct{})
		f.mu.Lock()
		f.cancels[sess] = ch
		f.mu.Unlock()
		<-ch
		f.reply(id, map[string]any{"stopReason": "cancelled"})
		return
	case "permission":
		f.mu.Lock()
		f.nextReq++
		reqID := fmt.Sprintf("perm-%d", f.nextReq)
		ch := make(chan map[string]any, 1)
		f.replies[reqID] = ch
		f.mu.Unlock()
		f.send(map[string]any{"id": reqID, "method": "session/request_permission", "params": map[string]any{
			"sessionId": sess,
			"toolCall":  map[string]any{"toolCallId": "call_1", "title": "Write file"},
			"options": []any{
				map[string]any{"optionId": "allow", "name": "Allow once", "kind": "allow_once"},
				map[string]any{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
			},
		}})
		resp := <-ch
		result, _ := resp["result"].(map[string]any)
		outcome, _ := result["outcome"].(map[string]any)
		f.chunk(sess, fmt.Sprintf("outcome=%v/%v", outcome["outcome"], outcome["optionId"]))
		f.reply(id, map[string]any{"stopReason": "end_turn"})
		return
	case "fsread":
		// subpipe advertised no fs capability, so it must refuse this.
		f.mu.Lock()
		f.nextReq++
		reqID := fmt.Sprintf("fs-%d", f.nextReq)
		ch := make(chan map[string]any, 1)
		f.replies[reqID] = ch
		f.mu.Unlock()
		f.send(map[string]any{"id": reqID, "method": "fs/read_text_file", "params": map[string]any{
			"sessionId": sess, "path": "/etc/passwd",
		}})
		resp := <-ch
		errObj, _ := resp["error"].(map[string]any)
		f.chunk(sess, fmt.Sprintf("fs=%v", errObj["code"]))
		f.reply(id, map[string]any{"stopReason": "end_turn"})
		return
	case "maxtokens":
		f.chunk(sess, "partial")
		f.reply(id, map[string]any{"stopReason": "max_tokens"})
		return
	case "oddstop":
		f.chunk(sess, "done")
		f.reply(id, map[string]any{"stopReason": "some_future_reason"})
		return
	}

	// A non-text update must be ignored by the client.
	f.send(map[string]any{"method": "session/update", "params": map[string]any{
		"sessionId": sess,
		"update": map[string]any{
			"sessionUpdate": "agent_thought_chunk",
			"content":       map[string]any{"type": "text", "text": "thinking..."},
		},
	}})
	f.chunk(sess, "model="+model+";")
	f.chunk(sess, "prompt="+text.String())
	f.reply(id, map[string]any{"stopReason": "end_turn"})
}
