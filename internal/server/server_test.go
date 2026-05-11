package server

import "testing"

func TestIsPublicAPIPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/health", want: true},
		{path: "/healthz", want: true},
		{path: "/api/auth/me", want: true},
		{path: "/api/auth/login", want: true},
		{path: "/api/logs", want: false},
		{path: "/api/config", want: false},
		{path: "/api/authx", want: false},
	}

	for _, tt := range tests {
		if got := isPublicAPIPath(tt.path); got != tt.want {
			t.Fatalf("isPublicAPIPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsHealthCheckPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/health", want: true},
		{path: "/healthz", want: true},
		{path: "/api/healthz", want: false},
		{path: "/health", want: false},
	}

	for _, tt := range tests {
		if got := isHealthCheckPath(tt.path); got != tt.want {
			t.Fatalf("isHealthCheckPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
