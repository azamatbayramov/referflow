package coupons

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicRejectsMalformedAndNonReadRequestsWithoutDatabase(t *testing.T) {
	h := PublicHandler{}
	for _, tc := range []struct {
		method, path string
		code         int
	}{
		{"GET", "/api/public/coupons/123456", 404},
		{"GET", "/api/public/coupons/", 404},
		{"GET", "/api/public/coupons/not-a-uuid", 404},
		{"GET", "/api/public/coupons/00000000-0000-1000-8000-000000000000", 404},
		{"GET", "/api/public/coupons/00000000-0000-4000-0000-000000000000", 404},
		{"GET", "/api/public/coupons/00000000-0000-4000-8000-000000000000/revoke", 404},
		{"POST", "/api/public/coupons/00000000-0000-4000-8000-000000000000", 405},
		{"DELETE", "/api/public/coupons/00000000-0000-4000-8000-000000000000", 405},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") {
			t.Fatal("missing privacy/cache headers")
		}
		if tc.code == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal("read-only endpoint")
		}
		if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("unexpected session or CORS")
		}
	}
}
