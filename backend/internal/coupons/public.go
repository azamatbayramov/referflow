package coupons

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PublicCoupon deliberately has no client, owner, audit or usage-count fields.
// Never serialize Coupon or derive this response by removing private JSON keys.
type PublicCoupon struct {
	Code           string     `json:"code"`
	DiscountType   string     `json:"discount_type"`
	Value          string     `json:"value"`
	Restrictions   string     `json:"restrictions"`
	TotalLimit     *int       `json:"total_limit"`
	PerClientLimit *int       `json:"per_client_limit"`
	FirstVisitOnly bool       `json:"first_visit_only"`
	ExpiresOn      *string    `json:"expires_on"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Status         string     `json:"status"`
}

func publicUUID(s string) bool {
	return validUUID(s) && s[14] == '4' && strings.ContainsRune("89abAB", rune(s[19]))
}

func (s Store) GetPublic(ctx context.Context, id string) (PublicCoupon, error) {
	if !publicUUID(id) {
		return PublicCoupon{}, ErrNotFound
	}
	var c PublicCoupon
	var value int64
	err := s.Pool.QueryRow(ctx, `SELECT code,discount_type,value_minor,restrictions,total_limit,per_client_limit,first_visit_only,
 to_char(expires_on,'YYYY-MM-DD'),expires_at,
 CASE WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=clock_timestamp() THEN 'expired'
 WHEN total_limit IS NOT NULL AND used_count>=total_limit THEN 'exhausted' ELSE 'available' END
 FROM coupons WHERE id=$1`, id).Scan(&c.Code, &c.DiscountType, &value, &c.Restrictions, &c.TotalLimit, &c.PerClientLimit, &c.FirstVisitOnly, &c.ExpiresOn, &c.ExpiresAt, &c.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicCoupon{}, ErrNotFound
	}
	c.Value = formatValue(value)
	return c, err
}

type PublicHandler struct{ Store Store }

func (h PublicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	// A public link grants read access to terms only. No session or CSRF lookup,
	// operation creation, cookie writes, counters or request/body logging here.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		reply(w, 405, map[string]string{"status": "method_not_allowed"})
		return
	}
	const prefix = "/api/public/coupons/"
	if !strings.HasPrefix(r.URL.Path, prefix) || !publicUUID(strings.TrimPrefix(r.URL.Path, prefix)) {
		reply(w, 404, map[string]string{"status": "not_found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	c, err := h.Store.GetPublic(ctx, strings.TrimPrefix(r.URL.Path, prefix))
	if errors.Is(err, ErrNotFound) {
		reply(w, 404, map[string]string{"status": "not_found"})
		return
	}
	if err != nil {
		reply(w, 503, map[string]string{"status": "unavailable"})
		return
	}
	reply(w, 200, c)
}
