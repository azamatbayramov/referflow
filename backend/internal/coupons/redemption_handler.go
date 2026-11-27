package coupons

import (
	"errors"
	"github.com/azamatbayramov/referflow/backend/internal/auth"
	"net/http"
	"strings"
)

type RedemptionHandler struct{ Store Store }

func (h RedemptionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		reply(w, 401, map[string]string{"status": "unauthenticated"})
		return
	}
	var result any
	var err error
	code := 200
	switch {
	case r.URL.Path == "/api/redemptions":
		if !method(w, r, "POST") {
			return
		}
		var in RedemptionInput
		if !decode(w, r, &in) {
			return
		}
		var u Redemption
		var replay bool
		u, replay, err = h.Store.Redeem(r.Context(), user.ID, r.Header.Get("Idempotency-Key"), in)
		result = u
		if !replay {
			code = 201
		}
	case r.URL.Path == "/api/redemptions/search":
		if !method(w, r, "POST") {
			return
		}
		var in RedemptionSearch
		if !decode(w, r, &in) {
			return
		}
		result, err = h.Store.SearchRedemptions(r.Context(), user.ID, in)
	case strings.HasPrefix(r.URL.Path, "/api/redemptions/") && strings.HasSuffix(r.URL.Path, "/cancel"):
		if !method(w, r, "POST") {
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/redemptions/"), "/cancel")
		var in struct {
			Reason string `json:"reason"`
		}
		if !decode(w, r, &in) {
			return
		}
		result, err = h.Store.CancelRedemption(r.Context(), user.ID, id, in.Reason)
	case strings.HasPrefix(r.URL.Path, "/api/redemptions/"):
		if !method(w, r, "GET") {
			return
		}
		result, err = h.Store.GetRedemption(r.Context(), user.ID, strings.TrimPrefix(r.URL.Path, "/api/redemptions/"))
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
		case errors.Is(err, ErrRevoked):
			code = 422
			status = "coupon_revoked"
		case errors.Is(err, ErrExpired):
			code = 422
			status = "coupon_expired"
		case errors.Is(err, ErrTotalLimit):
			code = 422
			status = "total_limit_reached"
		case errors.Is(err, ErrPersonalLimit):
			code = 422
			status = "personal_limit_reached"
		case errors.Is(err, ErrArchivedClient):
			code = 422
			status = "client_archived"
		case errors.Is(err, ErrConfirmation):
			code = 422
			status = "confirmation_required"
		}
		reply(w, code, map[string]string{"status": status})
		return
	}
	reply(w, code, result)
}
