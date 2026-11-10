// Package supervisor finds or launches the official language_server.exe and
// exposes the local ConnectRPC endpoint (port + CSRF token) subpipe talks to.
//
// Attach mode reads the discovery files the official binary writes under
// %USERPROFILE%\.gemini\antigravity\daemon\hub_<port>.json. Spawn mode starts
// language_server.exe --standalone as a child process when no live hub exists.
package supervisor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	csrfHeader = "x-codeium-csrf-token"
	configPath = "/exa.language_server_pb.LanguageServerService/GetServerConfiguration"
)

// ErrNotReady reports that a spawned language server never answered the
// health check before its deadline or exited first.
var ErrNotReady = errors.New("language server not ready")

// Hub is one parsed hub_<port>.json discovery file.
type Hub struct {
	PID       int    `json:"pid"`
	HTTPSPort int    `json:"httpsPort"`
	HTTPPort  int    `json:"httpPort"`
	LSVersion string `json:"lsVersion"`
	CSRFToken string `json:"csrfToken"`

	Path    string    `json:"-"`
	ModTime time.Time `json:"-"`
}

// Endpoint is everything needed to call the local ConnectRPC server.
type Endpoint struct {
	PID       int
	HTTPPort  int
	CSRFToken string
	Version   string
}

// BaseURL returns the HTTP origin of the local language server.
func (e Endpoint) BaseURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(e.HTTPPort)
}

func (h Hub) endpoint() Endpoint {
	return Endpoint{PID: h.PID, HTTPPort: h.HTTPPort, CSRFToken: h.CSRFToken, Version: h.LSVersion}
}

// DefaultHubDir is %USERPROFILE%\.gemini\antigravity\daemon.
func DefaultHubDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity", "daemon"), nil
}

// ReadHubs returns every valid hub file in dir, newest first. A missing
// directory is not an error: it means no daemon has ever published one.
// Malformed files and files without a port or token are skipped.
func ReadHubs(dir string) ([]Hub, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "hub_*.json"))
	if err != nil {
		return nil, err
	}
	var hubs []Hub
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var h Hub
		if json.Unmarshal(raw, &h) != nil || h.HTTPPort <= 0 || h.CSRFToken == "" {
			continue
		}
		h.Path, h.ModTime = p, info.ModTime()
		hubs = append(hubs, h)
	}
	sort.SliceStable(hubs, func(i, j int) bool { return hubs[i].ModTime.After(hubs[j].ModTime) })
	return hubs, nil
}

// FindBinary returns <localAppData>\Programs\antigravity\resources\bin\language_server.exe
// if it exists.
func FindBinary(localAppData string) (string, error) {
	p := filepath.Join(localAppData, "Programs", "antigravity", "resources", "bin", "language_server.exe")
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("language_server.exe not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("language_server.exe not found: %s is a directory", p)
	}
	return p, nil
}

// Ping calls GetServerConfiguration with the endpoint's CSRF token and
// succeeds only on HTTP 200. A nil client uses a short-timeout default.
func Ping(ctx context.Context, client *http.Client, ep Endpoint) error {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.BaseURL()+configPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, ep.CSRFToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GetServerConfiguration on port %d: HTTP %d", ep.HTTPPort, resp.StatusCode)
	}
	return nil
}

// Config controls how Connect finds or starts the language server.
type Config struct {
	HubDir       string        // discovery directory; empty uses DefaultHubDir
	Binary       string        // language_server.exe path; empty uses FindBinary(%LOCALAPPDATA%)
	StartTimeout time.Duration // spawn readiness deadline; zero means 30s
}

// Conn is a usable endpoint. Process is nil when attached to an existing
// daemon and non-nil when subpipe owns the child process.
type Conn struct {
	Endpoint Endpoint
	Process  *Process
}

// Connect attaches to the newest healthy hub, or spawns the official binary
// when none answers.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	dir := cfg.HubDir
	if dir == "" {
		d, err := DefaultHubDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	hubs, err := ReadHubs(dir)
	if err != nil {
		return nil, err
	}
	for _, h := range hubs {
		if Ping(ctx, nil, h.endpoint()) == nil {
			return &Conn{Endpoint: h.endpoint()}, nil
		}
	}

	bin := cfg.Binary
	if bin == "" {
		bin, err = FindBinary(os.Getenv("LOCALAPPDATA"))
		if err != nil {
			return nil, err
		}
	}
	version := ""
	if len(hubs) > 0 {
		version = hubs[0].LSVersion
	}
	timeout := cfg.StartTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	p, err := Spawn(ctx, bin, version, timeout)
	if err != nil {
		return nil, err
	}
	return &Conn{Endpoint: p.Endpoint, Process: p}, nil
}

// Process is a language_server.exe child owned by subpipe.
type Process struct {
	Endpoint Endpoint

	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
	err   error
	once  sync.Once
}

// Done is closed when the child exits.
func (p *Process) Done() <-chan struct{} { return p.done }

// Err returns the child's exit error once Done is closed.
func (p *Process) Err() error {
	<-p.done
	return p.err
}

// Stop closes stdin (the binary exits on EOF via -exit_on_stdin_close) and
// kills the child if it is still alive after grace.
func (p *Process) Stop(grace time.Duration) error {
	p.once.Do(func() { _ = p.stdin.Close() })
	select {
	case <-p.done:
		return nil
	case <-time.After(grace):
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-p.done
	return nil
}

// spawnArgs mirrors the flags Antigravity.exe uses to launch the hub server,
// minus the IDE host bridge, plus a fixed port/token we control.
func spawnArgs(port int, token, version string) []string {
	args := []string{
		"--standalone",
		"--override_ide_name", "antigravity",
		"--subclient_type", "hub",
		"--override_user_agent_name", "antigravity",
		"--app_data_dir", "antigravity",
		"--http_server_port", strconv.Itoa(port),
		"--https_server_port", "0",
		"--csrf_token", token,
		"--exit_on_stdin_close",
	}
	if version != "" {
		args = append(args, "--override_ide_version", version)
	}
	return args
}

// Spawn starts binary with a free port and a fresh CSRF token, then waits
// until GetServerConfiguration answers. extra args are appended verbatim.
func Spawn(ctx context.Context, binary, version string, timeout time.Duration, extra ...string) (*Process, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	token, err := newUUID()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, append(spawnArgs(port, token, version), extra...)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}
	p := &Process{
		Endpoint: Endpoint{PID: cmd.Process.Pid, HTTPPort: port, CSRFToken: token, Version: version},
		cmd:      cmd,
		stdin:    stdin,
		done:     make(chan struct{}),
	}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if Ping(ctx, nil, p.Endpoint) == nil {
			return p, nil
		}
		select {
		case <-p.done:
			return nil, fmt.Errorf("%w: process exited: %v", ErrNotReady, p.err)
		case <-ctx.Done():
			_ = p.Stop(2 * time.Second)
			return nil, fmt.Errorf("%w: %v", ErrNotReady, ctx.Err())
		case <-tick.C:
		}
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
