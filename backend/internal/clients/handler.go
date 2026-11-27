package clients

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/azamatbayramov/referflow/backend/internal/auth"
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
	case r.URL.Path == "/api/clients":
		if !method(w, r, "POST") {
			return
		}
		var in struct {
			Name  string `json:"name"`
			Phone string `json:"phone"`
		}
		if !decode(w, r, &in) {
			return
		}
		var c Client
		var replay bool
		c, replay, err = h.Store.Create(r.Context(), user.ID, r.Header.Get("Idempotency-Key"), Input{Name: in.Name, Phone: in.Phone})
		result = c
		if !replay {
			code = 201
		}
	case r.URL.Path == "/api/clients/search":
		if !method(w, r, "POST") {
			return
		}
		var in Search
		if !decode(w, r, &in) {
			return
		}
		result, err = h.Store.Search(r.Context(), user.ID, in)
	case strings.HasPrefix(r.URL.Path, "/api/clients/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/clients/")
		if r.Method == "GET" {
			var c Client
			var events []Event
			c, events, err = h.Store.Get(r.Context(), user.ID, id)
			result = struct {
				Client Client  `json:"client"`
				Events []Event `json:"events"`
			}{c, events}
		} else if r.Method == "PUT" {
			var in struct {
				Name     *string `json:"name"`
				Phone    *string `json:"phone"`
				Archived *bool   `json:"archived"`
				Version  *int64  `json:"version"`
			}
			if !decode(w, r, &in) {
				return
			}
			if in.Name == nil || in.Phone == nil || in.Archived == nil || in.Version == nil {
				reply(w, 400, map[string]string{"status": "invalid_request"})
				return
			}
			result, err = h.Store.Update(r.Context(), user.ID, id, Input{Name: *in.Name, Phone: *in.Phone, Archived: *in.Archived, Version: *in.Version})
		} else {
			w.Header().Set("Allow", "GET, PUT")
			reply(w, 405, map[string]string{"status": "method_not_allowed"})
			return
		}
	default:
		reply(w, 404, map[string]string{"status": "not_found"})
		return
	}
	if err != nil {
		status := "unavailable"
		code = 503
		switch {
		case errors.Is(err, ErrInvalid):
			status = "invalid_request"
			code = 400
		case errors.Is(err, ErrNotFound):
			status = "not_found"
			code = 404
		case errors.Is(err, ErrConflict):
			status = "conflict"
			code = 409
		}
		reply(w, code, map[string]string{"status": status})
		return
	}
	reply(w, code, result)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		reply(w, 415, map[string]string{"status": "unsupported_media_type"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
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
