//go:build integration

package coupons

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"github.com/azamatbayramov/referflow/backend/internal/clients"
	"github.com/azamatbayramov/referflow/backend/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
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
	schema := "coupons_test_" + hex.EncodeToString(random[:])
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
func baseInput() Input {
	one := 1
	three := 3
	return Input{DiscountType: "percent", Value: "12.50", TotalLimit: &three, PerClientLimit: &one, Restrictions: "Тестовое ограничение\nВторая строка"}
}
func TestPostgresCouponIssuanceRevocationAndIsolation(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	a, token := owner(t, pool, "first")
	b, otherToken := owner(t, pool, "second")
	store := Store{Pool: pool}
	key, _ := newUUID()
	in := baseInput()
	in.Code = "12-34"
	refKey, _ := newUUID()
	ref, _, err := (clients.Store{Pool: pool}).Create(ctx, a, refKey, clients.Input{Name: "Рекомендатель Тестовый"})
	if err != nil {
		t.Fatal(err)
	}
	in.ReferrerClientID = &ref.ID
	hAuth, err := auth.NewHandler(auth.Store{Pool: pool}, auth.Options{Origins: []string{"https://referflow.example"}, SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	csrfFor := func(token string) string {
		r := httptest.NewRequest("GET", "/api/auth/session", nil)
		r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
		w := httptest.NewRecorder()
		hAuth.ServeHTTP(w, r)
		var s auth.Session
		_ = json.Unmarshal(w.Body.Bytes(), &s)
		return s.CSRFToken
	}
	csrf, otherCSRF := csrfFor(token), csrfFor(otherToken)
	h := hAuth.Protect(Handler{Store: store})
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
	if w := call("POST", "/api/coupons", in, "", "", key); w.Code != 401 {
		t.Fatal("anonymous issuance", w.Code)
	}
	if w := call("POST", "/api/coupons", in, token, "", key); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call("POST", "/api/coupons", in, token, csrf, key) }()
	}
	wg.Wait()
	close(results)
	var c Coupon
	created := 0
	for w := range results {
		if w.Code == 201 {
			created++
		} else if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var got Coupon
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if c.ID != "" && c.ID != got.ID {
			t.Fatal("duplicate issuance")
		}
		c = got
	}
	if created != 1 || c.Code != "001234" || c.ID[14] != '4' || c.ReferrerName == nil || *c.ReferrerName != "Рекомендатель Тестовый" {
		t.Fatal("incorrect issued coupon", created, c)
	}
	if w := call("GET", "/api/coupons/"+c.ID, nil, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign coupon exposed", w.Code)
	}
	if w := call("POST", "/api/coupons/"+c.ID+"/revoke", map[string]string{"reason": "Foreign"}, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign revocation", w.Code)
	}
	foreignKey, _ := newUUID()
	if _, _, err := store.Create(ctx, b, foreignKey, in); err != ErrInvalid {
		t.Fatal("foreign referrer accepted", err)
	}
	takenKey, _ := newUUID()
	if _, _, err := store.Create(ctx, a, takenKey, in); err != ErrCodeTaken {
		t.Fatal("code reused", err)
	}
	altered := in
	altered.Value = "13"
	if _, _, err := store.Create(ctx, a, key, altered); err != ErrConflict {
		t.Fatal("key payload conflict ignored", err)
	}
	for range 2 {
		p, err := store.Search(ctx, a, Search{Code: "0012-34"})
		if err != nil || len(p.Items) != 1 || p.Items[0].UsedCount != 0 {
			t.Fatal("lookup consumed coupon", err)
		}
	}
	if p, err := store.Search(ctx, b, Search{}); err != nil || len(p.Items) != 0 {
		t.Fatal("foreign list exposed")
	}
	wg = sync.WaitGroup{}
	revocations := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Revoke(ctx, a, c.ID, "Ошибка выдачи")
			revocations <- err
		}()
	}
	wg.Wait()
	close(revocations)
	for err := range revocations {
		if err != nil {
			t.Fatal(err)
		}
	}
	saved, events, err := store.Get(ctx, a, c.ID)
	if err != nil || saved.Status != "revoked" || saved.UsedCount != 0 || len(events) != 2 {
		t.Fatal("revocation lost audit or counted use", err, events)
	}
	replay, repeated, err := store.Create(ctx, a, key, in)
	if err != nil || !repeated || replay.Status != "revoked" || replay.ID != c.ID {
		t.Fatal("retry revived coupon", err)
	}
	if _, _, err := store.Create(ctx, a, takenKey, in); err != ErrCodeTaken {
		t.Fatal("revoked number reused", err)
	}
	other := in
	other.ReferrerClientID = nil
	if _, _, err := store.Create(ctx, b, takenKey, other); err != nil {
		t.Fatal("owner namespace collided", err)
	}
	expired := baseInput()
	expired.DiscountType = "fixed"
	expired.Value = "200,25"
	expired.TotalLimit = nil
	expired.PerClientLimit = nil
	date := "2020-01-01"
	expired.ExpiresOn = &date
	expiredKey, _ := newUUID()
	old, _, err := store.Create(ctx, a, expiredKey, expired)
	if err != nil || old.Status != "expired" || old.Value != "200.25" || old.TotalLimit != nil || old.PerClientLimit != nil {
		t.Fatal("expired/unlimited/fixed coupon", err, old)
	}
	defaultsKey, _ := newUUID()
	w := call("POST", "/api/coupons", map[string]any{"discount_type": "fixed", "value": "10.00"}, token, csrf, defaultsKey)
	var defaults Coupon
	_ = json.Unmarshal(w.Body.Bytes(), &defaults)
	if w.Code != 201 || defaults.PerClientLimit == nil || *defaults.PerClientLimit != 1 || defaults.TotalLimit != nil {
		t.Fatal("default personal budget", w.Code, w.Body.String())
	}
	// The shared lookup budget applies to details and searches for this owner only.
	if _, err := pool.Exec(ctx, `INSERT INTO coupon_lookup_limits(specialist_id,attempts,resets_at) VALUES($1,120,now()+interval '1 minute') ON CONFLICT(specialist_id) DO UPDATE SET attempts=120,resets_at=now()+interval '1 minute'`, a); err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/api/coupons/search", Search{}, token, csrf, ""); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("search budget bypassed", w.Code)
	}
	if w := call("GET", "/api/coupons/"+c.ID, nil, token, csrf, ""); w.Code != 429 {
		t.Fatal("detail budget bypassed", w.Code)
	}
	if ok, err := store.AllowLookup(ctx, b); err != nil || !ok {
		t.Fatal("other specialist budget affected", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE coupon_lookup_limits SET resets_at=now()-interval '1 second' WHERE specialist_id=$1`, a); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.AllowLookup(ctx, a); err != nil || !ok {
		t.Fatal("lookup budget did not recover", err)
	}
}
func TestPostgresGeneratedCodesAndBudgetConcurrency(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	a, _ := owner(t, pool, "codes")
	store := Store{Pool: pool}
	codes := map[string]bool{}
	for range 15 {
		key, _ := newUUID()
		c, _, err := store.Create(ctx, a, key, baseInput())
		if err != nil || len(c.Code) != 6 || codes[c.Code] {
			t.Fatal("invalid auto-number", err, c.Code)
		}
		codes[c.Code] = true
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coupon_lookup_limits(specialist_id,attempts,resets_at) VALUES($1,115,now()+interval '1 minute')`, a); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan bool, 20)
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); ok, err := store.AllowLookup(ctx, a); results <- ok; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for ok := range results {
		if ok {
			count++
		}
	}
	if count != 5 {
		t.Fatal("concurrent lookup budget exceeded", count)
	}
}
