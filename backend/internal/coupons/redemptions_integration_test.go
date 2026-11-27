//go:build integration

package coupons

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"github.com/azamatbayramov/referflow/backend/internal/clients"
	"github.com/jackc/pgx/v5/pgxpool"
)

func redemptionClient(t *testing.T, pool *pgxpool.Pool, owner, name string) string {
	t.Helper()
	key, _ := newUUID()
	c, _, err := (clients.Store{Pool: pool}).Create(context.Background(), owner, key, clients.Input{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}
func redemptionCoupon(t *testing.T, s Store, owner string, in Input) Coupon {
	t.Helper()
	key, _ := newUUID()
	c, _, err := s.Create(context.Background(), owner, key, in)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestPostgresRedemptionLifecycleHTTPAndOwnership(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, token := owner(t, pool, "redeem-a")
	b, otherToken := owner(t, pool, "redeem-b")
	client := redemptionClient(t, pool, a, "Клиент Тестовый")
	foreignClient := redemptionClient(t, pool, b, "Чужой Тестовый")
	terms := baseInput()
	terms.FirstVisitOnly = true
	coupon := redemptionCoupon(t, s, a, terms)
	foreignCoupon := redemptionCoupon(t, s, b, terms)
	authHandler, err := auth.NewHandler(auth.Store{Pool: pool}, auth.Options{Origins: []string{"https://referflow.example"}, SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	csrfFor := func(token string) string {
		r := httptest.NewRequest("GET", "/api/auth/session", nil)
		r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
		w := httptest.NewRecorder()
		authHandler.ServeHTTP(w, r)
		var session auth.Session
		_ = json.Unmarshal(w.Body.Bytes(), &session)
		return session.CSRFToken
	}
	csrf, otherCSRF := csrfFor(token), csrfFor(otherToken)
	h := authHandler.Protect(RedemptionHandler{Store: s})
	call := func(method, path string, body any, token, csrf, key string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Origin", "https://referflow.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	input := RedemptionInput{CouponID: coupon.ID, ClientID: client, SingleCouponConfirmed: true, FirstVisitConfirmed: true}
	key, _ := newUUID()
	if w := call("POST", "/api/redemptions", input, "", "", key); w.Code != 401 {
		t.Fatal("anonymous redemption", w.Code)
	}
	if w := call("POST", "/api/redemptions", input, token, "", key); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	if w := call("POST", "/api/redemptions", map[string]any{"coupon_id": coupon.ID, "client_id": client, "specialist_id": b}, token, csrf, key); w.Code != 400 {
		t.Fatal("caller controlled owner", w.Code)
	}
	for _, field := range []string{"confirmed_at", "visit_id"} {
		body := map[string]any{"coupon_id": coupon.ID, "client_id": client, "single_coupon_confirmed": true, "first_visit_confirmed": true, field: "forged"}
		if w := call("POST", "/api/redemptions", body, token, csrf, key); w.Code != 400 {
			t.Fatal("caller controlled time/visit", w.Code)
		}
	}
	if w := call("POST", "/api/redemptions", input, token, csrf, ""); w.Code != 400 {
		t.Fatal("missing idempotency key", w.Code)
	}
	for _, bad := range []RedemptionInput{{CouponID: coupon.ID, ClientID: client}, {CouponID: coupon.ID, ClientID: client, SingleCouponConfirmed: true}} {
		if w := call("POST", "/api/redemptions", bad, token, csrf, key); w.Code != 422 {
			t.Fatal("missing confirmation accepted", w.Code)
		}
	}
	foreign := input
	foreign.ClientID = foreignClient
	if w := call("POST", "/api/redemptions", foreign, token, csrf, key); w.Code != 404 {
		t.Fatal("foreign client", w.Code)
	}
	foreign = input
	foreign.CouponID = foreignCoupon.ID
	if w := call("POST", "/api/redemptions", foreign, token, csrf, key); w.Code != 404 {
		t.Fatal("foreign coupon", w.Code)
	}
	if _, err := pool.Exec(ctx, `UPDATE clients SET archived=true WHERE id=$1`, client); err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/api/redemptions", input, token, csrf, key); w.Code != 422 {
		t.Fatal("archived client accepted", w.Code)
	}
	_, _ = pool.Exec(ctx, `UPDATE clients SET archived=false WHERE id=$1`, client)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call("POST", "/api/redemptions", input, token, csrf, key) }()
	}
	wg.Wait()
	close(results)
	var saved Redemption
	created := 0
	for w := range results {
		if w.Code == 201 {
			created++
		} else if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var got Redemption
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if saved.ID != "" && saved.ID != got.ID {
			t.Fatal("duplicate redemption")
		}
		saved = got
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable history")
		}
	}
	if created != 1 || saved.Value != "12.50" || saved.AuthorID != a || !saved.SingleCouponConfirmed || !saved.FirstVisitConfirmed || !saved.FirstVisitRequired {
		t.Fatal("audit or nominal missing", created, saved)
	}
	altered := input
	altered.FirstVisitConfirmed = false
	if w := call("POST", "/api/redemptions", altered, token, csrf, key); w.Code != 409 {
		t.Fatal("key payload conflict", w.Code)
	}
	for _, path := range []string{"/api/redemptions/" + saved.ID, "/api/redemptions/00000000-0000-4000-8000-000000000000"} {
		if w := call("GET", path, nil, otherToken, otherCSRF, ""); w.Code != 404 {
			t.Fatal("foreign history exposed", w.Code)
		}
	}
	if w := call("POST", "/api/redemptions/"+saved.ID+"/cancel", map[string]string{"reason": "Foreign"}, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign cancellation", w.Code)
	}
	if w := call("POST", "/api/redemptions/search", RedemptionSearch{ClientID: client}, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign filter exposed", w.Code)
	}
	newKey, _ := newUUID()
	if _, _, err := s.Redeem(ctx, a, newKey, input); err != ErrPersonalLimit {
		t.Fatal("personal limit ignored", err)
	}
	clientStore := clients.Store{Pool: pool}
	card, _, err := clientStore.Get(ctx, a, client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = clientStore.Update(ctx, a, client, clients.Input{Name: "Клиент Исправленный", Phone: "+7 900 000 00 01", Version: card.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Redeem(ctx, a, newKey, input); err != ErrPersonalLimit {
		t.Fatal("contact change reset personal limit", err)
	}
	canceled := make(chan Redemption, 12)
	failures := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := s.CancelRedemption(ctx, a, saved.ID, "Ошибка выбора купона")
			canceled <- u
			failures <- err
		}()
	}
	wg.Wait()
	close(canceled)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var cancelTime time.Time
	for u := range canceled {
		if u.CanceledAt == nil || u.CanceledBy == nil || *u.CanceledBy != a {
			t.Fatal("cancel audit missing")
		}
		if !cancelTime.IsZero() && !cancelTime.Equal(*u.CanceledAt) {
			t.Fatal("repeat changed cancel time")
		}
		cancelTime = *u.CanceledAt
	}
	c, _, err := s.Get(ctx, a, coupon.ID)
	if err != nil || c.UsedCount != 0 {
		t.Fatal("cancel released budget more than once", c, err)
	}
	replay, repeated, err := s.Redeem(ctx, a, key, input)
	if err != nil || !repeated || replay.CanceledAt == nil || replay.ID != saved.ID {
		t.Fatal("replay resurrected canceled usage", err)
	}
	replacement, _, err := s.Redeem(ctx, a, newKey, input)
	if err != nil || replacement.ID == saved.ID {
		t.Fatal("explicit replacement failed", err)
	}
	if _, err := s.Revoke(ctx, a, coupon.ID, "Отзыв тест"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelRedemption(ctx, a, replacement.ID, "Отмена после отзыва"); err != nil {
		t.Fatal(err)
	}
	c, _, err = s.Get(ctx, a, coupon.ID)
	if err != nil || c.Status != "revoked" || c.UsedCount != 0 {
		t.Fatal("cancel revived revoked coupon", err, c)
	}
	if _, _, err := s.Redeem(ctx, a, key, input); err != nil {
		t.Fatal("revocation blocked idempotent replay", err)
	}
	finalKey, _ := newUUID()
	if _, _, err := s.Redeem(ctx, a, finalKey, input); err != ErrRevoked {
		t.Fatal("redeemed revoked coupon", err)
	}
	page, err := s.SearchRedemptions(ctx, a, RedemptionSearch{ClientID: client})
	if err != nil || len(page.Items) != 2 || page.Items[0].ClientName != "Клиент Исправленный" {
		t.Fatal("history lost after corrections/cancel", err)
	}
}
func TestPostgresConcurrentRedemptionLimits(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, _ := owner(t, pool, "limits")
	client := redemptionClient(t, pool, a, "Получатель")
	other := redemptionClient(t, pool, a, "Другой получатель")
	one, two, four := 1, 2, 4
	cases := []struct {
		name            string
		total, personal *int
		want            int
		alternate       bool
	}{{"global", &one, nil, 1, true}, {"personal", nil, &one, 1, false}, {"both", &four, &two, 4, true}, {"unlimited", nil, nil, 20, false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			in.TotalLimit = tc.total
			in.PerClientLimit = tc.personal
			c := redemptionCoupon(t, s, a, in)
			var wg sync.WaitGroup
			results := make(chan error, 20)
			for i := range 20 {
				wg.Add(1)
				id := client
				if tc.alternate && i%2 == 1 {
					id = other
				}
				go func(clientID string) {
					defer wg.Done()
					key, _ := newUUID()
					_, _, err := s.Redeem(ctx, a, key, RedemptionInput{CouponID: c.ID, ClientID: clientID, SingleCouponConfirmed: true})
					results <- err
				}(id)
			}
			wg.Wait()
			close(results)
			accepted := 0
			for err := range results {
				if err == nil {
					accepted++
				} else if err != ErrTotalLimit && err != ErrPersonalLimit {
					t.Fatal(err)
				}
			}
			if accepted != tc.want {
				t.Fatal("concurrent budget violated", accepted, tc.want)
			}
			var active int
			_ = pool.QueryRow(ctx, `SELECT count(*) FROM coupon_redemptions WHERE coupon_id=$1 AND canceled_at IS NULL`, c.ID).Scan(&active)
			saved, _, err := s.Get(ctx, a, c.ID)
			if err != nil || saved.UsedCount != accepted || active != accepted {
				t.Fatal("counter and active operations differ", err, saved.UsedCount, active)
			}
		})
	}
}
func TestPostgresRedemptionVersusRevocation(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, _ := owner(t, pool, "revoke-race")
	client := redemptionClient(t, pool, a, "Тестовая гонка")
	for range 10 {
		c := redemptionCoupon(t, s, a, baseInput())
		key, _ := newUUID()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var u Redemption
		var redeemErr, revokeErr error
		go func() {
			defer wg.Done()
			<-start
			u, _, redeemErr = s.Redeem(ctx, a, key, RedemptionInput{CouponID: c.ID, ClientID: client, SingleCouponConfirmed: true})
		}()
		go func() { defer wg.Done(); <-start; _, revokeErr = s.Revoke(ctx, a, c.ID, "Гонка отзыва") }()
		close(start)
		wg.Wait()
		if revokeErr != nil {
			t.Fatal(revokeErr)
		}
		if redeemErr != nil && redeemErr != ErrRevoked {
			t.Fatal(redeemErr)
		}
		saved, _, err := s.Get(ctx, a, c.ID)
		if err != nil || saved.Status != "revoked" {
			t.Fatal(err)
		}
		want := 0
		if redeemErr == nil {
			want = 1
			if u.ConfirmedAt.After(*saved.RevokedAt) {
				t.Fatal("usage confirmed after revocation")
			}
		}
		if saved.UsedCount != want {
			t.Fatal("race counter mismatch")
		}
		afterKey, _ := newUUID()
		if _, _, err := s.Redeem(ctx, a, afterKey, RedemptionInput{CouponID: c.ID, ClientID: client, SingleCouponConfirmed: true}); err != ErrRevoked {
			t.Fatal("usage after committed revocation", err)
		}
	}
}
func TestPostgresExpiryAfterLockWait(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, _ := owner(t, pool, "expiry-wait")
	client := redemptionClient(t, pool, a, "Ожидание")
	c := redemptionCoupon(t, s, a, baseInput())
	var expires time.Time
	if err := pool.QueryRow(ctx, `UPDATE coupons SET expires_on=current_date,expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE id=$1 RETURNING expires_at`, c.ID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(blocker)
	if _, err := blocker.Exec(ctx, `SELECT id FROM coupons WHERE id=$1 FOR UPDATE`, c.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		key, _ := newUUID()
		_, _, err := s.Redeem(ctx, a, key, RedemptionInput{CouponID: c.ID, ClientID: client, SingleCouponConfirmed: true})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var start *time.Time
		err := pool.QueryRow(ctx, `SELECT min(xact_start) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '/* coupon redemption lock */%'`).Scan(&start)
		if err != nil {
			t.Fatal(err)
		}
		if start != nil {
			if !start.Before(expires) {
				t.Fatal("test request did not start before expiry")
			}
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("redemption did not wait on coupon lock")
	}
	if delay := time.Until(expires.Add(30 * time.Millisecond)); delay > 0 {
		time.Sleep(delay)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrExpired) {
		t.Fatal("expired coupon accepted after waiting", err)
	}
	saved, _, err := s.Get(ctx, a, c.ID)
	if err != nil || saved.UsedCount != 0 {
		t.Fatal("expired usage changed count", err)
	}
}

func TestPostgresRedemptionSharedKeyAndHistoryPages(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, _ := owner(t, pool, "shared-key")
	client := redemptionClient(t, pool, a, "История")
	terms := baseInput()
	terms.TotalLimit = nil
	terms.PerClientLimit = nil
	first := redemptionCoupon(t, s, a, terms)
	second := redemptionCoupon(t, s, a, terms)
	key, _ := newUUID()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(couponID string) {
			defer wg.Done()
			_, _, err := s.Redeem(ctx, a, key, RedemptionInput{CouponID: couponID, ClientID: client, SingleCouponConfirmed: true})
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if err == ErrConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatal("same key crossed coupons", accepted, conflicts)
	}
	for range 51 {
		key, _ := newUUID()
		if _, _, err := s.Redeem(ctx, a, key, RedemptionInput{CouponID: first.ID, ClientID: client, SingleCouponConfirmed: true}); err != nil {
			t.Fatal(err)
		}
	}
	one, err := s.SearchRedemptions(ctx, a, RedemptionSearch{ClientID: client})
	if err != nil || len(one.Items) != 50 || !one.HasMore {
		t.Fatal("first history page", err)
	}
	two, err := s.SearchRedemptions(ctx, a, RedemptionSearch{ClientID: client, Offset: 50})
	if err != nil || len(two.Items) != 2 || two.HasMore {
		t.Fatal("second history page", err)
	}
	seen := map[string]bool{}
	for _, u := range one.Items {
		seen[u.ID] = true
	}
	for _, u := range two.Items {
		if seen[u.ID] {
			t.Fatal("history repeated row")
		}
	}
}
func TestPostgresCancelAfterExpiryDoesNotRestoreCoupon(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, _ := owner(t, pool, "cancel-expired")
	client := redemptionClient(t, pool, a, "Истекший")
	c := redemptionCoupon(t, s, a, baseInput())
	key, _ := newUUID()
	in := RedemptionInput{CouponID: c.ID, ClientID: client, SingleCouponConfirmed: true}
	u, _, err := s.Redeem(ctx, a, key, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE coupons SET expires_on=current_date,expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelRedemption(ctx, a, u.ID, "Отмена после срока"); err != nil {
		t.Fatal(err)
	}
	expired, _, err := s.Get(ctx, a, c.ID)
	if err != nil || expired.Status != "expired" || expired.UsedCount != 0 {
		t.Fatal("cancel restored expired coupon", err)
	}
	newKey, _ := newUUID()
	if _, _, err := s.Redeem(ctx, a, newKey, in); err != ErrExpired {
		t.Fatal("new usage of expired coupon", err)
	}
	if replay, _, err := s.Redeem(ctx, a, key, in); err != nil || replay.CanceledAt == nil {
		t.Fatal("expired replay resurrected usage", err)
	}
}
