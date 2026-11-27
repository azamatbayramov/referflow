package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	user     User
	hash     string
	sessions map[string]Session
	allowed  bool
	offline  bool
}

func (s *memoryStore) Account(_ context.Context, email string) (User, string, error) {
	if s.offline {
		return User{}, "", errors.New("private database details")
	}
	if email != s.user.Email {
		return User{}, "", ErrNotFound
	}
	return s.user, s.hash, nil
}
func (s *memoryStore) AllowLogin(context.Context, string) (bool, error) {
	if s.offline {
		return false, errors.New("private database details")
	}
	return s.allowed, nil
}
func (s *memoryStore) CreateSession(_ context.Context, id string, hash, old []byte, ttl time.Duration) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, string(old))
	expires := time.Now().Add(ttl)
	s.sessions[string(hash)] = Session{User: s.user, ExpiresAt: expires}
	return expires, nil
}
func (s *memoryStore) Session(_ context.Context, hash []byte) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offline {
		return Session{}, errors.New("private database details")
	}
	session, ok := s.sessions[string(hash)]
	if !ok {
		return Session{}, ErrNotFound
	}
	return session, nil
}
func (s *memoryStore) DeleteSession(_ context.Context, hash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offline {
		return errors.New("private database details")
	}
	delete(s.sessions, string(hash))
	return nil
}
func fixture(t *testing.T) (*Handler, *memoryStore, string) {
	t.Helper()
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{user: User{ID: "f37d3f11-3531-4ec1-897c-bd6c328a1b43", Email: "specialist@example.test", Name: "Тестовый специалист"}, hash: hash, sessions: map[string]Session{}, allowed: true}
	handler, err := NewHandler(store, Options{Origins: []string{"https://referflow.example"}, SecureCookie: true, SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store, password
}
func call(h *Handler, method, path, body string, cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Origin", "https://referflow.example")
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}
func credentials(email, password string) string {
	data, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return string(data)
}
func loginForTest(t *testing.T, h *Handler, s *memoryStore, password string) (*http.Cookie, Session) {
	t.Helper()
	response := call(h, "POST", "/api/auth/login", credentials(s.user.Email, password), nil, "")
	if response.Code != 200 {
		t.Fatalf("login: %d %s", response.Code, response.Body.String())
	}
	var session Session
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	return response.Result().Cookies()[0], session
}
func TestSessionLifecycleAndOwnership(t *testing.T) {
	h, s, password := fixture(t)
	if res := call(h, "GET", "/api/auth/session", "", nil, ""); res.Code != 401 {
		t.Fatal(res.Code)
	}
	cookie, session := loginForTest(t, h, s, password)
	if cookie.Name != "__Host-referflow_session" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 3600 {
		t.Fatalf("unsafe cookie flags: %+v", cookie)
	}
	if session.User.ID != s.user.ID || session.CSRFToken == "" || session.CSRFToken == cookie.Value {
		t.Fatal("invalid session response")
	}
	if _, ok := s.sessions[string(digest(cookie.Value))]; !ok {
		t.Fatal("session hash not stored")
	}
	if _, ok := s.sessions[cookie.Value]; ok {
		t.Fatal("raw session token was stored")
	}
	res := call(h, "GET", "/api/auth/session?specialist_id=someone-else", "", cookie, "")
	if res.Code != 200 || !bytes.Contains(res.Body.Bytes(), []byte(s.user.ID)) {
		t.Fatal("client selected session owner")
	}
	for _, secret := range []string{password, s.hash, cookie.Value} {
		if bytes.Contains(res.Body.Bytes(), []byte(secret)) {
			t.Fatal("secret in response")
		}
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session must not be cached")
	}
	if res := call(h, "POST", "/api/auth/logout", "", cookie, "incorrect"); res.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if res := call(h, "GET", "/api/auth/logout", "", cookie, session.CSRFToken); res.Code != 405 {
		t.Fatal("GET logged out")
	}
	res = call(h, "POST", "/api/auth/logout", "", cookie, session.CSRFToken)
	if res.Code != 204 || res.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	if res := call(h, "GET", "/api/auth/session", "", cookie, ""); res.Code != 401 {
		t.Fatal("old cookie survived logout")
	}
}
func TestRotationExpiryAndDatabaseFailure(t *testing.T) {
	h, s, password := fixture(t)
	old, _ := loginForTest(t, h, s, password)
	res := call(h, "POST", "/api/auth/login", credentials(s.user.Email, password), old, "")
	if res.Code != 200 {
		t.Fatal(res.Code)
	}
	current := res.Result().Cookies()[0]
	if current.Value == old.Value {
		t.Fatal("session fixation")
	}
	if res := call(h, "GET", "/api/auth/session", "", old, ""); res.Code != 401 {
		t.Fatal("replaced session still valid")
	}
	expired := s.sessions[string(digest(current.Value))]
	expired.ExpiresAt = time.Now().Add(-time.Second)
	s.sessions[string(digest(current.Value))] = expired
	if res := call(h, "GET", "/api/auth/session", "", current, ""); res.Code != 401 {
		t.Fatal("expired session still valid")
	}
	s.offline = true
	res = call(h, "GET", "/api/auth/session", "", current, "")
	if res.Code != 503 || strings.Contains(res.Body.String(), "private") || len(res.Result().Cookies()) != 0 {
		t.Fatal("database outage leaked details or cleared cookie")
	}
}
func TestLoginErrorsAndCSRF(t *testing.T) {
	h, s, password := fixture(t)
	for _, email := range []string{s.user.Email, "missing@example.test"} {
		res := call(h, "POST", "/api/auth/login", credentials(email, password+"wrong"), nil, "")
		if res.Code != 401 || strings.TrimSpace(res.Body.String()) != `{"status":"invalid_credentials"}` || len(res.Result().Cookies()) != 0 {
			t.Fatal("credentials error disclosed account or session")
		}
	}
	for _, origin := range []string{"", "null", "https://evil.example", "https://referflow.example.evil"} {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(credentials(s.user.Email, password)))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != 403 {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	for _, body := range []string{`{`, credentials(s.user.Email, password) + `{}`, `{"email":"specialist@example.test","password":"invalid","specialist_id":"other"}`} {
		if res := call(h, "POST", "/api/auth/login", body, nil, ""); res.Code != 400 {
			t.Fatal("accepted malformed/extra fields")
		}
	}
	res := call(h, "POST", "/api/auth/login", `{"email":"`+strings.Repeat("a", 5000)+`"}`, nil, "")
	if res.Code != 413 {
		t.Fatal("unbounded request body")
	}
	s.allowed = false
	res = call(h, "POST", "/api/auth/login", credentials(s.user.Email, password), nil, "")
	if res.Code != 429 || res.Header().Get("Retry-After") == "" {
		t.Fatal("login rate limit missing")
	}
}
func TestRejectsAmbiguousCookieAndMissingLogoutOrigin(t *testing.T) {
	h, s, password := fixture(t)
	cookie, session := loginForTest(t, h, s, password)
	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	req.AddCookie(cookie)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatal("duplicate cookie accepted")
	}
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(cookie)
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("logout without Origin accepted")
	}
	if res := call(h, "GET", "/api/auth/session", "", cookie, ""); res.Code != 200 {
		t.Fatal("invalid logout deleted session")
	}
}
