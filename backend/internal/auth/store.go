package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type Session struct {
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
	CSRFToken string    `json:"csrf_token"`
}

type Store struct{ Pool *pgxpool.Pool }

func (s Store) CreateAccount(ctx context.Context, login, name, password string) error {
	login, err := NormalizeLogin(login)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120 {
		return errors.New("name must contain 1 to 120 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return errors.New("cannot generate account ID")
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	_, err = s.Pool.Exec(ctx, `INSERT INTO specialists(id,login,name,password_hash) VALUES($1,$2,$3,$4)`, uuid, login, name, hash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errors.New("account already exists")
	}
	if err != nil {
		return errors.New("cannot create account; check database migrations")
	}
	return nil
}

func (s Store) Account(ctx context.Context, login string) (User, string, error) {
	var user User
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT id::text,login,name,password_hash FROM specialists WHERE login=$1`, login).Scan(&user.ID, &user.Login, &user.Name, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	return user, hash, err
}

// AllowLogin limits all attempts (including successful ones), without revealing
// account existence. PostgreSQL makes these budgets effective across replicas.
func (s Store) AllowLogin(ctx context.Context, login string) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `DELETE FROM login_limits WHERE resets_at <= now()`); err != nil {
		return false, err
	}
	budgets := []struct {
		key            string
		limit, seconds int
	}{
		{"global", 120, 60},
		{fmt.Sprintf("account:%x", sha256.Sum256([]byte(login))), 10, 900},
	}
	allowed := true
	for _, budget := range budgets {
		var attempts int
		err := tx.QueryRow(ctx, `INSERT INTO login_limits(key,attempts,resets_at) VALUES($1,1,now()+make_interval(secs => $2::integer))
            ON CONFLICT(key) DO UPDATE SET attempts=LEAST(login_limits.attempts+1,$3+1)
            RETURNING attempts`, budget.key, budget.seconds, budget.limit).Scan(&attempts)
		if err != nil {
			return false, err
		}
		if attempts > budget.limit {
			allowed = false
			break
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return allowed, nil
}

func (s Store) CreateSession(ctx context.Context, userID string, tokenHash, oldHash []byte, ttl time.Duration) (time.Time, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now() OR token_hash=$1`, oldHash); err != nil {
		return time.Time{}, err
	}
	var expires time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO sessions(token_hash,specialist_id,expires_at)
        VALUES($1,$2,now()+make_interval(secs => $3::integer)) RETURNING expires_at`, tokenHash, userID, int(ttl.Seconds())).Scan(&expires); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return expires, nil
}

func (s Store) Session(ctx context.Context, hash []byte) (Session, error) {
	var session Session
	err := s.Pool.QueryRow(ctx, `SELECT u.id::text,u.login,u.name,s.expires_at FROM sessions s
        JOIN specialists u ON u.id=s.specialist_id WHERE s.token_hash=$1 AND s.expires_at>now()`, hash).
		Scan(&session.User.ID, &session.User.Login, &session.User.Name, &session.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return session, err
}

func (s Store) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hash)
	return err
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
