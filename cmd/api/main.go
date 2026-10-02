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
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "github.com/MohakGupta2004/vipin-lms-backend/docs"
)

const (
	serverShutdownTimeout = 15 * time.Second
)

// @title			Vipin LMS API
// @version		1.0
// @description	Backend API for the Vipin LMS platform.
// @BasePath		/api/v1
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

	pdfStore, err := storage.NewGCSPDFStore(ctx, cfg.GCSBucketName)
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()

	// repositories
	userRepo := models.NewUserRepository(db)
	postRepo := models.NewPostRepository(db)
	courseRepo := models.NewCourseRepository(db)
	examRepo := models.NewExamRepository(db)
	enrollmentRepo := models.NewEnrollmentRepository(db)
	courseNoteRepo := models.NewCourseNoteRepository(db)

	// services
	authService := service.NewAuthService(userRepo, ctx, cfg.JWTSecretKey, cfg.AccessTokenExpiry, cfg.RefreshSecretKey, cfg.RefreshTokenExpiry) // Set the access token expiry duration

	postService := service.NewPostService(postRepo)
	courseService := service.NewCourseService(courseRepo)
	enrollmentService := service.NewEnrollmentService(enrollmentRepo)
	courseNoteService := service.NewCourseNoteService(courseNoteRepo, pdfStore)

	// middlewares
	authMiddleware := middleware.NewAuthMiddleware(cfg.JWTSecretKey, authService, userRepo)

	// handlerFunctions
	authHandler := handlers.NewAuthHandler(userRepo, authService)
	postHandler := handlers.NewPostHandler(postService)
	courseHandler := handlers.NewCourseHandler(courseService)
	examHandler := handlers.NewExamHandler(examRepo)
	enrollmentHandler := handlers.NewEnrollmentHandler(enrollmentService)
	courseNoteHandler := handlers.NewCourseNoteHandler(courseNoteService)

	// handlers
	mux.HandleFunc("GET /api/v1/healthz", handlers.HealthHandler)
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))

	// auth routes
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.LoginHandler)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.RefreshTokenHandler)

	// post routes (login required)
	mux.Handle("POST /api/v1/posts", authMiddleware.RequireAuth(http.HandlerFunc(postHandler.CreatePost)))
	mux.Handle("GET /api/v1/posts", authMiddleware.RequireAuth(http.HandlerFunc(postHandler.ListFeed)))
	mux.Handle("DELETE /api/v1/posts/{id}", authMiddleware.RequireAuth(http.HandlerFunc(postHandler.DeletePost)))

	// course routes (admin only, checked in the service)
	mux.Handle("POST /api/v1/courses", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.CreateCourse)))
	mux.Handle("GET /api/v1/courses", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.ListCourses)))
	mux.Handle("PATCH /api/v1/courses/{id}/status", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.UpdateCourseStatus))) // instructor only

	// exam routes (any logged-in user)
	mux.Handle("GET /api/v1/exams", authMiddleware.RequireAuth(http.HandlerFunc(examHandler.ListExams)))

	// enrollment routes (admin only, checked in the service)
	mux.Handle("POST /api/v1/enrollments", authMiddleware.RequireAuth(http.HandlerFunc(enrollmentHandler.CreateEnrollment)))
	mux.Handle("GET /api/v1/enrollments", authMiddleware.RequireAuth(http.HandlerFunc(enrollmentHandler.ListEnrollments)))
	mux.Handle("PATCH /api/v1/enrollments/{id}", authMiddleware.RequireAuth(http.HandlerFunc(enrollmentHandler.UpdateEnrollmentStatus)))

	// course note routes (instructor uploads; instructor and enrolled students read)
	mux.Handle("POST /api/v1/courses/{id}/notes", authMiddleware.RequireAuth(http.HandlerFunc(courseNoteHandler.UploadNote)))
	mux.Handle("GET /api/v1/courses/{id}/notes", authMiddleware.RequireAuth(http.HandlerFunc(courseNoteHandler.ListNotes)))
	mux.Handle("GET /api/v1/notes/{id}/file", authMiddleware.RequireAuth(http.HandlerFunc(courseNoteHandler.DownloadNote)))
	mux.Handle("DELETE /api/v1/notes/{id}", authMiddleware.RequireAuth(http.HandlerFunc(courseNoteHandler.DeleteNote)))

	srv := http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      middleware.CORS(cfg.CORSAllowedOrigins, mux),
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

	if err := pdfStore.Close(); err != nil {
		logger.Error("closing storage client failed", "err", err)
	}
	db.Close()
	logger.Info("bye")
}
