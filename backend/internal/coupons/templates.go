package coupons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// A template contains defaults only: no client, public ID, code or counters.
type TemplateInput struct {
	Name           string  `json:"name"`
	DiscountType   string  `json:"discount_type"`
	Value          string  `json:"value"`
	Restrictions   string  `json:"restrictions"`
	TotalLimit     *int    `json:"total_limit"`
	PerClientLimit *int    `json:"per_client_limit"`
	FirstVisitOnly bool    `json:"first_visit_only"`
	ExpiresOn      *string `json:"expires_on"`
	Archived       bool    `json:"archived"`
	Version        int     `json:"version"`
}
type Template struct {
	TemplateInput
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type TemplateSearch struct {
	Query  string `json:"query"`
	State  string `json:"state"`
	Offset int    `json:"offset"`
	Sort   string `json:"sort"`
}
type TemplatePage struct {
	Items   []Template `json:"items"`
	HasMore bool       `json:"has_more"`
}

func normalizeTemplate(in TemplateInput) (TemplateInput, int64, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !validText(in.Name, false) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 120 {
		return in, 0, ErrInvalid
	}
	terms, value, _, err := normalize(Input{DiscountType: in.DiscountType, Value: in.Value, Restrictions: in.Restrictions, TotalLimit: in.TotalLimit, PerClientLimit: in.PerClientLimit, FirstVisitOnly: in.FirstVisitOnly, ExpiresOn: in.ExpiresOn})
	in.Value, in.Restrictions = terms.Value, terms.Restrictions
	return in, value, err
}

const templateColumns = `id::text,name,discount_type,value_minor,restrictions,total_limit,per_client_limit,first_visit_only,to_char(expires_on,'YYYY-MM-DD'),archived,version,created_at,updated_at`

func scanTemplate(row pgx.Row) (Template, error) {
	var t Template
	var value int64
	err := row.Scan(&t.ID, &t.Name, &t.DiscountType, &value, &t.Restrictions, &t.TotalLimit, &t.PerClientLimit, &t.FirstVisitOnly, &t.ExpiresOn, &t.Archived, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	t.Value = formatValue(value)
	return t, err
}

// SaveTemplate creates immutable terms or changes only their archive state.
// The permanent replay key is checked before the current archive version.
func (s Store) SaveTemplate(ctx context.Context, owner, id, key string, in TemplateInput) (Template, bool, error) {
	creating := id == ""
	if !creating && !validUUID(id) {
		return Template{}, false, ErrNotFound
	}
	in, value, err := normalizeTemplate(in)
	if err != nil || !validUUID(key) || creating && (in.Version != 0 || in.Archived) || !creating && (in.Version < 1 || in.Version >= 2147483647) {
		return Template{}, false, ErrInvalid
	}
	data, _ := json.Marshal(struct {
		ID    string
		Input TemplateInput
	}{id, in})
	hash := sha256.Sum256(data)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Template{}, false, err
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "coupon-template:"+owner); err != nil {
		return Template{}, false, err
	}
	var oldHash []byte
	var oldID string
	err = tx.QueryRow(ctx, `SELECT template_id::text,request_hash FROM coupon_template_operations WHERE specialist_id=$1 AND idempotency_key=$2`, owner, key).Scan(&oldID, &oldHash)
	if err == nil {
		if !bytes.Equal(hash[:], oldHash) {
			return Template{}, false, ErrConflict
		}
		t, err := scanTemplate(tx.QueryRow(ctx, `SELECT `+templateColumns+` FROM coupon_templates WHERE specialist_id=$1 AND id=$2`, owner, oldID))
		if err != nil {
			return Template{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Template{}, false, err
		}
		return t, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Template{}, false, err
	}
	if creating {
		id, err = newUUID()
		if err != nil {
			return Template{}, false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO coupon_templates(id,specialist_id,name,discount_type,value_minor,restrictions,total_limit,per_client_limit,first_visit_only,expires_on) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, owner, in.Name, in.DiscountType, value, in.Restrictions, in.TotalLimit, in.PerClientLimit, in.FirstVisitOnly, in.ExpiresOn)
	} else {
		old, getErr := scanTemplate(tx.QueryRow(ctx, `SELECT `+templateColumns+` FROM coupon_templates WHERE specialist_id=$1 AND id=$2 FOR UPDATE`, owner, id))
		if getErr != nil {
			return Template{}, false, getErr
		}
		if old.Version != in.Version {
			return Template{}, false, ErrConflict
		}
		terms := old.TemplateInput
		terms.Archived, terms.Version = in.Archived, in.Version
		if !reflect.DeepEqual(terms, in) {
			return Template{}, false, ErrInvalid
		}
		_, err = tx.Exec(ctx, `UPDATE coupon_templates SET archived=$3,version=version+1,updated_at=clock_timestamp() WHERE specialist_id=$1 AND id=$2`, owner, id, in.Archived)
	}
	if err != nil {
		return Template{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO coupon_template_operations(specialist_id,idempotency_key,template_id,request_hash) VALUES($1,$2,$3,$4)`, owner, key, id, hash[:]); err != nil {
		return Template{}, false, err
	}
	t, err := scanTemplate(tx.QueryRow(ctx, `SELECT `+templateColumns+` FROM coupon_templates WHERE specialist_id=$1 AND id=$2`, owner, id))
	if err != nil {
		return Template{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Template{}, false, err
	}
	return t, false, nil
}

func (s Store) GetTemplate(ctx context.Context, owner, id string) (Template, error) {
	if !validUUID(id) {
		return Template{}, ErrNotFound
	}
	return scanTemplate(s.Pool.QueryRow(ctx, `SELECT `+templateColumns+` FROM coupon_templates WHERE specialist_id=$1 AND id=$2`, owner, id))
}

func (s Store) SearchTemplates(ctx context.Context, owner string, in TemplateSearch) (TemplatePage, error) {
	in.Query = strings.TrimSpace(in.Query)
	if !validText(in.Query, false) || utf8.RuneCountInString(in.Query) > 120 || in.Offset < 0 || in.Offset > 100000 {
		return TemplatePage{}, ErrInvalid
	}
	if in.State == "" {
		in.State = "active"
	}
	if in.State != "active" && in.State != "archived" && in.State != "all" {
		return TemplatePage{}, ErrInvalid
	}
	if in.Sort != "" && in.Sort != "recent" && in.Sort != "popular" {
		return TemplatePage{}, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+templateColumns+` FROM coupon_templates t WHERE specialist_id=$1 AND ($2='all' OR archived=($2='archived')) AND strpos(replace(lower(name),'ё','е'),$3)>0 ORDER BY CASE WHEN $5='popular' THEN (SELECT count(*) FROM coupons c WHERE c.specialist_id=t.specialist_id AND c.template_id=t.id) ELSE 0 END DESC,created_at DESC,id DESC LIMIT 51 OFFSET $4`, owner, in.State, strings.ReplaceAll(strings.ToLower(in.Query), "ё", "е"), in.Offset, in.Sort)
	if err != nil {
		return TemplatePage{}, err
	}
	defer rows.Close()
	page := TemplatePage{Items: []Template{}}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return TemplatePage{}, err
		}
		page.Items = append(page.Items, t)
	}
	if err := rows.Err(); err != nil {
		return TemplatePage{}, err
	}
	if len(page.Items) > 50 {
		page.Items = page.Items[:50]
		page.HasMore = true
	}
	return page, nil
}
