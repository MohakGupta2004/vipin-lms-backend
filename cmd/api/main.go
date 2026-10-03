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

	"github.com/MohakGupta2004/vipin-lms-backend/internal/cdn"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/config"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/database"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/handlers"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/transcoder"
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

	// PDF storage is opt-in: without GCS_ENABLE=true no Google client is created and no credentials are needed.
	var pdfStore storage.PDFStore = storage.DisabledPDFStore{}
	if cfg.GCSEnabled {
		pdfStore, err = storage.NewGCSPDFStore(ctx, cfg.GCSBucketName)
		if err != nil {
			panic(err)
		}
		fmt.Println("GCS STORAGE ENABLED, BUCKET:", cfg.GCSBucketName)
	} else {
		fmt.Println("GCS STORAGE DISABLED (set GCS_ENABLE=true to enable PDF uploads)")
	}

	// Video storage and transcoding follow the same switch.
	var videoStore storage.VideoStore = storage.DisabledVideoStore{}
	var transcodeClient *transcoder.Client
	var cdnSigner *cdn.Signer
	if cfg.GCSEnabled {
		cdnSigner, err = cdn.NewSigner(cfg.CDNDomain, cfg.CDNKeyName, cfg.CDNSigningKey)
		if err != nil {
			panic(err)
		}
		videoStore, err = storage.NewGCSVideoStore(ctx, cfg.GCSBucketName)
		if err != nil {
			panic(err)
		}
		transcodeClient, err = transcoder.New(ctx, cfg.GCPProjectID, cfg.TranscoderLocation)
		if err != nil {
			panic(err)
		}
	}

	mux := http.NewServeMux()

	// repositories
	userRepo := models.NewUserRepository(db)
	postRepo := models.NewPostRepository(db)
	courseRepo := models.NewCourseRepository(db)
	examRepo := models.NewExamRepository(db)
	enrollmentRepo := models.NewEnrollmentRepository(db)
	lessonRepo := models.NewLessonRepository(db)
	noteRepo := models.NewNoteRepository(db)
	quizRepo := models.NewQuizRepository(db)
	videoRepo := models.NewVideoRepository(db)

	// services
	authService := service.NewAuthService(userRepo, ctx, cfg.JWTSecretKey, cfg.AccessTokenExpiry, cfg.RefreshSecretKey, cfg.RefreshTokenExpiry) // Set the access token expiry duration

	postService := service.NewPostService(postRepo)
	courseService := service.NewCourseService(courseRepo)
	enrollmentService := service.NewEnrollmentService(enrollmentRepo)
	lessonService := service.NewLessonService(lessonRepo, noteRepo, pdfStore)
	noteService := service.NewNoteService(noteRepo, lessonRepo, pdfStore)
	quizService := service.NewQuizService(quizRepo, lessonRepo)
	userService := service.NewUserService(userRepo)

	// transcoding runs on an in-process queue; it stays unused (and nothing is enqueued) when GCS is disabled
	var transcodeQueue *service.TranscodeQueue
	if transcodeClient != nil {
		transcodeQueue = service.NewTranscodeQueue(videoRepo, videoStore, transcodeClient)
		transcodeQueue.Start(ctx)
	}
	var videoEnqueuer service.TranscodeEnqueuer = disabledEnqueuer{}
	if transcodeQueue != nil {
		videoEnqueuer = transcodeQueue
	}
	videoService := service.NewVideoService(videoRepo, lessonRepo, videoStore, videoEnqueuer, cdnSigner)

	// middlewares
	authMiddleware := middleware.NewAuthMiddleware(cfg.JWTSecretKey, authService, userRepo)

	// handlerFunctions
	authHandler := handlers.NewAuthHandler(userRepo, authService)
	postHandler := handlers.NewPostHandler(postService)
	courseHandler := handlers.NewCourseHandler(courseService)
	examHandler := handlers.NewExamHandler(examRepo)
	enrollmentHandler := handlers.NewEnrollmentHandler(enrollmentService)
	lessonHandler := handlers.NewLessonHandler(lessonService)
	noteHandler := handlers.NewNoteHandler(noteService)
	quizHandler := handlers.NewQuizHandler(quizService)
	userHandler := handlers.NewUserHandler(userService)
	videoHandler := handlers.NewVideoHandler(videoService)

	// handlers
	mux.HandleFunc("GET /api/v1/healthz", handlers.HealthHandler)
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))

	// auth routes
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.LoginHandler)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.RefreshTokenHandler)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.LogoutHandler) // no auth: must work with an expired session
	mux.Handle("GET /api/v1/auth/me", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.MeHandler)))

	// user routes (admin only, checked in the service)
	mux.Handle("GET /api/v1/users", authMiddleware.RequireAuth(http.HandlerFunc(userHandler.ListUsers)))

	// post routes (login required)
	mux.Handle("POST /api/v1/posts", authMiddleware.RequireAuth(http.HandlerFunc(postHandler.CreatePost)))
	mux.Handle("GET /api/v1/posts", authMiddleware.RequireAuth(http.HandlerFunc(postHandler.ListFeed)))
	mux.Handle("DELETE /api/v1/posts/{id}", authMiddleware.RequireAuth(http.HandlerFunc(postHandler.DeletePost)))

	// course routes (create/list all: admin only; edit/delete/status: course owner; checked in the service)
	mux.Handle("POST /api/v1/courses", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.CreateCourse)))
	mux.Handle("GET /api/v1/courses", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.ListCourses)))
	mux.Handle("GET /api/v1/courses/{id}", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.GetCourse)))
	mux.Handle("PATCH /api/v1/courses/{id}", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.UpdateCourse)))
	mux.Handle("DELETE /api/v1/courses/{id}", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.DeleteCourse)))
	mux.Handle("PATCH /api/v1/courses/{id}/status", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.UpdateCourseStatus)))
	mux.Handle("GET /api/v1/me/courses", authMiddleware.RequireAuth(http.HandlerFunc(courseHandler.ListMyCourses)))

	// exam routes (any logged-in user)
	mux.Handle("GET /api/v1/exams", authMiddleware.RequireAuth(http.HandlerFunc(examHandler.ListExams)))

	// enrollment routes (admin only, checked in the service)
	mux.Handle("POST /api/v1/enrollments", authMiddleware.RequireAuth(http.HandlerFunc(enrollmentHandler.CreateEnrollment)))
	mux.Handle("GET /api/v1/enrollments", authMiddleware.RequireAuth(http.HandlerFunc(enrollmentHandler.ListEnrollments)))
	mux.Handle("PATCH /api/v1/enrollments/{id}", authMiddleware.RequireAuth(http.HandlerFunc(enrollmentHandler.UpdateEnrollmentStatus)))

	// lessons (chapters) and their PDF notes (course owner writes; owner and enrolled students read)
	mux.Handle("POST /api/v1/courses/{id}/lessons", authMiddleware.RequireAuth(http.HandlerFunc(lessonHandler.CreateLesson)))
	mux.Handle("GET /api/v1/courses/{id}/lessons", authMiddleware.RequireAuth(http.HandlerFunc(lessonHandler.ListLessons)))
	mux.Handle("PATCH /api/v1/lessons/{id}", authMiddleware.RequireAuth(http.HandlerFunc(lessonHandler.UpdateLesson)))
	mux.Handle("DELETE /api/v1/lessons/{id}", authMiddleware.RequireAuth(http.HandlerFunc(lessonHandler.DeleteLesson)))
	mux.Handle("POST /api/v1/lessons/{id}/notes", authMiddleware.RequireAuth(http.HandlerFunc(noteHandler.UploadNote)))
	mux.Handle("GET /api/v1/courses/{id}/notes", authMiddleware.RequireAuth(http.HandlerFunc(noteHandler.ListNotes)))
	mux.Handle("GET /api/v1/notes/{id}/file", authMiddleware.RequireAuth(http.HandlerFunc(noteHandler.DownloadNote)))
	mux.Handle("DELETE /api/v1/notes/{id}", authMiddleware.RequireAuth(http.HandlerFunc(noteHandler.DeleteNote)))

	// lesson videos (course owner uploads straight to the bucket, confirms, then polls until transcoded)
	mux.Handle("POST /api/v1/lessons/{id}/videos", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.RequestUpload)))
	mux.Handle("POST /api/v1/videos/{id}/confirm", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.ConfirmUpload)))
	mux.Handle("POST /api/v1/videos/{id}/retry", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.RetryTranscode)))
	mux.Handle("GET /api/v1/videos/{id}", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.GetVideo)))
	// playback: signed CDN URL + video lists for owners and students
	mux.Handle("GET /api/v1/videos/{id}/stream", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.StreamVideo)))
	mux.HandleFunc("GET /api/v1/videos/{id}/hls/{file}", videoHandler.VideoPlaylist) // no auth: the signed query is the credential
	mux.Handle("GET /api/v1/lessons/{id}/videos", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.ListLessonVideos)))
	mux.Handle("GET /api/v1/courses/{id}/videos", authMiddleware.RequireAuth(http.HandlerFunc(videoHandler.ListCourseVideos)))

	// quizzes on lessons (course owner creates and edits; enrolled students take them)
	mux.Handle("POST /api/v1/lessons/{id}/quizzes", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.CreateQuiz)))
	mux.Handle("GET /api/v1/lessons/{id}/quizzes", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.ListQuizzes)))
	mux.Handle("PATCH /api/v1/quizzes/{id}/status", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.UpdateQuizStatus)))
	mux.Handle("GET /api/v1/quizzes/{id}", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.GetQuiz)))
	mux.Handle("PATCH /api/v1/quizzes/{id}", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.UpdateQuiz)))
	mux.Handle("DELETE /api/v1/quizzes/{id}", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.DeleteQuiz)))
	mux.Handle("POST /api/v1/quizzes/{id}/attempts", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.SubmitAttempt)))
	mux.Handle("GET /api/v1/quizzes/{id}/attempts", authMiddleware.RequireAuth(http.HandlerFunc(quizHandler.ListAttempts)))

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

	// ctx is already cancelled, so the transcode workers are stopping; wait for them before closing the DB
	if transcodeQueue != nil {
		transcodeQueue.Wait()
	}
	if transcodeClient != nil {
		if err := transcodeClient.Close(); err != nil {
			logger.Error("closing transcoder client failed", "err", err)
		}
	}
	if err := videoStore.Close(); err != nil {
		logger.Error("closing video storage client failed", "err", err)
	}
	if err := pdfStore.Close(); err != nil {
		logger.Error("closing storage client failed", "err", err)
	}
	db.Close()
	logger.Info("bye")
}

// disabledEnqueuer is used when GCS is off. Uploads are refused earlier, so it is never reached.
type disabledEnqueuer struct{}

func (disabledEnqueuer) Enqueue(context.Context, string) error { return storage.ErrStorageDisabled }
