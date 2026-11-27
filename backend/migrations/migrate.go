package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

type Checker struct{ Pool *pgxpool.Pool }

func (c Checker) Ping(ctx context.Context) error { return Check(ctx, c.Pool) }

// Up is an explicit operator command, never called by an API replica.
// A transaction-scoped lock serializes simultaneous migration commands.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("cannot begin migration")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(74633791)`); err != nil {
		return errors.New("cannot lock migrations")
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return errors.New("cannot initialize migrations")
	}
	entries, err := files.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sql, err := files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(sql))
		var applied string
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, entry.Name()).Scan(&exists); err != nil {
			return errors.New("cannot read migration state")
		}
		if exists {
			if err := tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, entry.Name()).Scan(&applied); err != nil {
				return errors.New("cannot read migration checksum")
			}
			if applied != checksum {
				return fmt.Errorf("migration %s checksum mismatch", entry.Name())
			}
			continue
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s failed", entry.Name())
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)`, entry.Name(), checksum); err != nil {
			return errors.New("cannot record migration")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("cannot commit migrations")
	}
	return nil
}

func Check(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := files.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		var checksum string
		if err := pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, entry.Name()).Scan(&checksum); err != nil {
			return errors.New("database schema is not ready")
		}
		if checksum != fmt.Sprintf("%x", sha256.Sum256(data)) {
			return errors.New("database schema checksum mismatch")
		}
	}
	return nil
}
