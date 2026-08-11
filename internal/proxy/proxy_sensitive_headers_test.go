package proxy

import (
	"reflect"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/requestoverride"
)

func TestSanitizeHeaderChangesMasksSensitiveValuesWithoutMutatingInput(t *testing.T) {
	changes := []requestoverride.HeaderChange{
		{
			Op:       "set",
			Name:     "authorization",
			Value:    "Bearer replacement-secret",
			OldValue: "Bearer original-token",
		},
		{
			Op:       "set",
			Name:     "X-Custom",
			Value:    "visible-value",
			OldValue: "old-visible-value",
		},
		{
			Op:       "remove",
			Name:     "X-API-Key",
			OldValue: "short-key",
		},
	}
	original := append([]requestoverride.HeaderChange(nil), changes...)

	got := sanitizeHeaderChanges(changes, []string{"Authorization", "x-api-key"})

	want := []requestoverride.HeaderChange{
		{
			Op:       "set",
			Name:     "authorization",
			Value:    "Beare***ret",
			OldValue: "Beare***ken",
		},
		{
			Op:       "set",
			Name:     "X-Custom",
			Value:    "visible-value",
			OldValue: "old-visible-value",
		},
		{
			Op:       "remove",
			Name:     "X-API-Key",
			OldValue: "***",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized changes = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(changes, original) {
		t.Fatalf("input changes mutated: got %#v, want %#v", changes, original)
	}
}

func TestMaskSensitiveHeaderValueHandlesShortAndEmptyValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "short", value: "secret", want: "***"},
		{name: "empty", value: "", want: "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskSensitiveHeaderValue(tt.value); got != tt.want {
				t.Fatalf("maskSensitiveHeaderValue(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
