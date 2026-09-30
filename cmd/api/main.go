package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/config"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/database"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/handlers"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	serverShutdownTimeout = 15 * time.Second
)

func main() {
	cfg := config.MustLoad()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Connect(cfg.DBUrl)
	if err != nil {
		panic(err)
	}

	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})

	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	fmt.Println("DATABASE CONNECTED")
	mux := http.NewServeMux()

	// repositories
	userRepo := models.NewUserRepository(db)

	// services
	authService := service.NewAuthService(userRepo, ctx, cfg.JWTSecretKey, cfg.AccessTokenExpiry) // Set the access token expiry duration

	// middlewares
	authMiddleware := middleware.NewAuthMiddleware(cfg.JWTSecretKey, authService, userRepo)

	// handlerFunctions
	authHandler := handlers.NewAuthHandler(userRepo, authService)

	// handlers
	mux.Handle("GET /api/v1/healthz", authMiddleware.RequireAuth(http.HandlerFunc(handlers.HealthHandler)))
	mux.HandleFunc("POST /api/v1/register", authHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/login", authHandler.LoginHandler)

	srv := http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 30,
		IdleTimeout:  time.Second * 60,
	}
	go func() {
		log.Printf("server is listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()
	// escape hatch double ctrl+c
	stop()

	logger.Info("shutting down...")
	srvShutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(srvShutdownCtx); err != nil {
		logger.Error("server shutdown failed", "err", err)
	}

	db.Close()
	logger.Info("bye")
}
