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
	for _, bad := range []string{"", strings.Repeat("x", 14), strings.Repeat("x", 129), string([]byte{0xff})} {
		if _, err := HashPassword(bad); err == nil {
			t.Fatal("password policy not enforced")
		}
	}
}
func TestEmailNormalization(t *testing.T) {
	got, err := NormalizeEmail("  SPECIALIST@EXAMPLE.TEST ")
	if err != nil || got != "specialist@example.test" {
		t.Fatal(got, err)
	}
	for _, value := range []string{"", "missing-domain", "Name <person@example.test>", "a@example.test\nb@example.test"} {
		if _, err := NormalizeEmail(value); err == nil {
			t.Fatal("invalid account identifier accepted")
		}
	}
}
