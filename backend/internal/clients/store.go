package clients

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ Pool *pgxpool.Pool }

const columns = `id::text,name,phone,archived,version,created_at,updated_at`

func scan(row pgx.Row) (Client, error) {
	var c Client
	err := row.Scan(&c.ID, &c.Name, &c.Phone, &c.Archived, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}
func (s Store) Create(ctx context.Context, owner, key string, in Input) (Client, bool, error) {
	in, digits, err := normalize(in)
	if err != nil || !validUUID(key) || in.Archived || in.Version != 0 {
		return Client{}, false, ErrInvalid
	}
	hash := fingerprint(in)
	id, err := newUUID()
	if err != nil {
		return Client{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Client{}, false, err
	}
	defer tx.Rollback(ctx)
	c, err := scan(tx.QueryRow(ctx, `INSERT INTO clients(id,specialist_id,name,name_search,phone,phone_digits,creation_key,creation_hash)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(specialist_id,creation_key) DO NOTHING RETURNING `+columns, id, owner, in.Name, searchText(in.Name), in.Phone, digits, key, hash))
	replay := errors.Is(err, ErrNotFound)
	if replay {
		var oldHash []byte
		if err = tx.QueryRow(ctx, `SELECT creation_hash FROM clients WHERE specialist_id=$1 AND creation_key=$2`, owner, key).Scan(&oldHash); err != nil {
			return Client{}, false, err
		}
		if !bytes.Equal(hash, oldHash) {
			return Client{}, false, ErrConflict
		}
		c, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM clients WHERE specialist_id=$1 AND creation_key=$2`, owner, key))
	} else if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO client_events(client_id,version,specialist_id,action) VALUES($1,1,$2,'created')`, c.ID, owner)
	}
	if err != nil {
		return Client{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Client{}, false, err
	}
	return c, replay, nil
}
func (s Store) Search(ctx context.Context, owner string, q Search) (Page, error) {
	if !validText(q.Query) || utf8.RuneCountInString(q.Query) > 120 || !validText(q.Phone) || len(q.Phone) > 40 || q.Offset < 0 || q.Offset > 100000 {
		return Page{}, ErrInvalid
	}
	q.Query = strings.Join(strings.Fields(q.Query), " ")
	if q.State == "" {
		q.State = "active"
	}
	if q.State != "active" && q.State != "archived" && q.State != "all" {
		return Page{}, ErrInvalid
	}
	digits := phoneSearch(q.Phone)
	if digits == "" {
		digits = phoneSearch(q.Query)
	}
	// An explicit phone is a second possible duplicate match, not an AND condition.
	rows, err := s.Pool.Query(ctx, `SELECT `+columns+` FROM clients WHERE specialist_id=$1
 AND ($2='all' OR archived=($2='archived'))
 AND (($3='' AND $4='') OR ($3<>'' AND strpos(name_search,$3)>0) OR ($4<>'' AND strpos(phone_digits,$4)>0))
 ORDER BY created_at DESC,id DESC LIMIT 51 OFFSET $5`, owner, q.State, searchText(q.Query), digits, q.Offset)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Items: []Client{}}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, c)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Items) > 50 {
		page.HasMore = true
		page.Items = page.Items[:50]
	}
	return page, nil
}
func (s Store) Get(ctx context.Context, owner, id string) (Client, []Event, error) {
	if !validUUID(id) {
		return Client{}, nil, ErrNotFound
	}
	c, err := scan(s.Pool.QueryRow(ctx, `SELECT `+columns+` FROM clients WHERE specialist_id=$1 AND id=$2`, owner, id))
	if err != nil {
		return Client{}, nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT version,action,occurred_at FROM client_events WHERE specialist_id=$1 AND client_id=$2 ORDER BY version DESC LIMIT 50`, owner, id)
	if err != nil {
		return Client{}, nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Version, &e.Action, &e.OccurredAt); err != nil {
			return Client{}, nil, err
		}
		events = append(events, e)
	}
	return c, events, rows.Err()
}
func (s Store) Update(ctx context.Context, owner, id string, in Input) (Client, error) {
	if !validUUID(id) {
		return Client{}, ErrNotFound
	}
	in, digits, err := normalize(in)
	if err != nil || in.Version < 1 {
		return Client{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Client{}, err
	}
	defer tx.Rollback(ctx)
	old, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM clients WHERE specialist_id=$1 AND id=$2 FOR UPDATE`, owner, id))
	if err != nil {
		return Client{}, err
	}
	if old.Version != in.Version {
		return Client{}, ErrConflict
	}
	if old.Name == in.Name && old.Phone == in.Phone && old.Archived == in.Archived {
		return old, nil
	}
	c, err := scan(tx.QueryRow(ctx, `UPDATE clients SET name=$3,name_search=$4,phone=$5,phone_digits=$6,archived=$7,version=version+1,updated_at=now()
 WHERE specialist_id=$1 AND id=$2 RETURNING `+columns, owner, id, in.Name, searchText(in.Name), in.Phone, digits, in.Archived))
	if err != nil {
		return Client{}, err
	}
	action := "updated"
	if old.Archived != c.Archived {
		if c.Archived {
			action = "archived"
		} else {
			action = "restored"
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO client_events(client_id,version,specialist_id,action) VALUES($1,$2,$3,$4)`, id, c.Version, owner, action); err != nil {
		return Client{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Client{}, err
	}
	return c, nil
}
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
