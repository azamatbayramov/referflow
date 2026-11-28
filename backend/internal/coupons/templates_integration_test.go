//go:build integration

package coupons

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"github.com/azamatbayramov/referflow/backend/internal/clients"
)

func TestPostgresTemplateLifecycleIsolationAndSnapshot(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	a, token := owner(t, pool, "template-first")
	b, otherToken := owner(t, pool, "template-second")
	store := Store{Pool: pool}
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
	h := authHandler.Protect(Handler{Store: store})
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
	key, _ := newUUID()
	payload := map[string]any{"name": "  Друзьям  ", "discount_type": "percent", "value": "12,50", "total_limit": 1, "first_visit_only": true, "expires_on": "2099-02-28", "restrictions": "Полный\nтекст условий"}
	if w := call("POST", "/api/coupons/templates", payload, "", "", key); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	if w := call("POST", "/api/coupons/templates", payload, token, "", key); w.Code != 403 {
		t.Fatal("csrf", w.Code)
	}
	for _, field := range []string{"code", "referrer_client_id", "specialist_id", "id", "used_count"} {
		bad := map[string]any{"name": "Тест", "discount_type": "fixed", "value": "10", field: ""}
		if w := call("POST", "/api/coupons/templates", bad, token, csrf, key); w.Code != 400 {
			t.Fatal("unknown field", field, w.Code)
		}
	}
	if w := call("POST", "/api/coupons/templates", payload, token, csrf, ""); w.Code != 400 {
		t.Fatal("missing replay key", w.Code)
	}
	if w := call("PUT", "/api/coupons/templates/", payload, token, csrf, key); w.Code != 404 {
		t.Fatal("empty ID created template", w.Code)
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 10)
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call("POST", "/api/coupons/templates", payload, token, csrf, key) }()
	}
	wg.Wait()
	close(results)
	var template Template
	created := 0
	for w := range results {
		if w.Code == 201 {
			created++
		} else if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var got Template
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if template.ID != "" && template.ID != got.ID {
			t.Fatal("duplicate templates")
		}
		template = got
	}
	if created != 1 || template.Name != "Друзьям" || template.Value != "12.50" || template.Version != 1 || template.PerClientLimit == nil || *template.PerClientLimit != 1 {
		t.Fatal("defaults or normalization", created, template)
	}
	url := "/api/coupons/templates/" + template.ID
	if w := call("GET", url, nil, otherToken, otherCSRF, ""); w.Code != 404 {
		t.Fatal("foreign get", w.Code)
	}
	if w := call("PUT", url, template.TemplateInput, otherToken, otherCSRF, key); w.Code != 404 {
		t.Fatal("foreign update", w.Code)
	}
	if page, err := store.SearchTemplates(ctx, b, TemplateSearch{}); err != nil || len(page.Items) != 0 {
		t.Fatal("foreign search", page, err)
	}
	if page, err := store.SearchTemplates(ctx, a, TemplateSearch{Query: "ДРУЗЬЯМ"}); err != nil || len(page.Items) != 1 {
		t.Fatal("name search", page, err)
	}
	changed := template.TemplateInput
	changed.Value = "20"
	if _, _, err := store.SaveTemplate(ctx, a, "", key, changed); err != ErrInvalid {
		t.Fatal("creation with version accepted", err)
	}
	changed.Version = 0
	if _, _, err := store.SaveTemplate(ctx, a, "", key, changed); err != ErrConflict {
		t.Fatal("changed replay accepted", err)
	}
	// Issuance uses a copied set of terms. Each copy receives fresh identity,
	// limits and public UUID; no template/client metadata is persisted in it.
	in := Input{DiscountType: template.DiscountType, Value: template.Value, Restrictions: template.Restrictions, TotalLimit: template.TotalLimit, PerClientLimit: template.PerClientLimit, FirstVisitOnly: template.FirstVisitOnly, ExpiresOn: template.ExpiresOn}
	issueKey, _ := newUUID()
	coupon, _, err := store.Create(ctx, a, issueKey, in)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, _ := newUUID()
	second, _, err := store.Create(ctx, a, secondKey, in)
	if err != nil || coupon.ID == second.ID || coupon.Code == second.Code || coupon.UsedCount != 0 || second.UsedCount != 0 || coupon.ReferrerClientID != nil {
		t.Fatal("copies shared identity or counters", err)
	}
	update := template.TemplateInput
	update.DiscountType, update.Value, update.Archived = "fixed", "250,25", true
	updateKey, _ := newUUID()
	w := call("PUT", url, update, token, csrf, updateKey)
	if w.Code != 200 {
		t.Fatal("edit", w.Code, w.Body.String())
	}
	var edited Template
	_ = json.Unmarshal(w.Body.Bytes(), &edited)
	if edited.Version != 2 || !edited.Archived || edited.Value != "250.25" {
		t.Fatal("archive/edit", edited)
	}
	if w := call("PUT", url, update, token, csrf, updateKey); w.Code != 200 {
		t.Fatal("lost edit response replay", w.Code)
	}
	staleKey, _ := newUUID()
	if w := call("PUT", url, update, token, csrf, staleKey); w.Code != 409 {
		t.Fatal("stale overwrite", w.Code)
	}
	replayed := call("POST", "/api/coupons/templates", payload, token, csrf, key)
	var replay Template
	_ = json.Unmarshal(replayed.Body.Bytes(), &replay)
	if replayed.Code != 200 || !replay.Archived || replay.Version != 2 {
		t.Fatal("creation replay restored old data", replayed.Code, replay)
	}
	for _, c := range []Coupon{coupon, second} {
		unchanged, _, err := store.Get(ctx, a, c.ID)
		if err != nil || unchanged.Value != "12.50" || unchanged.Status != "available" || unchanged.Restrictions != in.Restrictions || *unchanged.ExpiresOn != "2099-02-28" {
			t.Fatal("template edit affected issued coupon", err, unchanged)
		}
	}

	// Each issued copy has its own total and personal budget, even for one client.
	clientKey, _ := newUUID()
	client, _, err := (clients.Store{Pool: pool}).Create(ctx, a, clientKey, clients.Input{Name: "Клиент Тестовый"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []Coupon{coupon, second} {
		redeemKey, _ := newUUID()
		if _, _, err := store.Redeem(ctx, a, redeemKey, RedemptionInput{CouponID: c.ID, ClientID: client.ID, SingleCouponConfirmed: true, FirstVisitConfirmed: true}); err != nil {
			t.Fatal("copy shared budget", err)
		}
		anotherKey, _ := newUUID()
		if _, _, err := store.Redeem(ctx, a, anotherKey, RedemptionInput{CouponID: c.ID, ClientID: client.ID, SingleCouponConfirmed: true, FirstVisitConfirmed: true}); err == nil {
			t.Fatal("issued copy lost its limit")
		}
	}
	if page, err := store.SearchTemplates(ctx, a, TemplateSearch{}); err != nil || len(page.Items) != 0 {
		t.Fatal("archived template offered", err)
	}
	if page, err := store.SearchTemplates(ctx, a, TemplateSearch{State: "archived"}); err != nil || len(page.Items) != 1 {
		t.Fatal("archive missing", err)
	}
	// Restore with unlimited caps, then reject another edit from that old version.
	restore := edited.TemplateInput
	restore.Archived, restore.TotalLimit, restore.PerClientLimit, restore.ExpiresOn = false, nil, nil, nil
	restoreKey, _ := newUUID()
	restored, _, err := store.SaveTemplate(ctx, a, template.ID, restoreKey, restore)
	if err != nil || restored.Archived || restored.Version != 3 || restored.PerClientLimit != nil || restored.ExpiresOn != nil {
		t.Fatal("restore/unlimited", restored, err)
	}
	retry, replayedUpdate, err := store.SaveTemplate(ctx, a, template.ID, updateKey, update)
	if err != nil || !replayedUpdate || retry.Version != 3 || retry.Archived {
		t.Fatal("old edit replay overwrote newer state", retry, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coupon_lookup_limits(specialist_id,attempts,resets_at) VALUES($1,120,now()+interval '1 minute') ON CONFLICT(specialist_id) DO UPDATE SET attempts=120,resets_at=now()+interval '1 minute'`, a); err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/api/coupons/templates/search", TemplateSearch{}, token, csrf, ""); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("template search budget", w.Code)
	}
	if w := call("GET", url, nil, token, csrf, ""); w.Code != 429 {
		t.Fatal("template details budget", w.Code)
	}
}

func TestPostgresTemplateConcurrentEditsAndPagination(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	a, _ := owner(t, pool, "template-edits")
	store := Store{Pool: pool}
	key, _ := newUUID()
	input := TemplateInput{Name: "Шаблон", DiscountType: "fixed", Value: "10"}
	template, _, err := store.SaveTemplate(ctx, a, "", key, input)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"Изменение А", "Изменение Б"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := template.TemplateInput
			in.Name = name
			key, _ := newUUID()
			_, _, err := store.SaveTemplate(ctx, a, template.ID, key, in)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	saved, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			saved++
		} else if err == ErrConflict {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if saved != 1 || conflicts != 1 {
		t.Fatal("lost update", saved, conflicts)
	}
	for range 50 {
		key, _ := newUUID()
		if _, _, err := store.SaveTemplate(ctx, a, "", key, input); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.SearchTemplates(ctx, a, TemplateSearch{})
	if err != nil || len(first.Items) != 50 || !first.HasMore {
		t.Fatal("first page", err)
	}
	last, err := store.SearchTemplates(ctx, a, TemplateSearch{Offset: 50})
	if err != nil || len(last.Items) != 1 || last.HasMore {
		t.Fatal("last page", err)
	}
	if _, err := store.SearchTemplates(ctx, a, TemplateSearch{State: "invalid"}); err != ErrInvalid {
		t.Fatal("invalid filter", err)
	}
	if _, err := store.SearchTemplates(ctx, a, TemplateSearch{Offset: -1}); err != ErrInvalid {
		t.Fatal("invalid offset", err)
	}
}
