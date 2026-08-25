package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	ListenAddr      string
	DatabasePath    string
	SessionTTL      time.Duration
	WorkerInterval  time.Duration
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:   env("LISTEN_ADDR", ":8080"),
		DatabasePath: env("DATABASE_PATH", "data/youstay.db"),
	}
	var err error
	if cfg.SessionTTL, err = duration("SESSION_TTL", 12*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerInterval, err = duration("WORKER_INTERVAL", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ListenAddr == "" {
		return Config{}, fmt.Errorf("LISTEN_ADDR cannot be empty")
	}
	if cfg.DatabasePath == "" {
		return Config{}, fmt.Errorf("DATABASE_PATH cannot be empty")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := env(key, fallback.String())
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}
