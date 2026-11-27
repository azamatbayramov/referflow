package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Pinger interface {
	Ping(context.Context) error
}

// NewHandler deliberately exposes only infrastructure endpoints.
func NewHandler(db Pinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !allowRead(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("/api/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !allowRead(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			// Do not disclose connection details to an unauthenticated health caller.
			writeJSON(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeJSON(w, http.StatusOK, "ready")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, "not_found")
	})
	return mux
}

func allowRead(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	writeJSON(w, http.StatusMethodNotAllowed, "method_not_allowed")
	return false
}

func writeJSON(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{Status: status})
}
