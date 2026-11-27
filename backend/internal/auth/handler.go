package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
)

type Repository interface {
	Account(context.Context, string) (User, string, error)
	AllowLogin(context.Context, string) (bool, error)
	CreateSession(context.Context, string, []byte, []byte, time.Duration) (time.Time, error)
	Session(context.Context, []byte) (Session, error)
	DeleteSession(context.Context, []byte) error
}

type Options struct {
	Origins      []string
	SecureCookie bool
	SessionTTL   time.Duration
}

type Handler struct {
	store     Repository
	options   Options
	dummyHash string
	hashing   chan struct{}
}

func NewHandler(store Repository, options Options) (*Handler, error) {
	// Unknown accounts do the same expensive password check as existing ones.
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, errors.New("cannot initialize authentication")
	}
	dummy, err := HashPassword(base64.RawURLEncoding.EncodeToString(random))
	if err != nil {
		return nil, err
	}
	return &Handler{store: store, options: options, dummyHash: dummy, hashing: make(chan struct{}, 4)}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	switch r.URL.Path {
	case "/api/auth/login":
		if !method(w, r, http.MethodPost) || !h.sameOrigin(w, r) {
			return
		}
		h.login(w, r)
	case "/api/auth/session":
		if !method(w, r, http.MethodGet) {
			return
		}
		session, _, ok := h.requireSession(w, r)
		if ok {
			respond(w, http.StatusOK, session)
		}
	case "/api/auth/logout":
		if !method(w, r, http.MethodPost) || !h.sameOrigin(w, r) {
			return
		}
		h.logout(w, r)
	default:
		status(w, http.StatusNotFound, "not_found")
	}
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		status(w, 415, "unsupported_media_type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&input); err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			status(w, 413, "request_too_large")
		} else {
			status(w, 400, "invalid_request")
		}
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		status(w, 400, "invalid_request")
		return
	}
	email, err := NormalizeEmail(input.Email)
	if err != nil || len(input.Password) == 0 || len(input.Password) > 512 {
		status(w, 400, "invalid_request")
		return
	}
	allowed, err := h.store.AllowLogin(r.Context(), email)
	if err != nil {
		status(w, 503, "unavailable")
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "900")
		status(w, 429, "too_many_attempts")
		return
	}
	select {
	case h.hashing <- struct{}{}:
		defer func() { <-h.hashing }()
	default:
		status(w, 503, "unavailable")
		return
	}
	user, hash, err := h.store.Account(r.Context(), email)
	missing := errors.Is(err, ErrNotFound)
	if err != nil && !missing {
		status(w, 503, "unavailable")
		return
	}
	if missing {
		hash = h.dummyHash
	}
	matches := CheckPassword(hash, input.Password)
	if !matches || missing {
		status(w, 401, "invalid_credentials")
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		status(w, 503, "unavailable")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	var oldHash []byte
	if old := h.token(r); old != "" {
		oldHash = digest(old)
	}
	expires, err := h.store.CreateSession(r.Context(), user.ID, digest(token), oldHash, h.options.SessionTTL)
	if err != nil {
		status(w, 503, "unavailable")
		return
	}
	cookie := h.cookie(token)
	cookie.Expires = expires
	cookie.MaxAge = int(h.options.SessionTTL.Seconds())
	http.SetCookie(w, cookie)
	respond(w, 200, Session{User: user, ExpiresAt: expires, CSRFToken: csrf(token)})
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (Session, string, bool) {
	token := h.token(r)
	if token == "" {
		status(w, 401, "unauthenticated")
		return Session{}, "", false
	}
	session, err := h.store.Session(r.Context(), digest(token))
	if errors.Is(err, ErrNotFound) || (err == nil && !session.ExpiresAt.After(time.Now())) {
		h.clearCookie(w)
		status(w, 401, "unauthenticated")
		return Session{}, "", false
	}
	if err != nil {
		status(w, 503, "unavailable")
		return Session{}, "", false
	}
	session.CSRFToken = csrf(token)
	return session, token, true
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	session, token, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1 {
		status(w, 403, "csrf_failed")
		return
	}
	if err := h.store.DeleteSession(r.Context(), digest(token)); err != nil {
		status(w, 503, "unavailable")
		return
	}
	h.clearCookie(w)
	respond(w, 204, nil)
}

func (h *Handler) token(r *http.Request) string {
	cookies := r.CookiesNamed(h.cookieName())
	if len(cookies) != 1 {
		return ""
	}
	token := cookies[0].Value
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return ""
	}
	return token
}

func (h *Handler) cookieName() string {
	if h.options.SecureCookie {
		return "__Host-referflow_session"
	}
	return "referflow_session"
}

func (h *Handler) cookie(token string) *http.Cookie {
	return &http.Cookie{Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.options.SecureCookie, SameSite: http.SameSiteStrictMode}
}

func (h *Handler) clearCookie(w http.ResponseWriter) {
	cookie := h.cookie("")
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0)
	http.SetCookie(w, cookie)
}

func (h *Handler) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	for _, allowed := range h.options.Origins {
		if origin != "" && origin == allowed {
			return true
		}
	}
	status(w, 403, "csrf_failed")
	return false
}

func digest(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

func csrf(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte("referflow-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func method(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed {
		return true
	}
	w.Header().Set("Allow", allowed)
	status(w, 405, "method_not_allowed")
	return false
}

func status(w http.ResponseWriter, code int, value string) {
	respond(w, code, map[string]string{"status": value})
}

func respond(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Vary", "Cookie, Origin")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}
