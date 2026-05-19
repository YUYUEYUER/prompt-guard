package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"prompt-guard/internal/server"
	"prompt-guard/internal/version"
)

func main() {
	configPath := flag.String("config", "configs/config.example.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	app, err := server.New(*configPath, logger)
	if err != nil {
		logger.Error("failed to initialize server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("starting prompt-guard",
		slog.String("version", version.Version),
		slog.String("listen", app.ListenAddr()),
		slog.String("config", *configPath),
	)

	httpServer := &http.Server{
		Addr:              app.ListenAddr(),
		Handler:           app.Handler(),
		ReadTimeout:       app.ReadTimeout(),
		WriteTimeout:      app.WriteTimeout(),
		IdleTimeout:       app.IdleTimeout(),
		MaxHeaderBytes:    app.MaxHeaderBytes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server exited unexpectedly", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
