package coupons

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ Pool *pgxpool.Pool }

const selection = `SELECT c.id::text,c.code,c.discount_type,c.value_minor,c.restrictions,c.total_limit,c.per_client_limit,c.first_visit_only,
 to_char(c.expires_on,'YYYY-MM-DD'),c.expires_at,c.referrer_client_id::text,r.name,c.used_count,
 CASE WHEN c.revoked_at IS NOT NULL THEN 'revoked' WHEN c.expires_at<=now() THEN 'expired'
 WHEN c.total_limit IS NOT NULL AND c.used_count>=c.total_limit THEN 'exhausted' ELSE 'available' END AS status,
 c.revoked_at,c.revoke_reason,c.created_at FROM coupons c LEFT JOIN clients r ON r.id=c.referrer_client_id AND r.specialist_id=c.specialist_id `

func scan(row pgx.Row) (Coupon, error) {
	var c Coupon
	var value int64
	err := row.Scan(&c.ID, &c.Code, &c.DiscountType, &value, &c.Restrictions, &c.TotalLimit, &c.PerClientLimit, &c.FirstVisitOnly, &c.ExpiresOn, &c.ExpiresAt, &c.ReferrerClientID, &c.ReferrerName, &c.UsedCount, &c.Status, &c.RevokedAt, &c.RevokeReason, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	c.Value = formatValue(value)
	return c, err
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (s Store) Create(ctx context.Context, owner, key string, in Input) (Coupon, bool, error) {
	in, value, expires, err := normalize(in)
	if err != nil || !validUUID(key) {
		return Coupon{}, false, ErrInvalid
	}
	hash := fingerprint(in)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Coupon{}, false, err
	}
	defer rollback(tx)
	// Serialize the small issuance workload per specialist, across all API replicas.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "coupon-create:"+owner); err != nil {
		return Coupon{}, false, err
	}
	var oldHash []byte
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text,creation_hash FROM coupons WHERE specialist_id=$1 AND creation_key=$2`, owner, key).Scan(&id, &oldHash)
	if err == nil {
		if !bytes.Equal(hash, oldHash) {
			return Coupon{}, false, ErrConflict
		}
		c, err := scan(tx.QueryRow(ctx, selection+`WHERE c.specialist_id=$1 AND c.id=$2`, owner, id))
		if err != nil {
			return Coupon{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Coupon{}, false, err
		}
		return c, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, false, err
	}
	if in.ReferrerClientID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clients WHERE specialist_id=$1 AND id=$2 AND NOT archived)`, owner, *in.ReferrerClientID).Scan(&exists); err != nil {
			return Coupon{}, false, err
		}
		if !exists {
			return Coupon{}, false, ErrInvalid
		}
	}
	id, err = newUUID()
	if err != nil {
		return Coupon{}, false, err
	}
	inserted := false
	for range 20 {
		code := in.Code
		if code == "" {
			code, err = randomCode()
			if err != nil {
				return Coupon{}, false, err
			}
		}
		command, err := tx.Exec(ctx, `INSERT INTO coupons(id,specialist_id,code,discount_type,value_minor,restrictions,total_limit,per_client_limit,first_visit_only,expires_on,expires_at,referrer_client_id,creation_key,creation_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(specialist_id,code) DO NOTHING`, id, owner, code, in.DiscountType, value, in.Restrictions, in.TotalLimit, in.PerClientLimit, in.FirstVisitOnly, in.ExpiresOn, expires, in.ReferrerClientID, key, hash)
		if err != nil {
			return Coupon{}, false, err
		}
		if command.RowsAffected() == 1 {
			inserted = true
			break
		}
		if in.Code != "" {
			return Coupon{}, false, ErrCodeTaken
		}
	}
	if !inserted {
		return Coupon{}, false, ErrCodeTaken
	}
	if _, err := tx.Exec(ctx, `INSERT INTO coupon_events(coupon_id,specialist_id,action) VALUES($1,$2,'created')`, id, owner); err != nil {
		return Coupon{}, false, err
	}
	c, err := scan(tx.QueryRow(ctx, selection+`WHERE c.specialist_id=$1 AND c.id=$2`, owner, id))
	if err != nil {
		return Coupon{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Coupon{}, false, err
	}
	return c, false, nil
}
func (s Store) AllowLookup(ctx context.Context, owner string) (bool, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `INSERT INTO coupon_lookup_limits(specialist_id,attempts,resets_at) VALUES($1,1,now()+interval '1 minute')
 ON CONFLICT(specialist_id) DO UPDATE SET attempts=CASE WHEN coupon_lookup_limits.resets_at<=now() THEN 1 ELSE LEAST(coupon_lookup_limits.attempts+1,121) END,
 resets_at=CASE WHEN coupon_lookup_limits.resets_at<=now() THEN now()+interval '1 minute' ELSE coupon_lookup_limits.resets_at END RETURNING attempts`, owner).Scan(&n)
	return n <= 120, err
}
func (s Store) Search(ctx context.Context, owner string, in Search) (Page, error) {
	if in.Offset < 0 || in.Offset > 100000 {
		return Page{}, ErrInvalid
	}
	if in.Code != "" {
		code, err := normalizeCode(in.Code)
		if err != nil {
			return Page{}, err
		}
		in.Code = code
	}
	if in.State == "" {
		in.State = "all"
	}
	if in.State != "all" && in.State != "available" && in.State != "expired" && in.State != "revoked" && in.State != "exhausted" {
		return Page{}, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT * FROM (`+selection+`WHERE c.specialist_id=$1 AND ($2='' OR c.code=$2)) AS found
 WHERE ($3='all' OR found.status=$3) ORDER BY created_at DESC,id DESC LIMIT 51 OFFSET $4`, owner, in.Code, in.State, in.Offset)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	p := Page{Items: []Coupon{}}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return Page{}, err
		}
		p.Items = append(p.Items, c)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(p.Items) > 50 {
		p.HasMore = true
		p.Items = p.Items[:50]
	}
	return p, nil
}
func (s Store) Get(ctx context.Context, owner, id string) (Coupon, []Event, error) {
	if !validUUID(id) {
		return Coupon{}, nil, ErrNotFound
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Coupon{}, nil, err
	}
	defer rollback(tx)
	c, err := scan(tx.QueryRow(ctx, selection+`WHERE c.specialist_id=$1 AND c.id=$2`, owner, id))
	if err != nil {
		return Coupon{}, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT action,reason,occurred_at FROM coupon_events WHERE specialist_id=$1 AND coupon_id=$2 ORDER BY occurred_at DESC,action DESC`, owner, id)
	if err != nil {
		return Coupon{}, nil, err
	}
	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Action, &e.Reason, &e.OccurredAt); err != nil {
			rows.Close()
			return Coupon{}, nil, err
		}
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Coupon{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Coupon{}, nil, err
	}
	return c, events, nil
}
func (s Store) Revoke(ctx context.Context, owner, id, reason string) (Coupon, error) {
	if !validUUID(id) {
		return Coupon{}, ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	if !validText(reason, false) || len([]rune(reason)) < 1 || len([]rune(reason)) > 500 {
		return Coupon{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Coupon{}, err
	}
	defer rollback(tx)
	var revoked *time.Time
	if err := tx.QueryRow(ctx, `SELECT revoked_at FROM coupons WHERE specialist_id=$1 AND id=$2 FOR UPDATE`, owner, id).Scan(&revoked); errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, ErrNotFound
	} else if err != nil {
		return Coupon{}, err
	}
	if revoked == nil {
		if _, err := tx.Exec(ctx, `UPDATE coupons SET revoked_at=now(),revoke_reason=$3 WHERE specialist_id=$1 AND id=$2`, owner, id, reason); err != nil {
			return Coupon{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO coupon_events(coupon_id,specialist_id,action,reason) VALUES($1,$2,'revoked',$3)`, id, owner, reason); err != nil {
			return Coupon{}, err
		}
	}
	c, err := scan(tx.QueryRow(ctx, selection+`WHERE c.specialist_id=$1 AND c.id=$2`, owner, id))
	if err != nil {
		return Coupon{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Coupon{}, err
	}
	return c, nil
}
