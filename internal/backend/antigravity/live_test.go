package antigravity

import (
	"context"
	"os"
	"testing"
)

// TestLiveAttach checks attach mode against the real Antigravity daemon on
// this machine. Opt in with SUBPIPE_LIVE=1; it never spawns a process.
func TestLiveAttach(t *testing.T) {
	if os.Getenv("SUBPIPE_LIVE") != "1" {
		t.Skip("set SUBPIPE_LIVE=1 to run against the local language_server.exe")
	}
	dir, err := DefaultHubDir()
	if err != nil {
		t.Fatal(err)
	}
	hubs, err := ReadHubs(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hubs {
		if Ping(context.Background(), nil, h.endpoint()) == nil {
			t.Logf("attached: pid=%d port=%d version=%s file=%s", h.PID, h.HTTPPort, h.LSVersion, h.Path)
			return
		}
	}
	t.Fatalf("no healthy hub among %d discovery files in %s", len(hubs), dir)
}
