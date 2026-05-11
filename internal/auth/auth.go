package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

const (
	CookieName      = "prismcat_session"
	sessionDuration = 30 * 24 * time.Hour

	hashAlgorithm = "pbkdf2_sha256"
	hashIter      = 210000
	hashSaltBytes = 16
	hashKeyBytes  = 32
)

var (
	ErrInvalidPassword  = errors.New("invalid password")
	ErrPasswordRequired = errors.New("password required")
	ErrAlreadySetup     = errors.New("password already configured")
)

type Manager struct {
	cfg *config.Config
	now func() time.Time
}

type Status struct {
	Authenticated    bool   `json:"authenticated"`
	AuthRequired     bool   `json:"auth_required"`
	SetupRequired    bool   `json:"setup_required"`
	SessionExpiresAt string `json:"session_expires_at,omitempty"`
}

type sessionPayload struct {
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"nonce"`
}

func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg: cfg,
		now: time.Now,
	}
}

func (m *Manager) Status(r *http.Request) Status {
	status := Status{
		AuthRequired:  true,
		SetupRequired: !m.hasPassword(),
	}
	if status.SetupRequired {
		return status
	}

	expiresAt, ok := m.authenticate(r)
	if !ok {
		return status
	}
	status.Authenticated = true
	status.SessionExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	return status
}

func (m *Manager) Authenticated(r *http.Request) bool {
	_, ok := m.authenticate(r)
	return ok
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request, password string) (Status, error) {
	if !m.hasPassword() {
		return m.Status(r), ErrPasswordRequired
	}
	if !m.verifyPassword(password) {
		return m.Status(r), ErrInvalidPassword
	}
	expiresAt, err := m.setSessionCookie(w, r)
	if err != nil {
		return m.Status(r), err
	}
	return Status{
		Authenticated:    true,
		AuthRequired:     true,
		SetupRequired:    false,
		SessionExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) Setup(w http.ResponseWriter, r *http.Request, password string) (Status, error) {
	if strings.TrimSpace(password) == "" {
		return m.Status(r), ErrPasswordRequired
	}
	if m.hasPassword() {
		return m.Status(r), ErrAlreadySetup
	}

	hashValue, err := HashPassword(password)
	if err != nil {
		return m.Status(r), err
	}
	m.cfg.SetUIPasswordHash(hashValue)
	if err := m.cfg.Save(); err != nil {
		return m.Status(r), err
	}

	expiresAt, err := m.setSessionCookie(w, r)
	if err != nil {
		return m.Status(r), err
	}
	return Status{
		Authenticated:    true,
		AuthRequired:     true,
		SetupRequired:    false,
		SessionExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
}

func (m *Manager) hasPassword() bool {
	snapshot := m.cfg.AuthSnapshot()
	return snapshot.UIPassword != "" || strings.TrimSpace(snapshot.UIPasswordHash) != ""
}

func (m *Manager) verifyPassword(password string) bool {
	snapshot := m.cfg.AuthSnapshot()
	if snapshot.UIPassword != "" {
		return constantTimeStringEqual(password, snapshot.UIPassword)
	}
	return VerifyPassword(password, snapshot.UIPasswordHash)
}

func (m *Manager) setSessionCookie(w http.ResponseWriter, r *http.Request) (time.Time, error) {
	secret, ok := m.sessionSecret()
	if !ok {
		return time.Time{}, ErrPasswordRequired
	}

	expiresAt := m.now().Add(sessionDuration)
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return time.Time{}, err
	}
	payload := sessionPayload{
		ExpiresAt: expiresAt.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return time.Time{}, err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := sign(encodedPayload, secret)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    encodedPayload + "." + signature,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
	return expiresAt, nil
}

func (m *Manager) authenticate(r *http.Request) (time.Time, bool) {
	secret, ok := m.sessionSecret()
	if !ok {
		return time.Time{}, false
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return time.Time{}, false
	}

	encodedPayload, signature, ok := strings.Cut(cookie.Value, ".")
	if !ok || encodedPayload == "" || signature == "" {
		return time.Time{}, false
	}
	if !verifySignature(encodedPayload, signature, secret) {
		return time.Time{}, false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return time.Time{}, false
	}
	var payload sessionPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return time.Time{}, false
	}
	if payload.ExpiresAt <= m.now().Unix() {
		return time.Time{}, false
	}
	return time.Unix(payload.ExpiresAt, 0), true
}

func (m *Manager) sessionSecret() ([]byte, bool) {
	snapshot := m.cfg.AuthSnapshot()
	material := snapshot.UIPassword
	if material == "" {
		material = strings.TrimSpace(snapshot.UIPasswordHash)
	}
	if material == "" {
		return nil, false
	}
	sum := sha256.Sum256([]byte("prismcat-session-v1:" + material))
	return sum[:], true
}

func HashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", ErrPasswordRequired
	}
	salt := make([]byte, hashSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := deriveKey(password, salt, hashIter)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s",
		hashAlgorithm,
		hashIter,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 4 || parts[0] != hashAlgorithm {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := deriveKey(password, salt, iter)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func deriveKey(password string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(func() hash.Hash { return sha256.New() }, password, salt, iter, hashKeyBytes)
}

func sign(payload string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifySignature(payload, signature string, secret []byte) bool {
	want := sign(payload, secret)
	return subtle.ConstantTimeCompare([]byte(signature), []byte(want)) == 1
}

func constantTimeStringEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type Handler struct {
	manager *Manager
}

func NewHandler(manager *Manager) *Handler {
	return &Handler{manager: manager}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/me", h.handleMe)
	mux.HandleFunc("/api/auth/login", h.handleLogin)
	mux.HandleFunc("/api/auth/logout", h.handleLogout)
	mux.HandleFunc("/api/auth/setup", h.handleSetup)
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonResponse(w, h.manager.Status(r))
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := h.manager.Login(w, r, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidPassword):
			jsonError(w, "invalid password", http.StatusUnauthorized)
		case errors.Is(err, ErrPasswordRequired):
			jsonError(w, "password is not configured", http.StatusConflict)
		default:
			jsonError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	jsonResponse(w, status)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.manager.Logout(w, r)
	jsonResponse(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	status, err := h.manager.Setup(w, r, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadySetup):
			jsonError(w, "password already configured", http.StatusConflict)
		case errors.Is(err, ErrPasswordRequired):
			jsonError(w, "password required", http.StatusBadRequest)
		default:
			jsonError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	jsonResponse(w, status)
}

func RequireSession(manager *Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !manager.Authenticated(r) {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
