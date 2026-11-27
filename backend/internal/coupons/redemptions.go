package coupons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	ErrRevoked        = errors.New("coupon revoked")
	ErrExpired        = errors.New("coupon expired")
	ErrTotalLimit     = errors.New("total limit reached")
	ErrPersonalLimit  = errors.New("personal limit reached")
	ErrArchivedClient = errors.New("client archived")
	ErrConfirmation   = errors.New("confirmation required")
)

type RedemptionInput struct {
	CouponID              string `json:"coupon_id"`
	ClientID              string `json:"client_id"`
	SingleCouponConfirmed bool   `json:"single_coupon_confirmed"`
	FirstVisitConfirmed   bool   `json:"first_visit_confirmed"`
}
type Redemption struct {
	ID                    string     `json:"id"`
	CouponID              string     `json:"coupon_id"`
	CouponCode            string     `json:"coupon_code"`
	ClientID              string     `json:"client_id"`
	ClientName            string     `json:"client_name"`
	AuthorID              string     `json:"author_id"`
	AuthorName            string     `json:"author_name"`
	DiscountType          string     `json:"discount_type"`
	Value                 string     `json:"value"`
	SingleCouponConfirmed bool       `json:"single_coupon_confirmed"`
	FirstVisitConfirmed   bool       `json:"first_visit_confirmed"`
	FirstVisitRequired    bool       `json:"first_visit_required"`
	ConfirmedAt           time.Time  `json:"confirmed_at"`
	CanceledAt            *time.Time `json:"canceled_at"`
	CanceledBy            *string    `json:"canceled_by"`
	CanceledByName        *string    `json:"canceled_by_name"`
	CancelReason          *string    `json:"cancel_reason"`
}
type RedemptionSearch struct {
	CouponID string `json:"coupon_id"`
	ClientID string `json:"client_id"`
	Offset   int    `json:"offset"`
}
type RedemptionPage struct {
	Items   []Redemption `json:"items"`
	HasMore bool         `json:"has_more"`
}

const redemptionSelection = `SELECT u.id::text,u.coupon_id::text,c.code,u.client_id::text,p.name,u.specialist_id::text,a.name,u.discount_type,u.value_minor,
 u.single_coupon_confirmed,u.first_visit_confirmed,u.first_visit_required,u.confirmed_at,u.canceled_at,u.canceled_by::text,x.name,u.cancel_reason
 FROM coupon_redemptions u JOIN coupons c ON c.id=u.coupon_id AND c.specialist_id=u.specialist_id
 JOIN clients p ON p.id=u.client_id AND p.specialist_id=u.specialist_id JOIN specialists a ON a.id=u.specialist_id
 LEFT JOIN specialists x ON x.id=u.canceled_by `

