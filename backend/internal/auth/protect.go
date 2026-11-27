package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"
)

type sessionKey struct{}

// Protect resolves the specialist only from a valid server-side session.
// Every non-read request also requires the same Origin and CSRF protection as logout.
func (h *Handler) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		session, _, ok := h.requireSession(w, r)
		if !ok {
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !h.sameOrigin(w, r) {
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1 {
				status(w, http.StatusForbidden, "csrf_failed")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, sessionKey{}, session)))
	})
}

func CurrentUser(ctx context.Context) (User, bool) {
	session, ok := ctx.Value(sessionKey{}).(Session)
	return session.User, ok
}
