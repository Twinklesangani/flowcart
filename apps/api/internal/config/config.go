package config

import (
	"errors"
	"os"
	"time"
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AppEnv          string
}

func Load() (Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	accessTokenTTL, err := loadDuration("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTokenTTL, err := loadDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "development"
	}

	return Config{
		Port:            port,
		DatabaseURL:     databaseURL,
		JWTSecret:       jwtSecret,
		AccessTokenTTL:  accessTokenTTL,
		RefreshTokenTTL: refreshTokenTTL,
		AppEnv:          appEnv,
	}, nil
}

func loadDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, errors.New(name + " must be a valid duration")
	}
	return duration, nil
}
