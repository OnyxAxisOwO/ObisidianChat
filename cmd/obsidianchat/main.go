package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"obsidianchat/internal/chat"
	"obsidianchat/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	data := os.Getenv("OC_DATABASE")
	if data == "" {
		data = "data/chat.db"
	}
	if err := os.MkdirAll(filepath.Dir(data), 0700); err != nil {
		return err
	}
	store, err := chat.Open(data)
	if err != nil {
		return err
	}
	defer store.Close()
	token := os.Getenv("OC_SETUP_TOKEN")
	if token == "" {
		b := make([]byte, 24)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		token = hex.EncodeToString(b)
	}
	var users int
	if err = store.Read.QueryRow("SELECT COUNT(*) FROM users").Scan(&users); err != nil {
		return err
	}
	if users == 0 {
		slog.Info("first-run setup required; enter this token in the browser", "setup_token", token)
	}
	app := chat.New(store, chat.Config{SetupToken: token, Origin: os.Getenv("OC_ORIGIN"), SecureCookie: os.Getenv("OC_SECURE_COOKIE") == "true"})
	assets, err := fs.Sub(web.Files, "dist")
	if err != nil {
		return err
	}
	addr := os.Getenv("OC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8090"
	}
	server := &http.Server{Addr: addr, Handler: app.Handler(assets), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { slog.Info("listening", "address", addr); done <- server.ListenAndServe() }()
	select {
	case err = <-done:
		app.Shutdown()
		return err
	case <-ctx.Done():
		app.Shutdown()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = server.Shutdown(shutdown)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
