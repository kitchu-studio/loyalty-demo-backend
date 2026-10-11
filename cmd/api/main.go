package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kitchu-studio/loyalty-demo-backend/internal/ai"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/auth"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/config"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/httpapi"
	"github.com/kitchu-studio/loyalty-demo-backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("cannot connect to database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	assistant := ai.NewClient(cfg.AnthropicAPIKey, cfg.AnthropicModel, cfg.AnthropicURL)
	if assistant.Enabled() {
		logger.Info("assistant uses the Claude API")
	} else {
		logger.Info("ANTHROPIC_API_KEY not set: assistant answers in demo mode")
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	router := httpapi.NewRouter(st, tokens, httpapi.Options{
		AppEnv:       cfg.AppEnv,
		ShopName:     cfg.ShopName,
		BahtPerPoint: cfg.BahtPerPoint,
		AdminEmails:  cfg.AdminEmails,
		Assistant:    assistant,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      40 * time.Second, // the assistant can take a while to answer
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server started", "addr", srv.Addr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
