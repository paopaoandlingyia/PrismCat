package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestHashPasswordVerifiesOnlyCorrectPassword(t *testing.T) {
	hashValue, err := HashPassword("secret-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if !VerifyPassword("secret-password", hashValue) {
		t.Fatal("VerifyPassword rejected the correct password")
	}
	if VerifyPassword("wrong-password", hashValue) {
		t.Fatal("VerifyPassword accepted the wrong password")
	}
}

func TestLoginCreatesSessionCookie(t *testing.T) {
	hashValue, err := HashPassword("secret-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	manager := NewManager(&config.Config{
		Server: config.ServerConfig{
			UIPasswordHash: hashValue,
		},
	})
	manager.now = func() time.Time {
		return time.Unix(1000, 0)
	}

	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/auth/login", nil)
	rec := httptest.NewRecorder()
	status, err := manager.Login(rec, req, "secret-password")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if !status.Authenticated || status.SetupRequired {
		t.Fatalf("status = %#v", status)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName {
		t.Fatalf("cookies = %#v", cookies)
	}
	reqWithCookie := httptest.NewRequest(http.MethodGet, "http://localhost/api/logs", nil)
	reqWithCookie.AddCookie(cookies[0])
	if !manager.Authenticated(reqWithCookie) {
		t.Fatal("session cookie was not accepted")
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	manager := NewManager(&config.Config{
		Server: config.ServerConfig{
			UIPassword: "secret-password",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/auth/login", nil)
	rec := httptest.NewRecorder()
	if _, err := manager.Login(rec, req, "wrong-password"); err != ErrInvalidPassword {
		t.Fatalf("Login error = %v, want ErrInvalidPassword", err)
	}
}

func TestPlainUIPasswordTakesPrecedenceOverHash(t *testing.T) {
	hashValue, err := HashPassword("hashed-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	manager := NewManager(&config.Config{
		Server: config.ServerConfig{
			UIPassword:     "plain-password",
			UIPasswordHash: hashValue,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/auth/login", nil)
	rec := httptest.NewRecorder()
	if _, err := manager.Login(rec, req, "hashed-password"); err != ErrInvalidPassword {
		t.Fatalf("Login with hash password error = %v, want ErrInvalidPassword", err)
	}
	if _, err := manager.Login(rec, req, "plain-password"); err != nil {
		t.Fatalf("Login with plain password returned error: %v", err)
	}
}

func TestSetupStoresPasswordHash(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	manager := NewManager(cfg)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/auth/setup", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	status, err := manager.Setup(rec, req, "new-password")
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if !status.Authenticated || status.SetupRequired {
		t.Fatalf("status = %#v", status)
	}

	snapshot := cfg.AuthSnapshot()
	if snapshot.UIPassword != "" {
		t.Fatalf("plain password was stored: %q", snapshot.UIPassword)
	}
	if snapshot.UIPasswordHash == "" {
		t.Fatal("password hash was not stored")
	}
	if !VerifyPassword("new-password", snapshot.UIPasswordHash) {
		t.Fatal("stored password hash does not verify")
	}
}
