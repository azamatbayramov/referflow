package coupons

import (
	"encoding/json"
	"errors"
	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"io"
	"mime"
	"net/http"
	"strings"
)

type Handler struct{ Store Store }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		reply(w, 401, map[string]string{"status": "unauthenticated"})
		return
	}
	var result any
	var err error
	code := 200
	switch {
	case r.URL.Path == "/api/coupons":
		if !method(w, r, "POST") {
			return
		}
		one := 1
		in := Input{PerClientLimit: &one}
		if !decode(w, r, &in) {
			return
		}
		var c Coupon
		var replay bool
		c, replay, err = h.Store.Create(r.Context(), user.ID, r.Header.Get("Idempotency-Key"), in)
		result = c
		if !replay {
			code = 201
		}
	case r.URL.Path == "/api/coupons/search":
		if !method(w, r, "POST") {
			return
		}
		var in Search
		if !decode(w, r, &in) {
			return
		}
		if !h.lookup(w, r, user.ID) {
			return
		}
		result, err = h.Store.Search(r.Context(), user.ID, in)
	case strings.HasPrefix(r.URL.Path, "/api/coupons/") && strings.HasSuffix(r.URL.Path, "/revoke"):
		if !method(w, r, "POST") {
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/coupons/"), "/revoke")
		var in struct {
			Reason string `json:"reason"`
		}
		if !decode(w, r, &in) {
			return
		}
		result, err = h.Store.Revoke(r.Context(), user.ID, id, in.Reason)
	case strings.HasPrefix(r.URL.Path, "/api/coupons/"):
		if !method(w, r, "GET") {
			return
		}
		if !h.lookup(w, r, user.ID) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/coupons/")
		var c Coupon
		var events []Event
		c, events, err = h.Store.Get(r.Context(), user.ID, id)
		result = struct {
			Coupon Coupon  `json:"coupon"`
			Events []Event `json:"events"`
		}{c, events}
	default:
		reply(w, 404, map[string]string{"status": "not_found"})
		return
	}
	if err != nil {
		code = 503
		status := "unavailable"
		switch {
		case errors.Is(err, ErrInvalid):
			code = 400
			status = "invalid_request"
		case errors.Is(err, ErrNotFound):
			code = 404
			status = "not_found"
		case errors.Is(err, ErrConflict):
			code = 409
			status = "conflict"
		}
		reply(w, code, map[string]string{"status": status})
		return
	}
	reply(w, code, result)
}
func (h Handler) lookup(w http.ResponseWriter, r *http.Request, owner string) bool {
	allowed, err := h.Store.AllowLookup(r.Context(), owner)
	if err != nil {
		reply(w, 503, map[string]string{"status": "unavailable"})
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		reply(w, 429, map[string]string{"status": "too_many_requests"})
		return false
	}
	return true
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		reply(w, 415, map[string]string{"status": "unsupported_media_type"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err = dec.Decode(v)
	if err == nil {
		if err = dec.Decode(&struct{}{}); err == io.EOF {
			return true
		}
	}
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		reply(w, 413, map[string]string{"status": "request_too_large"})
	} else {
		reply(w, 400, map[string]string{"status": "invalid_request"})
	}
	return false
}
func method(w http.ResponseWriter, r *http.Request, m string) bool {
	if r.Method == m {
		return true
	}
	w.Header().Set("Allow", m)
	reply(w, 405, map[string]string{"status": "method_not_allowed"})
	return false
}
func reply(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Vary", "Cookie, Origin")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
