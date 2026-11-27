package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
)

type Config struct {
	HTTPAddr    string
	LogLevel    slog.Level
	DatabaseURL string
}

// Load takes an explicit environment reader to keep configuration tests isolated.
func Load(getenv func(string) string) (Config, error) {
	value := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	cfg := Config{HTTPAddr: value("HTTP_ADDR", ":8080")}
	_, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil || !validPort(port) {
		return Config{}, errors.New("HTTP_ADDR must contain a host and a port from 1 to 65535")
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(value("LOG_LEVEL", "info"))); err != nil {
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}
	password := getenv("PGPASSWORD")
	if password == "" {
		return Config{}, errors.New("PGPASSWORD is required")
	}
	dbPort := value("PGPORT", "5432")
	if !validPort(dbPort) {
		return Config{}, errors.New("PGPORT must be from 1 to 65535")
	}
	sslMode := value("PGSSLMODE", "require")
	switch sslMode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return Config{}, errors.New("PGSSLMODE must be disable, require, verify-ca, or verify-full")
	}
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(value("PGUSER", "referflow"), password),
		Host:   net.JoinHostPort(value("PGHOST", "127.0.0.1"), dbPort),
		Path:   "/" + value("PGDATABASE", "referflow"),
	}
	query := url.Values{"sslmode": {sslMode}, "connect_timeout": {"5"}}
	dsn.RawQuery = query.Encode()
	cfg.DatabaseURL = dsn.String()
	return cfg, nil
}

func validPort(raw string) bool {
	port, err := strconv.Atoi(raw)
	return err == nil && port > 0 && port <= 65535 && fmt.Sprint(port) == raw
}