func scanRedemption(row pgx.Row) (Redemption, error) {
	var u Redemption
	var value int64
	err := row.Scan(&u.ID, &u.CouponID, &u.CouponCode, &u.ClientID, &u.ClientName, &u.AuthorID, &u.AuthorName, &u.DiscountType, &value, &u.SingleCouponConfirmed, &u.FirstVisitConfirmed, &u.FirstVisitRequired, &u.ConfirmedAt, &u.CanceledAt, &u.CanceledBy, &u.CanceledByName, &u.CancelReason)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	u.Value = formatValue(value)
	return u, err
}
func (s Store) Redeem(ctx context.Context, owner, key string, in RedemptionInput) (Redemption, bool, error) {
	if !validUUID(in.CouponID) || !validUUID(in.ClientID) || !validUUID(key) {
		return Redemption{}, false, ErrInvalid
	}
	in.CouponID = strings.ToLower(in.CouponID)
	in.ClientID = strings.ToLower(in.ClientID)
	key = strings.ToLower(key)
	payload, _ := json.Marshal(in)
	hash := sha256.Sum256(payload)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Redemption{}, false, err
	}
	defer rollback(tx)
	// A key is shared by all coupons of this owner. Acquire it before any row lock.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "coupon-redeem:"+owner+":"+key); err != nil {
		return Redemption{}, false, err
	}
	var id string
	var oldHash []byte
	err = tx.QueryRow(ctx, `SELECT id::text,request_hash FROM coupon_redemptions WHERE specialist_id=$1 AND idempotency_key=$2`, owner, key).Scan(&id, &oldHash)
	if err == nil {
		if !bytes.Equal(hash[:], oldHash) {
			return Redemption{}, false, ErrConflict
		}
		u, err := scanRedemption(tx.QueryRow(ctx, redemptionSelection+`WHERE u.specialist_id=$1 AND u.id=$2`, owner, id))
		if err != nil {
			return Redemption{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Redemption{}, false, err
		}
		return u, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, false, err
	}
	var kind string
	var value int64
	var total, personal *int
	var first bool
	var used int
	var revoked, expires *time.Time
	err = tx.QueryRow(ctx, `/* coupon redemption lock */ SELECT discount_type,value_minor,total_limit,per_client_limit,first_visit_only,used_count,revoked_at,expires_at
 FROM coupons WHERE specialist_id=$1 AND id=$2 FOR UPDATE`, owner, in.CouponID).Scan(&kind, &value, &total, &personal, &first, &used, &revoked, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, false, ErrNotFound
	}
	if err != nil {
		return Redemption{}, false, err
	}
	var archived bool
	err = tx.QueryRow(ctx, `SELECT archived FROM clients WHERE specialist_id=$1 AND id=$2 FOR SHARE`, owner, in.ClientID).Scan(&archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, false, ErrNotFound
	}
	if err != nil {
		return Redemption{}, false, err
	}
	if archived {
		return Redemption{}, false, ErrArchivedClient
	}
	if !in.SingleCouponConfirmed || first && !in.FirstVisitConfirmed {
		return Redemption{}, false, ErrConfirmation
	}
	if revoked != nil {
		return Redemption{}, false, ErrRevoked
	}
	if total != nil && used >= *total {
		return Redemption{}, false, ErrTotalLimit
	}
	if personal != nil {
		var count int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM coupon_redemptions WHERE specialist_id=$1 AND coupon_id=$2 AND client_id=$3 AND canceled_at IS NULL`, owner, in.CouponID, in.ClientID).Scan(&count); err != nil {
			return Redemption{}, false, err
		}
		if count >= int64(*personal) {
			return Redemption{}, false, ErrPersonalLimit
		}
	}
	// now() is the transaction START; a request may have waited past expiry on locks.
	var confirmed time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&confirmed); err != nil {
		return Redemption{}, false, err
	}
	if expires != nil && !expires.After(confirmed) {
		return Redemption{}, false, ErrExpired
	}
	id, err = newUUID()
	if err != nil {
		return Redemption{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO coupon_redemptions(id,specialist_id,coupon_id,client_id,discount_type,value_minor,single_coupon_confirmed,first_visit_confirmed,first_visit_required,confirmed_at,idempotency_key,request_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, owner, in.CouponID, in.ClientID, kind, value, in.SingleCouponConfirmed, in.FirstVisitConfirmed, first, confirmed, key, hash[:]); err != nil {
		return Redemption{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE coupons SET used_count=used_count+1 WHERE specialist_id=$1 AND id=$2`, owner, in.CouponID); err != nil {
		return Redemption{}, false, err
	}
	u, err := scanRedemption(tx.QueryRow(ctx, redemptionSelection+`WHERE u.specialist_id=$1 AND u.id=$2`, owner, id))
	if err != nil {
		return Redemption{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Redemption{}, false, err
	}
	return u, false, nil
}
func (s Store) CancelRedemption(ctx context.Context, owner, id, reason string) (Redemption, error) {
	if !validUUID(id) {
		return Redemption{}, ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	if !validText(reason, false) || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 500 {
		return Redemption{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Redemption{}, err
	}
	defer rollback(tx)
	var couponID string
	if err := tx.QueryRow(ctx, `SELECT coupon_id::text FROM coupon_redemptions WHERE specialist_id=$1 AND id=$2`, owner, id).Scan(&couponID); errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, ErrNotFound
	} else if err != nil {
		return Redemption{}, err
	}
	// Same order as redemption/revocation: coupon first, then operation. No double release.
	if _, err := tx.Exec(ctx, `SELECT id FROM coupons WHERE specialist_id=$1 AND id=$2 FOR UPDATE`, owner, couponID); err != nil {
		return Redemption{}, err
	}
	var canceled *time.Time
	if err := tx.QueryRow(ctx, `SELECT canceled_at FROM coupon_redemptions WHERE specialist_id=$1 AND id=$2 FOR UPDATE`, owner, id).Scan(&canceled); err != nil {
		return Redemption{}, err
	}
	if canceled == nil {
		if _, err := tx.Exec(ctx, `UPDATE coupon_redemptions SET canceled_at=clock_timestamp(),canceled_by=$1,cancel_reason=$3 WHERE specialist_id=$1 AND id=$2`, owner, id, reason); err != nil {
			return Redemption{}, err
		}
		changed, err := tx.Exec(ctx, `UPDATE coupons SET used_count=used_count-1 WHERE specialist_id=$1 AND id=$2 AND used_count>0`, owner, couponID)
		if err != nil {
			return Redemption{}, err
		}
		if changed.RowsAffected() != 1 {
			return Redemption{}, errors.New("coupon counter is inconsistent")
		}
	}
	u, err := scanRedemption(tx.QueryRow(ctx, redemptionSelection+`WHERE u.specialist_id=$1 AND u.id=$2`, owner, id))
	if err != nil {
		return Redemption{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Redemption{}, err
	}
	return u, nil
}
func (s Store) GetRedemption(ctx context.Context, owner, id string) (Redemption, error) {
	if !validUUID(id) {
		return Redemption{}, ErrNotFound
	}
	return scanRedemption(s.Pool.QueryRow(ctx, redemptionSelection+`WHERE u.specialist_id=$1 AND u.id=$2`, owner, id))
}
func (s Store) SearchRedemptions(ctx context.Context, owner string, in RedemptionSearch) (RedemptionPage, error) {
	if in.Offset < 0 || in.Offset > 100000 || in.CouponID == "" && in.ClientID == "" {
		return RedemptionPage{}, ErrInvalid
	}
	if in.CouponID != "" {
		if !validUUID(in.CouponID) {
			return RedemptionPage{}, ErrInvalid
		}
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM coupons WHERE specialist_id=$1 AND id=$2)`, owner, in.CouponID).Scan(&exists); err != nil {
			return RedemptionPage{}, err
		}
		if !exists {
			return RedemptionPage{}, ErrNotFound
		}
	}
	if in.ClientID != "" {
		if !validUUID(in.ClientID) {
			return RedemptionPage{}, ErrInvalid
		}
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clients WHERE specialist_id=$1 AND id=$2)`, owner, in.ClientID).Scan(&exists); err != nil {
			return RedemptionPage{}, err
		}
		if !exists {
			return RedemptionPage{}, ErrNotFound
		}
	}
	// NULL UUID parameters avoid casts of empty strings and remain parameterized.
	var couponID, clientID *string
	if in.CouponID != "" {
		couponID = &in.CouponID
	}
	if in.ClientID != "" {
		clientID = &in.ClientID
	}
	rows, err := s.Pool.Query(ctx, redemptionSelection+`WHERE u.specialist_id=$1 AND ($2::uuid IS NULL OR u.coupon_id=$2) AND ($3::uuid IS NULL OR u.client_id=$3) ORDER BY u.confirmed_at DESC,u.id DESC LIMIT 51 OFFSET $4`, owner, couponID, clientID, in.Offset)
	if err != nil {
		return RedemptionPage{}, err
	}
	defer rows.Close()
	page := RedemptionPage{Items: []Redemption{}}
	for rows.Next() {
		u, err := scanRedemption(rows)
		if err != nil {
			return RedemptionPage{}, err
		}
		page.Items = append(page.Items, u)
	}
	if err := rows.Err(); err != nil {
		return RedemptionPage{}, err
	}
	if len(page.Items) > 50 {
		page.Items = page.Items[:50]
		page.HasMore = true
	}
	return page, nil
}
