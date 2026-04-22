package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func defaultDataDir() string {
	if d := os.Getenv("MULTICA_DATA_DIR"); d != "" {
		return d
	}
	// ~/.multica/lite on all platforms
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".multica", "lite")
	}
	return filepath.Join(home, ".multica", "lite")
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	dataDir := defaultDataDir()

	slog.Info("multica-lite starting",
		"port", port,
		"data_dir", dataDir,
		"os", runtime.GOOS,
	)

	db, err := openDB(dataDir)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	userID, workspaceID, err := seedDefaultData(db)
	if err != nil {
		slog.Error("failed to seed default data", "err", err)
		os.Exit(1)
	}
	slog.Info("default workspace ready",
		"user_id", userID,
		"workspace_id", workspaceID,
	)

	hub := newHub()
	go hub.Run()

	h := newHandler(db, hub, userID, workspaceID)
	router := newRouter(h)

	addr := fmt.Sprintf("127.0.0.1:%s", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "addr", addr, "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
