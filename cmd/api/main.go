package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"neighborparking/internal/audit"
	"neighborparking/internal/config"
	"neighborparking/internal/httpapi"
	"neighborparking/internal/realtime"
	"neighborparking/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	st, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for attempt := 1; attempt <= 15; attempt++ {
		if err = st.Ping(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	hub := realtime.NewHub(log)
	go hub.Run()
	aq := audit.New(st, cfg.AuditWorkers, cfg.AuditBuffer, log)
	api := httpapi.New(cfg, st, hub, aq, log)
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second}
	errCh := make(chan error, 1)
	go func() { log.Info("server listening", "address", cfg.HTTPAddr); errCh <- httpServer.ListenAndServe() }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-stop:
		log.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "error", err)
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	hub.Shutdown()
	if err := aq.Shutdown(shutdownCtx); err != nil {
		log.Error("audit shutdown incomplete", "error", err)
	}
	log.Info("shutdown complete")
}
