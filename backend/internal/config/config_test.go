package config

import (
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	env := map[string]string{"PGPASSWORD": "p@ss:/?#word", "LOG_LEVEL": "debug", "PGHOST": "::1"}
	cfg, err := Load(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if password != env["PGPASSWORD"] || u.Hostname() != "::1" || u.Port() != "5432" {
		t.Fatal("database connection details did not survive URL encoding")
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelDebug || u.Query().Get("sslmode") != "require" {
		t.Fatal("unexpected configuration defaults")
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"PGPASSWORD", ""}, {"HTTP_ADDR", "8080"}, {"HTTP_ADDR", ":0"},
		{"HTTP_ADDR", ":65536"}, {"LOG_LEVEL", "unknown"}, {"PGPORT", "oops"},
		{"PGPORT", "-1"}, {"PGSSLMODE", "prefer"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			env := map[string]string{"PGPASSWORD": "do-not-log-this"}
			env[tc.key] = tc.value
			_, err := Load(func(key string) string { return env[key] })
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected an error naming %s, got %v", tc.key, err)
			}
			if strings.Contains(err.Error(), "do-not-log-this") {
				t.Fatal("configuration error leaked the password")
			}
		})
	}
}

func TestSessionSettings(t *testing.T) {
	for _, tc := range []struct {
		origins, secure, ttl string
		valid                bool
	}{
		{"https://referflow.example", "true", "12h", true},
		{"http://127.0.0.1:8180,http://localhost:5173", "false", "1m", true},
		{"https://referflow.example", "false", "12h", false},
		{"http://referflow.example", "true", "12h", false},
		{"https://referflow.example/", "true", "12h", false},
		{"https://user:secret@referflow.example", "true", "12h", false},
		{"https://referflow.example?", "true", "12h", false},
		{"https://referflow.example", "invalid", "12h", false},
		{"https://referflow.example", "true", "0s", false},
		{"https://referflow.example", "true", "900h", false},
	} {
		env := map[string]string{"PGPASSWORD": "test-config-value", "APP_ORIGINS": tc.origins, "SESSION_COOKIE_SECURE": tc.secure, "SESSION_TTL": tc.ttl}
		_, err := Load(func(key string) string { return env[key] })
		if (err == nil) != tc.valid {
			t.Errorf("origins %q, secure %q, ttl %q: %v", tc.origins, tc.secure, tc.ttl, err)
		}
	}
}
