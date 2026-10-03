package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type EnvConfig struct {
	DBUrl              string        `env:"DATABASE_URL"`
	Port               string        `env:"PORT"`
	JWTSecretKey       string        `env:"JWT_SECRET_KEY"`
	AccessTokenExpiry  time.Duration `env:"ACCESS_TOKEN_EXPIRY"`
	RefreshTokenExpiry time.Duration `env:"REFRESH_TOKEN_EXPIRY"`
	RefreshSecretKey   string        `env:"REFRESH_SECRET_KEY"`
	CORSAllowedOrigins []string      `env:"CORS_ALLOWED_ORIGINS"`
	GCSEnabled         bool          `env:"GCS_ENABLE"` // PDF uploads work only when true
	GCSBucketName      string        `env:"GCS_BUCKET_NAME"`
	GCPProjectID       string        `env:"GCP_PROJECT_ID"`      // project that runs the Transcoder API
	TranscoderLocation string        `env:"TRANSCODER_LOCATION"` // Transcoder region, e.g. asia-south1
	CDNDomain          string        `env:"CDN_DOMAIN"`          // Cloud CDN origin serving the bucket, e.g. https://cdn.example.com
	CDNKeyName         string        `env:"CDN_KEY_NAME"`        // signed URL key name on the CDN backend bucket
	CDNSigningKey      string        `env:"CDN_SIGNING_KEY"`     // base64url signed URL key secret
}

func MustLoad() EnvConfig {
	var cfg EnvConfig
	// .env is optional; env vars may come from the environment (e.g. docker compose)
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using environment variables")
	}
	cfg.DBUrl = os.Getenv("DATABASE_URL")
	cfg.Port = os.Getenv("PORT")
	cfg.JWTSecretKey = os.Getenv("JWT_SECRET_KEY")
	cfg.AccessTokenExpiry, err = time.ParseDuration(os.Getenv("ACCESS_TOKEN_EXPIRY"))
	if err != nil {
		log.Fatal("Error parsing ACCESS_TOKEN_EXPIRY")
	}
	days, err := strconv.Atoi(os.Getenv("REFRESH_TOKEN_EXPIRY"))
	if err != nil {
		// Handle error if the environment variable isn't a valid number
		days = 0
	}

	// Correctly multiply and cast to time.Duration
	cfg.RefreshTokenExpiry = time.Duration(days) * 24 * time.Hour
	if err != nil {
		log.Fatal("Error parsing REFRESH_TOKEN_EXPIRY")
	}
	cfg.RefreshSecretKey = os.Getenv("REFRESH_SECRET_KEY")
	// comma-separated list of browser origins allowed to call the API
	origins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if origins == "" {
		origins = "http://localhost:3001"
	}
	cfg.CORSAllowedOrigins = strings.Split(origins, ",")
	cfg.GCSEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv("GCS_ENABLE")), "true")
	cfg.GCSBucketName = os.Getenv("GCS_BUCKET_NAME")
	if cfg.GCSBucketName == "" {
		cfg.GCSBucketName = "vipin-lms"
	}
	cfg.GCPProjectID = os.Getenv("GCP_PROJECT_ID")
	cfg.TranscoderLocation = os.Getenv("TRANSCODER_LOCATION")
	if cfg.GCSEnabled && cfg.GCPProjectID == "" {
		log.Fatal("GCP_PROJECT_ID is not set but GCS_ENABLE is true")
	}
	cfg.CDNDomain = os.Getenv("CDN_DOMAIN")
	cfg.CDNKeyName = os.Getenv("CDN_KEY_NAME")
	cfg.CDNSigningKey = os.Getenv("CDN_SIGNING_KEY")
	if cfg.GCSEnabled && cfg.CDNDomain == "" {
		log.Fatal("CDN_DOMAIN is not set but GCS_ENABLE is true")
	}
	if cfg.GCSEnabled && cfg.CDNKeyName == "" {
		log.Fatal("CDN_KEY_NAME is not set but GCS_ENABLE is true")
	}
	if cfg.GCSEnabled && cfg.CDNSigningKey == "" {
		log.Fatal("CDN_SIGNING_KEY is not set but GCS_ENABLE is true")
	}
	if cfg.GCSEnabled && cfg.TranscoderLocation == "" {
		log.Fatal("TRANSCODER_LOCATION is not set but GCS_ENABLE is true")
	}
	if cfg.DBUrl == "" {
		log.Fatal("DATABASE_URL is not set in the environment variables")
	}
	if cfg.Port == "" {
		log.Fatal("PORT is not set in the environment variables")
	}
	if cfg.JWTSecretKey == "" {
		log.Fatal("JWT_SECRET_KEY is not set in the environment variables")
	}
	if cfg.AccessTokenExpiry == 0 {
		log.Fatal("ACCESS_TOKEN_EXPIRY is not set in the environment variables")
	}
	fmt.Println("Configuration loaded successfully")
	return cfg
}
