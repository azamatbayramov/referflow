package auth

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	password := strings.Repeat("Я", 16) + " 😀 "
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes must have different random salts")
	}
	if !CheckPassword(first, password) || CheckPassword(first, password+"x") || CheckPassword(first, strings.TrimSpace(password)) {
		t.Fatal("password verification or whitespace handling failed")
	}
	for _, bad := range []string{"", "pbkdf2-sha256$1$x$x", first + "$extra", "plain-text"} {
		if CheckPassword(bad, password) {
			t.Fatal("malformed hash accepted")
		}
	}
	for _, bad := range []string{"", strings.Repeat("x", 7), strings.Repeat("Я", 7), strings.Repeat("x", 129), string([]byte{0xff})} {
		if _, err := HashPassword(bad); err == nil {
			t.Fatal("password policy not enforced")
		}
	}
}

func TestPasswordLengthBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
	}{
		{"minimum ASCII", strings.Repeat("x", 8)},
		{"minimum Unicode", strings.Repeat("Я", 8)},
		{"maximum Unicode", strings.Repeat("😀", 128)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := HashPassword(tc.password)
			if err != nil {
				t.Fatal(err)
			}
			if !CheckPassword(hash, tc.password) {
				t.Fatal("accepted password cannot be verified")
			}
		})
	}
}

func TestLoginNormalization(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{"  SPECIALIST_1 ", "specialist_1"},
		{"Анна.ИВАНОВА", "анна.иванова"},
		{"a", "a"},
		{strings.Repeat("Я", 254), strings.Repeat("я", 254)},
		{"SPECIALIST@EXAMPLE.TEST", "specialist@example.test"},
	} {
		got, err := NormalizeLogin(tc.input)
		if err != nil || got != tc.expected {
			t.Fatalf("normalizing %q: got %q, error %v", tc.input, got, err)
		}
	}
	for _, value := range []string{"", "  ", "two words", "two\twords", "a\nb", "a\u0000b", "a\u200bb", "a\u202eb", strings.Repeat("я", 255), string([]byte{0xff})} {
		if _, err := NormalizeLogin(value); err == nil {
			t.Fatalf("invalid login accepted: %q", value)
		}
	}
}
