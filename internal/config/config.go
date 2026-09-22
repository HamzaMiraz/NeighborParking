package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment     string
	HTTPAddr        string
	DatabaseURL     string
	SessionTTL      time.Duration
	CookieSecure    bool
	AllowedOrigin   string
	AuditWorkers    int
	AuditBuffer     int
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	c := Config{
		Environment:     env("APP_ENV", "development"),
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		AllowedOrigin:   env("ALLOWED_ORIGIN", "http://localhost:8080"),
		SessionTTL:      duration("SESSION_TTL", 7*24*time.Hour),
		ShutdownTimeout: duration("SHUTDOWN_TIMEOUT", 10*time.Second),
		CookieSecure:    boolean("COOKIE_SECURE", false),
		AuditWorkers:    integer("AUDIT_WORKERS", 2),
		AuditBuffer:     integer("AUDIT_BUFFER", 256),
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if c.AuditWorkers < 1 || c.AuditBuffer < 1 {
		return Config{}, errors.New("AUDIT_WORKERS and AUDIT_BUFFER must be positive")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}

func boolean(key string, fallback bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}

func integer(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}
