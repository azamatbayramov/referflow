//go:build integration

package coupons

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
)

func TestPostgresStatisticsDefinitionsFiltersAndIdentity(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, _ := owner(t, pool, "statistics-a")
	b, _ := owner(t, pool, "statistics-b")
	check := func(owner string, issued, used, active, unique, first, canceled int64) {
		t.Helper()
		got, err := s.Statistics(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		if got.AsOf.IsZero() || time.Since(got.AsOf) > 5*time.Second {
			t.Fatal("missing snapshot time")
		}
		want := Statistics{AsOf: got.AsOf, IssuedCoupons: issued, UsedCoupons: used, ActiveRedemptions: active, UniqueClients: unique, FirstVisitClients: first, CanceledRedemptions: canceled}
		if got != want {
			t.Fatalf("statistics: %+v; want %+v", got, want)
		}
	}
	check(a, 0, 0, 0, 0, 0, 0)
	alpha := redemptionClient(t, pool, a, "Первый тестовый клиент")
	beta := redemptionClient(t, pool, a, "Второй тестовый клиент")
	other := redemptionClient(t, pool, b, "Чужой тестовый клиент")
	multi := redemptionCoupon(t, s, a, Input{DiscountType: "fixed", Value: "350.25"})
	one := 1
	firstCoupon := redemptionCoupon(t, s, a, Input{DiscountType: "percent", Value: "15", TotalLimit: &one, PerClientLimit: &one, FirstVisitOnly: true})
	idle := redemptionCoupon(t, s, a, baseInput())
	past := "2000-01-01"
	expired := redemptionCoupon(t, s, a, Input{DiscountType: "percent", Value: "10", ExpiresOn: &past})
	foreign := redemptionCoupon(t, s, b, baseInput())
	use := func(owner, coupon, client string, first bool) Redemption {
		t.Helper()
		key, _ := newUUID()
		entry, _, err := s.Redeem(ctx, owner, key, RedemptionInput{CouponID: coupon, ClientID: client, SingleCouponConfirmed: true, FirstVisitConfirmed: first})
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	alphaOne := use(a, multi.ID, alpha, true)
	alphaTwo := use(a, multi.ID, alpha, true)
	_ = use(a, multi.ID, beta, false)
	betaFirst := use(a, firstCoupon.ID, beta, true)
	_ = use(b, foreign.ID, other, true)
	check(a, 4, 2, 4, 2, 2, 0)
	check(b, 1, 1, 1, 1, 1, 0)
	// Overall coupon status never removes successful historical applications.
	if _, err := s.Revoke(ctx, a, multi.ID, "Тестовый отзыв"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE coupons SET expires_on='2000-01-01',expires_at=now()-interval '1 day' WHERE id=$1`, firstCoupon.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke(ctx, a, idle.ID, "Не использован"); err != nil {
		t.Fatal(err)
	}
	check(a, 4, 2, 4, 2, 2, 0)
	cancel := func(id string) {
		t.Helper()
		if _, err := s.CancelRedemption(ctx, a, id, "Тестовая отмена"); err != nil {
			t.Fatal(err)
		}
	}
	cancel(alphaOne.ID)
	check(a, 4, 2, 3, 2, 2, 1)
	cancel(alphaOne.ID)
	check(a, 4, 2, 3, 2, 2, 1)
	cancel(alphaTwo.ID)
	check(a, 4, 2, 2, 1, 1, 2)
	cancel(betaFirst.ID)
	check(a, 4, 1, 1, 1, 0, 3)
	if _, err := pool.Exec(ctx, `UPDATE clients SET name='Исправленное имя',phone='+7 900 000-00-02',phone_digits='79000000002',archived=true WHERE id=$1`, beta); err != nil {
		t.Fatal(err)
	}
	check(a, 4, 1, 1, 1, 0, 3)
	for _, tc := range []struct {
		usage, state string
		ids          []string
	}{
		{"used", "all", []string{multi.ID}}, {"used", "revoked", []string{multi.ID}}, {"used", "expired", nil},
		{"unused", "all", []string{firstCoupon.ID, idle.ID, expired.ID}}, {"unused", "expired", []string{firstCoupon.ID, expired.ID}},
	} {
		page, err := s.Search(ctx, a, Search{Usage: tc.usage, State: tc.state})
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, c := range page.Items {
			got = append(got, c.ID)
		}
		sort.Strings(got)
		sort.Strings(tc.ids)
		if len(got) != len(tc.ids) || len(got) > 0 && !reflect.DeepEqual(got, tc.ids) {
			t.Fatalf("%s/%s: %v, want %v", tc.usage, tc.state, got, tc.ids)
		}
	}
	if _, err := s.Search(ctx, a, Search{Usage: "ever-used"}); err != ErrInvalid {
		t.Fatal("unknown usage accepted", err)
	}
	history, err := s.SearchRedemptions(ctx, a, RedemptionSearch{CouponID: multi.ID})
	if err != nil || len(history.Items) != 3 {
		t.Fatal("audit lost", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT used_count FROM coupons WHERE id=$1`, multi.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("statistics mutated counter", count, err)
	}
}

func TestPostgresStatisticsHTTPAccessAndBudget(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	a, token := owner(t, pool, "statistics-http")
	b, otherToken := owner(t, pool, "statistics-other")
	_ = redemptionCoupon(t, s, a, baseInput())
	authHandler, err := auth.NewHandler(auth.Store{Pool: pool}, auth.Options{Origins: []string{"https://referflow.example"}, SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	handler := authHandler.Protect(Handler{Store: s})
	call := func(method, path, token, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Origin", "https://referflow.example")
		r.Header.Set("X-CSRF-Token", csrf)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, token := range []string{"", "invalid"} {
		if w := call("GET", "/api/coupons/statistics", token, ""); w.Code != 401 {
			t.Fatal("anonymous statistics", w.Code)
		}
	}
	w := call("GET", "/api/coupons/statistics", token, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("statistics response", w.Code, w.Body.String())
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"active_redemptions", "as_of", "canceled_redemptions", "first_visit_clients", "issued_coupons", "unique_clients", "used_coupons"}) {
		t.Fatal("unexpected private fields", keys)
	}
	var got Statistics
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.IssuedCoupons != 1 || got.ActiveRedemptions != 0 {
		t.Fatal("wrong own statistics", got)
	}
	w = call("GET", "/api/coupons/statistics", otherToken, "")
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.IssuedCoupons != 0 {
		t.Fatal("foreign data leaked")
	}
	for _, query := range []string{"?specialist_id=" + a, "?from=2000-01-01", "?usage=all"} {
		if w := call("GET", "/api/coupons/statistics"+query, otherToken, ""); w.Code != 400 {
			t.Fatal("query accepted", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/auth/session", nil)
	r.AddCookie(&http.Cookie{Name: "referflow_session", Value: token})
	sessionW := httptest.NewRecorder()
	authHandler.ServeHTTP(sessionW, r)
	var session auth.Session
	_ = json.Unmarshal(sessionW.Body.Bytes(), &session)
	if w := call("POST", "/api/coupons/statistics", token, session.CSRFToken); w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("statistics mutation method", w.Code)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coupon_lookup_limits(specialist_id,attempts,resets_at) VALUES($1,119,now()+interval '1 minute') ON CONFLICT(specialist_id) DO UPDATE SET attempts=119,resets_at=excluded.resets_at`, b); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/coupons/statistics", otherToken, ""); w.Code != 200 {
		t.Fatal("premature budget", w.Code)
	}
	if w := call("GET", "/api/coupons/statistics", otherToken, ""); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("statistics budget bypass", w.Code)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE specialist_id=$1`, a); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/coupons/statistics", token, ""); w.Code != 401 {
		t.Fatal("expired session statistics", w.Code)
	}
}
