package coupons

import (
	"context"
	"time"
)

// Statistics counts recorded coupon operations, never actual appointments.
// All aggregates share one statement snapshot; revocation/expiry do not erase
// prior uses and archived/corrected clients keep their identity.
type Statistics struct {
	AsOf                time.Time `json:"as_of"`
	IssuedCoupons       int64     `json:"issued_coupons"`
	UsedCoupons         int64     `json:"used_coupons"`
	ActiveRedemptions   int64     `json:"active_redemptions"`
	UniqueClients       int64     `json:"unique_clients"`
	FirstVisitClients   int64     `json:"first_visit_clients"`
	CanceledRedemptions int64     `json:"canceled_redemptions"`
}

func (s Store) Statistics(ctx context.Context, owner string) (Statistics, error) {
	var result Statistics
	err := s.Pool.QueryRow(ctx, `SELECT statement_timestamp(),
 (SELECT count(*) FROM coupons WHERE specialist_id=$1),
 count(DISTINCT coupon_id) FILTER (WHERE canceled_at IS NULL),
 count(*) FILTER (WHERE canceled_at IS NULL),
 count(DISTINCT client_id) FILTER (WHERE canceled_at IS NULL),
 count(DISTINCT client_id) FILTER (WHERE canceled_at IS NULL AND first_visit_confirmed),
 count(*) FILTER (WHERE canceled_at IS NOT NULL)
 FROM coupon_redemptions WHERE specialist_id=$1`, owner).Scan(&result.AsOf, &result.IssuedCoupons, &result.UsedCoupons, &result.ActiveRedemptions, &result.UniqueClients, &result.FirstVisitClients, &result.CanceledRedemptions)
	return result, err
}
