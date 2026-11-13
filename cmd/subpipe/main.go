// Command subpipe runs the local OpenAI-compatible micro-gateway.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"subpipe/internal/backend"
	"subpipe/internal/backend/antigravity"
	"subpipe/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9099", "listen address")
	agy := flag.String("agy", "agy", "path to the agy binary; subpipe runs it as `<agy> acp`")
	workdir := flag.String("workdir", "", "working directory for agy sessions (default: <temp>/subpipe-antigravity)")
	configDir := flag.String("config-dir", "", "path to user profile/config directory for multi-account isolation (env: GEMINI_DIR or AGY_CONFIG_DIR)")
	watchdog := flag.Bool("watchdog", true, "enable automatic background supervisor respawn on child death")
	flag.Parse()

	resolvedConfigDir := resolveConfigDir(*configDir)
	ag := antigravity.New(backendConfig(*agy, *workdir, resolvedConfigDir, *watchdog))
	defer ag.Close()
	reg, err := backend.NewRegistry(ag)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{Addr: *addr, Handler: server.New(reg), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("subpipe listening on http://%s/v1", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// resolveConfigDir checks flag input, then GEMINI_DIR and AGY_CONFIG_DIR environment variables.
func resolveConfigDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("GEMINI_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("AGY_CONFIG_DIR"); v != "" {
		return v
	}
	return ""
}

// backendConfig builds the Antigravity backend config. agy is a single path,
// never split, so install directories with spaces work.
func backendConfig(agy, workdir, configDir string, watchdog bool) antigravity.Config {
	return antigravity.Config{
		Command:   []string{agy, "acp"},
		WorkDir:   workdir,
		ConfigDir: configDir,
		Stderr:    os.Stderr,
		Watchdog:  watchdog,
	}
}
