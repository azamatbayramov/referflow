//go:build integration

package coupons

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostgresPublicCouponPrivacyAndLiveStatus(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	s := Store{Pool: pool}
	ownerID, _ := owner(t, pool, "public-owner")
	clientID := redemptionClient(t, pool, ownerID, "Частное имя рекомендателя")
	in := baseInput()
	in.ReferrerClientID = &clientID
	in.FirstVisitOnly = true
	one := 1
	in.TotalLimit = &one
	coupon := redemptionCoupon(t, s, ownerID, in)
	h := PublicHandler{Store: s}
	read := func(id string, code int, status string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/public/coupons/"+id+"?specialist_id=forged", nil)
		r.Header.Set("Cookie", "referflow_session=invalid-session")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("public read: %d %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("cache or session mutation")
		}
		if code != 200 {
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		keys := []string{}
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		want := []string{"code", "discount_type", "expires_at", "expires_on", "first_visit_only", "per_client_limit", "restrictions", "status", "total_limit", "value"}
		if !reflect.DeepEqual(keys, want) {
			t.Fatalf("public allowlist changed: %v", keys)
		}
		for _, private := range []string{clientID, ownerID, coupon.ID, "Частное имя", "Причина отзыва", "Причина отмены"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("private field leaked")
			}
		}
		var got PublicCoupon
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != status || got.Code != coupon.Code || got.Value != in.Value || got.Restrictions != in.Restrictions || !got.FirstVisitOnly || *got.TotalLimit != 1 || *got.PerClientLimit != 1 {
			t.Fatalf("wrong public terms: %+v", got)
		}
	}
	read(coupon.ID, 200, "available")
	read(strings.ToUpper(coupon.ID), 200, "available")
	missing, _ := newUUID()
	read(missing, 404, "")
	read(coupon.Code, 404, "")
	// Existing and malformed links cannot be used as a redemption/write API.
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/public/coupons/"+coupon.ID, strings.NewReader(`{"client_id":"forged"}`)))
		if w.Code != 405 {
			t.Fatal("public write allowed", w.Code)
		}
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/api/public/coupons/"+coupon.ID, nil))
			if w.Code != 200 {
				t.Error("parallel public read", w.Code)
			}
		}()
	}
	wg.Wait()
	var count, events, uses, budgets int
	if err := pool.QueryRow(ctx, `SELECT used_count,(SELECT count(*) FROM coupon_events WHERE coupon_id=$1),(SELECT count(*) FROM coupon_redemptions WHERE coupon_id=$1),(SELECT count(*) FROM coupon_lookup_limits) FROM coupons WHERE id=$1`, coupon.ID).Scan(&count, &events, &uses, &budgets); err != nil {
		t.Fatal(err)
	}
	if count != 0 || events != 1 || uses != 0 || budgets != 0 {
		t.Fatal("public views changed domain state", count, events, uses, budgets)
	}
	key, _ := newUUID()
	use, _, err := s.Redeem(ctx, ownerID, key, RedemptionInput{CouponID: coupon.ID, ClientID: clientID, SingleCouponConfirmed: true, FirstVisitConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	read(coupon.ID, 200, "exhausted")
	if _, err := s.CancelRedemption(ctx, ownerID, use.ID, "Причина отмены"); err != nil {
		t.Fatal(err)
	}
	read(coupon.ID, 200, "available")
	// Time and status precedence, with no stale public response cache.
	if _, err := pool.Exec(ctx, `UPDATE coupons SET expires_on='2000-01-01',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, coupon.ID); err != nil {
		t.Fatal(err)
	}
	read(coupon.ID, 200, "expired")
	if _, err := s.Revoke(ctx, ownerID, coupon.ID, "Причина отзыва"); err != nil {
		t.Fatal(err)
	}
	read(coupon.ID, 200, "revoked")
	server := httptest.NewServer(h)
	defer server.Close()
	response, err := server.Client().Head(server.URL + "/api/public/coupons/" + coupon.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("HEAD failed", response.StatusCode)
	}
}

func TestPostgresPublicReadStorageFailureDoesNotDiscloseDetails(t *testing.T) {
	pool := database(t)
	pool.Close()
	h := PublicHandler{Store: Store{Pool: pool}}
	id, _ := newUUID()
	w := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/public/coupons/"+id, nil))
	if w.Code != 503 || strings.TrimSpace(w.Body.String()) != `{"status":"unavailable"}` || time.Since(start) > 3*time.Second {
		t.Fatal("unbounded or disclosed storage failure", w.Code, w.Body.String())
	}
}
