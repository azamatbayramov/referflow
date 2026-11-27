package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHealthContract(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, status string
		dbError                    error
		code                       int
		ping                       bool
	}{
		{"live without database", "GET", "/api/healthz", "ok", errors.New("offline"), 200, false},
		{"ready", "GET", "/api/readyz", "ready", nil, 200, true},
		{"not ready", "GET", "/api/readyz", "unavailable", errors.New("secret connection string"), 503, true},
		{"unknown API", "GET", "/api/missing", "not_found", nil, 404, false},
		{"no business route", "GET", "/api/v1/coupons", "not_found", nil, 404, false},
		{"wrong method", "POST", "/api/readyz", "method_not_allowed", nil, 405, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			db := pingFunc(func(ctx context.Context) error {
				called = true
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second {
					t.Error("readiness check must have a bounded timeout")
				}
				return tc.dbError
			})
			res := httptest.NewRecorder()
			NewHandler(db, nil, nil, nil).ServeHTTP(res, httptest.NewRequest(tc.method, tc.path, nil))
			if res.Code != tc.code || strings.TrimSpace(res.Body.String()) != `{"status":"`+tc.status+`"}` {
				t.Fatalf("got %d %s", res.Code, res.Body.String())
			}
			if called != tc.ping {
				t.Fatalf("database called = %v, want %v", called, tc.ping)
			}
			if res.Header().Get("Content-Type") != "application/json" || res.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("health responses must be JSON and must not be cached")
			}
			if tc.code == 405 && res.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("missing Allow header")
			}
		})
	}
}

func TestHeadHasNoBody(t *testing.T) {
	server := httptest.NewServer(NewHandler(pingFunc(func(context.Context) error { return nil }), nil, nil, nil))
	defer server.Close()
	res, err := server.Client().Head(server.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength < 0 {
		t.Fatalf("unexpected HEAD response: %v", res)
	}
}

func TestReadinessPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := pingFunc(func(ctx context.Context) error {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("request cancellation did not reach the database")
		}
		return ctx.Err()
	})
	req := httptest.NewRequest("GET", "/api/readyz", nil).WithContext(ctx)
	res := httptest.NewRecorder()
	NewHandler(db, nil, nil, nil).ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", res.Code)
	}
}
