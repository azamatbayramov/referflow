package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const passwordIterations = 600000
const passwordPrefix = "pbkdf2-sha256$600000$"

func NormalizeLogin(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", errors.New("invalid login")
	}
	login := strings.ToLower(strings.TrimSpace(raw))
	if login == "" || utf8.RuneCountInString(login) > 254 {
		return "", errors.New("login must contain 1 to 254 characters")
	}
	for _, char := range login {
		if unicode.IsSpace(char) || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return "", errors.New("login cannot contain spaces or control characters")
		}
	}
	return login, nil
}

func HashPassword(password string) (string, error) {
	n := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || n < 8 || n > 128 {
		return "", errors.New("password must contain 8 to 128 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("cannot generate password salt")
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return "", errors.New("cannot hash password")
	}
	return passwordPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func CheckPassword(encoded, password string) bool {
	if len(password) > 512 || !utf8.ValidString(password) || !strings.HasPrefix(encoded, passwordPrefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, passwordPrefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	return err == nil && subtle.ConstantTimeCompare(actual, expected) == 1
}
