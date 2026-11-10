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
	"strings"
	"time"

	"subpipe/internal/backend"
	"subpipe/internal/backend/antigravity"
	"subpipe/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9099", "listen address")
	agy := flag.String("agy", "agy acp", "command line that starts the Antigravity ACP server")
	workdir := flag.String("workdir", "", "working directory for agy sessions (default: <temp>/subpipe-antigravity)")
	flag.Parse()

	ag := antigravity.New(antigravity.Config{Command: strings.Fields(*agy), WorkDir: *workdir})
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
