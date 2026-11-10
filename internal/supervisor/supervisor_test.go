package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake language_server.exe when SUBPIPE_FAKE_LS=1, so
// Spawn can be exercised end to end without the real binary.
func TestMain(m *testing.M) {
	if os.Getenv("SUBPIPE_FAKE_LS") == "1" {
		runFakeLanguageServer(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

func runFakeLanguageServer(args []string) {
	var port, token string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--http_server_port":
			port = args[i+1]
		case "--csrf_token":
			token = args[i+1]
		}
	}
	go func() {
		// Mirrors -exit_on_stdin_close: exit when the parent closes stdin.
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}()
	_ = http.ListenAndServe("127.0.0.1:"+port, fakeHandler(token))
	os.Exit(3)
}

func fakeHandler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != configPath {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get(csrfHeader) != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"config":{"standalone":true}}`)
	})
}

func writeHub(t *testing.T, dir, name, body string, mod time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func hubJSON(pid, port int, token string) string {
	return fmt.Sprintf(`{"pid":%d,"httpsPort":%d,"httpPort":%d,"lsVersion":"2.29.0","csrfToken":%q}`, pid, port-1, port, token)
}

func TestReadHubs(t *testing.T) {
	now := time.Now()
	t.Run("missing directory means no daemon", func(t *testing.T) {
		hubs, err := ReadHubs(filepath.Join(t.TempDir(), "absent"))
		if err != nil || len(hubs) != 0 {
			t.Fatalf("got %v, %v; want empty, nil", hubs, err)
		}
	})
	t.Run("newest first, skipping malformed and incomplete files", func(t *testing.T) {
		dir := t.TempDir()
		writeHub(t, dir, "hub_100.json", hubJSON(1, 100, "old"), now.Add(-2*time.Hour))
		writeHub(t, dir, "hub_300.json", hubJSON(3, 300, "new"), now)
		writeHub(t, dir, "hub_200.json", hubJSON(2, 200, "mid"), now.Add(-time.Hour))
		writeHub(t, dir, "hub_400.json", "{not json", now.Add(time.Minute))
		writeHub(t, dir, "hub_500.json", `{"pid":5,"httpPort":500}`, now.Add(time.Minute))
		writeHub(t, dir, "other.json", hubJSON(9, 900, "ignored"), now.Add(time.Hour))

		hubs, err := ReadHubs(dir)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, h := range hubs {
			got = append(got, h.CSRFToken)
		}
		want := []string{"new", "mid", "old"}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("tokens = %v, want %v", got, want)
		}
		h := hubs[0]
		if h.PID != 3 || h.HTTPPort != 300 || h.HTTPSPort != 299 || h.LSVersion != "2.29.0" {
			t.Fatalf("parsed hub = %+v", h)
		}
	})
}

func TestFindBinary(t *testing.T) {
	root := t.TempDir()
	if _, err := FindBinary(root); err == nil {
		t.Fatal("expected error when binary is absent")
	}
	bin := filepath.Join(root, "Programs", "antigravity", "resources", "bin", "language_server.exe")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindBinary(root)
	if err != nil || got != bin {
		t.Fatalf("FindBinary = %q, %v; want %q", got, err, bin)
	}
}

func serverPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestPing(t *testing.T) {
	srv := httptest.NewServer(fakeHandler("good"))
	defer srv.Close()
	port := serverPort(t, srv)
	ctx := context.Background()

	if err := Ping(ctx, nil, Endpoint{HTTPPort: port, CSRFToken: "good"}); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	if err := Ping(ctx, nil, Endpoint{HTTPPort: port, CSRFToken: "bad"}); err == nil {
		t.Fatal("invalid token: expected error")
	}
	srv.Close()
	if err := Ping(ctx, nil, Endpoint{HTTPPort: port, CSRFToken: "good"}); err == nil {
		t.Fatal("closed port: expected error")
	}
}

func TestConnectAttachesToFirstHealthyHub(t *testing.T) {
	live := httptest.NewServer(fakeHandler("live"))
	defer live.Close()
	dead := httptest.NewServer(fakeHandler("dead"))
	deadPort := serverPort(t, dead)
	dead.Close()

	dir := t.TempDir()
	now := time.Now()
	writeHub(t, dir, "hub_dead.json", hubJSON(11, deadPort, "dead"), now)
	writeHub(t, dir, "hub_live.json", hubJSON(22, serverPort(t, live), "live"), now.Add(-time.Hour))

	conn, err := Connect(context.Background(), Config{HubDir: dir, Binary: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Process != nil {
		t.Fatal("attach mode must not spawn a process")
	}
	if conn.Endpoint.PID != 22 || conn.Endpoint.CSRFToken != "live" || conn.Endpoint.Version != "2.29.0" {
		t.Fatalf("endpoint = %+v", conn.Endpoint)
	}
}

func TestConnectSpawnsWhenNoHubIsHealthy(t *testing.T) {
	t.Setenv("SUBPIPE_FAKE_LS", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := Connect(ctx, Config{HubDir: t.TempDir(), Binary: self, StartTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Process == nil {
		t.Fatal("expected a spawned process")
	}
	if conn.Endpoint.PID == 0 || conn.Endpoint.HTTPPort == 0 || conn.Endpoint.CSRFToken == "" {
		t.Fatalf("endpoint = %+v", conn.Endpoint)
	}
	if err := Ping(ctx, nil, conn.Endpoint); err != nil {
		t.Fatalf("spawned server not healthy: %v", err)
	}

	if err := conn.Process.Stop(5 * time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-conn.Process.Done():
	default:
		t.Fatal("Done must be closed after Stop")
	}
	if err := Ping(ctx, nil, conn.Endpoint); err == nil {
		t.Fatal("server still answering after Stop")
	}
}

func TestSpawnFailsWhenServerNeverBecomesHealthy(t *testing.T) {
	// The test binary without SUBPIPE_FAKE_LS runs the test suite filtered to
	// nothing and exits, so it never serves the port.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Spawn(context.Background(), self, "", 2*time.Second, "-test.run=^$")
	if err == nil {
		t.Fatal("expected spawn error")
	}
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("err = %v, want ErrNotReady", err)
	}
}

func TestSpawnArgsMirrorOfficialLaunch(t *testing.T) {
	args := spawnArgs(4242, "tok", "2.29.0")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--standalone", "--http_server_port 4242", "--csrf_token tok",
		"--override_ide_version 2.29.0", "--subclient_type hub", "--exit_on_stdin_close",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
	if strings.Contains(strings.Join(spawnArgs(1, "t", ""), " "), "override_ide_version") {
		t.Error("empty version must omit --override_ide_version")
	}
}
