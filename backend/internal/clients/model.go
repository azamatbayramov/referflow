package clients

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid  = errors.New("invalid client input")
	ErrNotFound = errors.New("client not found")
	ErrConflict = errors.New("client conflict")
)

type Client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Archived  bool      `json:"archived"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Event struct {
	Version    int64     `json:"version"`
	Action     string    `json:"action"`
	OccurredAt time.Time `json:"occurred_at"`
}
type Input struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Archived bool   `json:"archived"`
	Version  int64  `json:"version"`
}
type Search struct {
	Query  string `json:"query"`
	Phone  string `json:"phone"`
	State  string `json:"state"`
	Offset int    `json:"offset"`
}
type Page struct {
	Items   []Client `json:"items"`
	HasMore bool     `json:"has_more"`
}

func validText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func normalize(in Input) (Input, string, error) {
	if !validText(in.Name) || !validText(in.Phone) {
		return Input{}, "", ErrInvalid
	}
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Phone = strings.TrimSpace(in.Phone)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 120 {
		return Input{}, "", ErrInvalid
	}
	if len(in.Phone) > 40 {
		return Input{}, "", ErrInvalid
	}
	digits := ""
	for i, r := range in.Phone {
		switch {
		case r >= '0' && r <= '9':
			digits += string(r)
		case r == '+' && i == 0, r == ' ', r == '-', r == '(', r == ')':
		default:
			return Input{}, "", ErrInvalid
		}
	}
	if in.Phone != "" && (len(digits) < 7 || len(digits) > 15) {
		return Input{}, "", ErrInvalid
	}
	return in, digits, nil
}
func searchText(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ё", "е") }
func phoneSearch(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func validUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil && len(raw) == 16
}
func fingerprint(in Input) []byte {
	data, _ := json.Marshal(struct{ Name, Phone string }{in.Name, in.Phone})
	sum := sha256.Sum256(data)
	return sum[:]
}
