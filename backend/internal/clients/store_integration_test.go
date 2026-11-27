//go:build integration

package clients

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"github.com/azamatbayramov/referflow/backend/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	var random [8]byte
	_, _ = rand.Read(random[:])
	schema := "clients_test_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err := migrations.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
func owner(t *testing.T, pool *pgxpool.Pool, login string) (string, string) {
	t.Helper()
	ctx := context.Background()
	store := auth.Store{Pool: pool}
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	if err := store.CreateAccount(ctx, login, "Тестовый специалист", base64.RawURLEncoding.EncodeToString(secret)); err != nil {
		t.Fatal(err)
	}
	user, _, err := store.Account(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(token))
	if _, err := store.CreateSession(ctx, user.ID, sum[:], nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	return user.ID, token
}
func TestPostgresClientIsolationIdempotencyAndHistory(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	a, token := owner(t, pool, "first")
	b, otherToken := owner(t, pool, "second")
	authHandler, err := auth.NewHandler(auth.Store{Pool: pool}, auth.Options{Origins: []string{"https://referflow.example"}, SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := authHandler.Protect(Handler{Store: Store{Pool: pool}})
	call := func(method, path string, body any, token, csrf, key string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://referflow.example")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	csrfFor := func(token string) string {
		r := httptest.NewRequest("GET", "/api/auth/session", nil)
		r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
		w := httptest.NewRecorder()
		authHandler.ServeHTTP(w, r)
		var session auth.Session
		if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		return session.CSRFToken
	}
	csrf := csrfFor(token)
	otherCSRF := csrfFor(otherToken)
	request := httptest.NewRequest("POST", "/api/clients/search", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != 403 {
		t.Fatal("missing Origin accepted", recorder.Code)
	}
	key, _ := newUUID()
	payload := map[string]string{"name": "Семёнов Тестовый", "phone": "+7 (900) 000-00-01"}
	if w := call("POST", "/api/clients", payload, "", "", key); w.Code != 401 {
		t.Fatal("anonymous access", w.Code)
	}
	if w := call("POST", "/api/clients", payload, token, "", key); w.Code != 403 {
		t.Fatal("missing CSRF", w.Code)
	}
	forged := map[string]string{"name": "Test", "specialist_id": b}
	if w := call("POST", "/api/clients", forged, token, csrf, key); w.Code != 400 {
		t.Fatal("caller controlled owner", w.Code)
	}
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- call("POST", "/api/clients", payload, token, csrf, key) }()
	}
	wg.Wait()
	close(responses)
	var client Client
	created := 0
	for w := range responses {
		if w.Code == 201 {
			created++
		} else if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var c Client
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		if client.ID != "" && client.ID != c.ID {
			t.Fatal("duplicate from replay")
		}
		client = c
	}
	if created != 1 {
		t.Fatal("create must commit once", created)
	}
	if w := call("PUT", "/api/clients/"+client.ID, map[string]any{"name": "Incomplete", "version": 1}, token, csrf, ""); w.Code != 400 {
		t.Fatal("missing update fields accepted", w.Code)
	}
	var count int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM clients WHERE specialist_id=$1`, a).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate rows", count)
	}
	if w := call("POST", "/api/clients", map[string]string{"name": "Different"}, token, csrf, key); w.Code != 409 {
		t.Fatal("idempotency payload conflict", w.Code)
	}
	if w := call("GET", "/api/clients/"+client.ID, nil, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign card exposed", w.Code)
	}
	if w := call("PUT", "/api/clients/"+client.ID, Input{Name: "Foreign", Version: 1}, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign card changed", w.Code)
	}
	for _, q := range []Search{{Query: "СЕМЕНОВ"}, {Query: "9000000001"}, {Phone: "+7 900 000 00 01"}} {
		page, err := (Store{Pool: pool}).Search(ctx, a, q)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("search failed", q, err)
		}
	}
	if page, err := (Store{Pool: pool}).Search(ctx, b, Search{}); err != nil || len(page.Items) != 0 {
		t.Fatal("foreign list exposed")
	}
	// Same contact is allowed for relatives, and the other owner has an independent key.
	if _, _, err := (Store{Pool: pool}).Create(ctx, b, key, Input{Name: "Родственник", Phone: payload["phone"]}); err != nil {
		t.Fatal(err)
	}
	update := Input{Name: "Семёнов Исправленный", Phone: "+7 900 000 00 02", Version: 1}
	first := call("PUT", "/api/clients/"+client.ID, update, token, csrf, "")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	if w := call("PUT", "/api/clients/"+client.ID, update, token, csrf, ""); w.Code != 409 {
		t.Fatal("stale update accepted", w.Code)
	}
	update.Version = 2
	update.Archived = true
	if w := call("PUT", "/api/clients/"+client.ID, update, token, csrf, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	replay := call("POST", "/api/clients", payload, token, csrf, key)
	_ = json.Unmarshal(replay.Body.Bytes(), &client)
	if replay.Code != 200 || client.Version != 3 || !client.Archived || client.Name != update.Name {
		t.Fatal("replay undid edits/archive")
	}
	got, events, err := (Store{Pool: pool}).Get(ctx, a, client.ID)
	if err != nil || got.ID != client.ID || len(events) != 3 || events[0].Action != "archived" {
		t.Fatal("history/identity lost", err, events)
	}
	update.Version = 3
	update.Archived = false
	if _, err := (Store{Pool: pool}).Update(ctx, a, client.ID, update); err != nil {
		t.Fatal(err)
	}
	if page, err := (Store{Pool: pool}).Search(ctx, a, Search{Query: "0000002"}); err != nil || len(page.Items) != 1 || page.Items[0].ID != client.ID {
		t.Fatal("contact edit changed identity")
	}
}

func TestPostgresConcurrentEditsAndSearchPages(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	ownerID, _ := owner(t, pool, "concurrent")
	store := Store{Pool: pool}
	key, _ := newUUID()
	client, _, err := store.Create(ctx, ownerID, key, Input{Name: "Исходный"})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"Первый", "Второй"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, err := store.Update(ctx, ownerID, client.ID, Input{Name: name, Version: 1})
			results <- err
		}(name)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err == ErrConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent edits lost a change", success, conflicts)
	}
	_, events, err := store.Get(ctx, ownerID, client.ID)
	if err != nil || len(events) != 2 {
		t.Fatal("unexpected audit after concurrent edits", err)
	}
	for range 51 {
		key, _ := newUUID()
		if _, _, err := store.Create(ctx, ownerID, key, Input{Name: "Страница"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.Search(ctx, ownerID, Search{})
	if err != nil || len(first.Items) != 50 || !first.HasMore {
		t.Fatal("first page incorrect", err)
	}
	second, err := store.Search(ctx, ownerID, Search{Offset: 50})
	if err != nil || len(second.Items) != 2 || second.HasMore {
		t.Fatal("second page incorrect", err)
	}
	seen := map[string]bool{}
	for _, c := range first.Items {
		seen[c.ID] = true
	}
	for _, c := range second.Items {
		if seen[c.ID] {
			t.Fatal("page repeated a record")
		}
	}
	if _, err := store.Search(ctx, ownerID, Search{Query: "%' OR 1=1 --"}); err != nil {
		t.Fatal("literal search failed", err)
	}
}
