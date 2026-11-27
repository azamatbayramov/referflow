package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"github.com/azamatbayramov/referflow/backend/internal/clients"
	"github.com/azamatbayramov/referflow/backend/internal/config"
	"github.com/azamatbayramov/referflow/backend/internal/coupons"
	"github.com/azamatbayramov/referflow/backend/internal/httpserver"
	"github.com/azamatbayramov/referflow/backend/internal/postgres"
	"github.com/azamatbayramov/referflow/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	authHandler, err := auth.NewHandler(auth.Store{Pool: pool}, auth.Options{Origins: cfg.AppOrigins, SecureCookie: cfg.SecureCookie, SessionTTL: cfg.SessionTTL})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.NewHandler(migrations.Checker{Pool: pool}, authHandler, authHandler.Protect(clients.Handler{Store: clients.Store{Pool: pool}}), authHandler.Protect(coupons.Handler{Store: coupons.Store{Pool: pool}}), authHandler.Protect(coupons.RedemptionHandler{Store: coupons.Store{Pool: pool}}), coupons.PublicHandler{Store: coupons.Store{Pool: pool}}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	serverError := make(chan error, 1)
	go func() { serverError <- server.ListenAndServe() }()
	logger.Info("API starting", "address", cfg.HTTPAddr)
	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-ctx.Done():
		stop()
		logger.Info("API shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown HTTP: %w", err)
		}
	}
	return nil
}
