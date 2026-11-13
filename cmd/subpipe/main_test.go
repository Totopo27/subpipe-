package main

import (
	"os"
	"reflect"
	"testing"
)

func TestBackendConfigKeepsAgyPathWithSpaces(t *testing.T) {
	path := `C:\Program Files\Antigravity\agy.exe`
	cfg := backendConfig(path, `D:\work dir`)
	if want := []string{path, "acp"}; !reflect.DeepEqual(cfg.Command, want) {
		t.Fatalf("Command = %q, want %q", cfg.Command, want)
	}
	if cfg.WorkDir != `D:\work dir` {
		t.Fatalf("WorkDir = %q", cfg.WorkDir)
	}
	if cfg.Stderr != os.Stderr {
		t.Fatal("agent stderr is not forwarded to subpipe's stderr")
	}
}

func TestBackendConfigDefaultRunsAgyAcp(t *testing.T) {
	cfg := backendConfig("agy", "")
	if want := []string{"agy", "acp"}; !reflect.DeepEqual(cfg.Command, want) {
		t.Fatalf("Command = %q, want %q", cfg.Command, want)
	}
}
