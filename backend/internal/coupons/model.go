package coupons

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid  = errors.New("invalid coupon")
	ErrNotFound = errors.New("coupon not found")
	ErrConflict = errors.New("coupon conflict")
)

type Input struct {
	DiscountType     string  `json:"discount_type"`
	Value            string  `json:"value"`
	Restrictions     string  `json:"restrictions"`
	TotalLimit       *int    `json:"total_limit"`
	PerClientLimit   *int    `json:"per_client_limit"`
	FirstVisitOnly   bool    `json:"first_visit_only"`
	ExpiresOn        *string `json:"expires_on"`
	ReferrerClientID *string `json:"referrer_client_id"`
}
type Coupon struct {
	ID               string     `json:"id"`
	Code             string     `json:"code"`
	DiscountType     string     `json:"discount_type"`
	Value            string     `json:"value"`
	Restrictions     string     `json:"restrictions"`
	TotalLimit       *int       `json:"total_limit"`
	PerClientLimit   *int       `json:"per_client_limit"`
	FirstVisitOnly   bool       `json:"first_visit_only"`
	ExpiresOn        *string    `json:"expires_on"`
	ExpiresAt        *time.Time `json:"expires_at"`
	ReferrerClientID *string    `json:"referrer_client_id"`
	ReferrerName     *string    `json:"referrer_name"`
	UsedCount        int        `json:"used_count"`
	Status           string     `json:"status"`
	RevokedAt        *time.Time `json:"revoked_at"`
	RevokeReason     *string    `json:"revoke_reason"`
	CreatedAt        time.Time  `json:"created_at"`
}
type Event struct {
	Action     string    `json:"action"`
	Reason     *string   `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}
type Search struct {
	Code   string `json:"code"`
	State  string `json:"state"`
	Offset int    `json:"offset"`
}
type Page struct {
	Items   []Coupon `json:"items"`
	HasMore bool     `json:"has_more"`
}

var decimal = regexp.MustCompile(`^[0-9]{1,10}([.,][0-9]{1,2})?$`)

func parseValue(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if !decimal.MatchString(s) {
		return 0, ErrInvalid
	}
	parts := strings.Split(s, ".")
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	frac := "00"
	if len(parts) == 2 {
		frac = (parts[1] + "0")[:2]
	}
	fraction, _ := strconv.ParseInt(frac, 10, 64)
	v := whole*100 + fraction
	if v < 1 || v > 999999999999 {
		return 0, ErrInvalid
	}
	return v, nil
}
func formatValue(v int64) string { return fmt.Sprintf("%d.%02d", v/100, v%100) }
func normalizeCode(s string) (string, error) {
	if len(s) > 20 {
		return "", ErrInvalid
	}
	s = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(s))
	if len(s) < 1 || len(s) > 6 {
		return "", ErrInvalid
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", ErrInvalid
		}
	}
	return strings.Repeat("0", 6-len(s)) + s, nil
}
func validText(s string, multiline bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t' || r == '\r'))) {
			return false
		}
	}
	return true
}
func normalize(in Input) (Input, int64, *time.Time, error) {
	v, err := parseValue(in.Value)
	if err != nil {
		return in, 0, nil, err
	}
	in.Value = formatValue(v)
	if in.DiscountType != "percent" && in.DiscountType != "fixed" || in.DiscountType == "percent" && v > 10000 {
		return in, 0, nil, ErrInvalid
	}
	in.Restrictions = strings.TrimSpace(in.Restrictions)
	if !validText(in.Restrictions, true) || utf8.RuneCountInString(in.Restrictions) > 2000 {
		return in, 0, nil, ErrInvalid
	}
	for _, n := range []*int{in.TotalLimit, in.PerClientLimit} {
		if n != nil && (*n < 1 || *n > 2147483647) {
			return in, 0, nil, ErrInvalid
		}
	}
	var expires *time.Time
	if in.ExpiresOn != nil {
		date, err := time.ParseInLocation("2006-01-02", *in.ExpiresOn, time.FixedZone("MSK", 3*3600))
		if err != nil || date.Year() < 1 || date.Year() > 9998 {
			return in, 0, nil, ErrInvalid
		}
		end := date.AddDate(0, 0, 1).UTC()
		expires = &end
	}
	if in.ReferrerClientID != nil && !validUUID(*in.ReferrerClientID) {
		return in, 0, nil, ErrInvalid
	}
	return in, v, expires, nil
}
func validUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	b, e := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return e == nil && len(b) == 16
}
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
func fingerprint(in Input) []byte { b, _ := json.Marshal(in); s := sha256.Sum256(b); return s[:] }
