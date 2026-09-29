package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type EnvConfig struct {
	DBUrl             string        `env:"DATABASE_URL"`
	Port              string        `env:"PORT"`
	JWTSecretKey      string        `env:"JWT_SECRET_KEY"`
	AccessTokenExpiry time.Duration `env:"ACCESS_TOKEN_EXPIRY"`
}

func MustLoad() EnvConfig {
	var cfg EnvConfig
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	cfg.DBUrl = os.Getenv("DATABASE_URL")
	cfg.Port = os.Getenv("PORT")
	cfg.JWTSecretKey = os.Getenv("JWT_SECRET_KEY")
	cfg.AccessTokenExpiry, err = time.ParseDuration(os.Getenv("ACCESS_TOKEN_EXPIRY"))
	if err != nil {
		log.Fatal("Error parsing ACCESS_TOKEN_EXPIRY")
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
