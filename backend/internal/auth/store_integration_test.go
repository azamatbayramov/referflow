//go:build integration

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azamatbayramov/referflow/backend/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "auth_test_" + hex.EncodeToString(random)
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatal("cannot create isolated test schema")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// This random schema is created by this test; no existing data is touched.
		if _, err := admin.Exec(cleanup, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error("test schema cleanup failed")
		}
		admin.Close()
	})
	return pool
}
func TestPostgresMigrationsAndSessions(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if err := migrations.Check(ctx, pool); err == nil {
		t.Fatal("unmigrated database was ready")
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- migrations.Up(ctx, pool) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := migrations.Up(ctx, pool); err != nil {
		t.Fatal("repeated migration failed", err)
	}
	if err := migrations.Check(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != 6 {
		t.Fatal("migration applied more than once", err)
	}
	store := Store{Pool: pool}
	password := strings.Repeat("integration-", 3)
	if err := store.CreateAccount(ctx, "  OWNER ", "Тест", password); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAccount(ctx, "owner", "Другой", password); err == nil {
		t.Fatal("duplicate account accepted")
	}
	user, hash, err := store.Account(ctx, "owner")
	if err != nil || hash == password || !CheckPassword(hash, password) {
		t.Fatal("account persistence failed", err)
	}
	old := digest("old synthetic session token")
	if _, err := store.CreateSession(ctx, user.ID, old, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	current := digest("new synthetic session token")
	if _, err := store.CreateSession(ctx, user.ID, current, old, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Session(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Fatal("rotation kept old token")
	}
	if got, err := store.Session(ctx, current); err != nil || got.User.ID != user.ID {
		t.Fatal("wrong owner", err)
	}
	if _, err := store.CreateSession(ctx, "00000000-0000-4000-8000-000000000000", digest("invalid owner"), current, time.Hour); err == nil {
		t.Fatal("unknown account got a session")
	}
	if _, err := store.Session(ctx, current); err != nil {
		t.Fatal("failed rotation deleted original session")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE token_hash=$1`, current); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Session(ctx, current); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	if _, err := store.CreateSession(ctx, user.ID, current, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Session(ctx, current); !errors.Is(err, ErrNotFound) {
		t.Fatal("logout token still usable")
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET checksum='altered'`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(ctx, pool); err == nil {
		t.Fatal("changed migration was silently accepted")
	}
	if err := migrations.Check(ctx, pool); err == nil {
		t.Fatal("changed migration was ready")
	}
}
func TestPostgresConcurrentLoginBudget(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if err := migrations.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := Store{Pool: pool}
	results := make(chan bool, 30)
	failures := make(chan error, 30)
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, err := store.AllowLogin(ctx, "same-specialist")
			results <- allowed
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for allowed := range results {
		if allowed {
			count++
		}
	}
	if count != 10 {
		t.Fatalf("concurrent requests passed account budget: got %d, want 10", count)
	}
	if _, err := pool.Exec(ctx, `UPDATE login_limits SET resets_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.AllowLogin(ctx, "same-specialist"); err != nil || !allowed {
		t.Fatal("budget did not recover", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE login_limits SET attempts=120 WHERE key='global'`); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.AllowLogin(ctx, "new-specialist"); err != nil || allowed {
		t.Fatal("global budget was bypassed", err)
	}
}

func TestPostgresLoginMigrationPreservesAccounts(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	// Reproduce an installation that already applied the original auth migration.
	initial, err := os.ReadFile("../../migrations/001_auth.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)`, "001_auth.sql", fmt.Sprintf("%x", sha256.Sum256(initial))); err != nil {
		t.Fatal(err)
	}
	password := strings.Repeat("old-account-", 2)
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	const userID = "a79cc233-f07e-4c0e-8be0-cc1d6dc64170"
	const oldIdentifier = "specialist@example.test"
	if _, err := pool.Exec(ctx, `INSERT INTO specialists(id,email,name,password_hash) VALUES($1,$2,$3,$4)`, userID, oldIdentifier, "Существующий специалист", hash); err != nil {
		t.Fatal(err)
	}
	store := Store{Pool: pool}
	tokenHash := digest("synthetic session before login migration")
	if _, err := store.CreateSession(ctx, userID, tokenHash, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AllowLogin(ctx, oldIdentifier); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Check(ctx, pool); err == nil {
		t.Fatal("old schema is ready for new API")
	}
	for range 2 {
		if err := migrations.Up(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrations.Check(ctx, pool); err != nil {
		t.Fatal(err)
	}
	user, savedHash, err := store.Account(ctx, oldIdentifier)
	if err != nil || user.ID != userID || user.Login != oldIdentifier || savedHash != hash || !CheckPassword(savedHash, password) {
		t.Fatal("migration changed account credentials", err)
	}
	if session, err := store.Session(ctx, tokenHash); err != nil || session.User != user {
		t.Fatal("migration lost the existing session", err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT attempts FROM login_limits WHERE key=$1`, fmt.Sprintf("account:%x", sha256.Sum256([]byte(oldIdentifier)))).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("migration lost the account login budget", err)
	}
	if err := store.CreateAccount(ctx, "  Doctor_1  ", "Новый специалист", strings.Repeat("x", 8)); err != nil {
		t.Fatal("plain login with 8-character password rejected", err)
	}
	if err := store.CreateAccount(ctx, "DOCTOR_1", "Дубликат", strings.Repeat("y", 8)); err == nil {
		t.Fatal("case variant created a duplicate account")
	}
}
