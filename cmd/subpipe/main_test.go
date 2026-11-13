package main

import (
	"os"
	"reflect"
	"testing"
)

func TestBackendConfigKeepsAgyPathWithSpaces(t *testing.T) {
	path := `C:\Program Files\Antigravity\agy.exe`
	cfg := backendConfig(path, `D:\work dir`, "", true)
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
	cfg := backendConfig("agy", "", "", true)
	if want := []string{"agy", "acp"}; !reflect.DeepEqual(cfg.Command, want) {
		t.Fatalf("Command = %q, want %q", cfg.Command, want)
	}
	if !cfg.Watchdog {
		t.Fatal("Watchdog = false, want true")
	}
}

func TestBackendConfigConfigDir(t *testing.T) {
	cfg := backendConfig("agy", "", `D:\profiles\acc1`, true)
	if cfg.ConfigDir != `D:\profiles\acc1` {
		t.Fatalf("ConfigDir = %q, want D:\\profiles\\acc1", cfg.ConfigDir)
	}
}

func TestResolveConfigDirFallsBackToEnv(t *testing.T) {
	t.Setenv("GEMINI_DIR", `D:\env-gemini`)
	if got := resolveConfigDir(""); got != `D:\env-gemini` {
		t.Fatalf("resolveConfigDir = %q, want D:\\env-gemini", got)
	}

	t.Setenv("GEMINI_DIR", "")
	t.Setenv("AGY_CONFIG_DIR", `D:\env-agy`)
	if got := resolveConfigDir(""); got != `D:\env-agy` {
		t.Fatalf("resolveConfigDir = %q, want D:\\env-agy", got)
	}

	if got := resolveConfigDir(`D:\explicit`); got != `D:\explicit` {
		t.Fatalf("resolveConfigDir = %q, want D:\\explicit", got)
	}
}
