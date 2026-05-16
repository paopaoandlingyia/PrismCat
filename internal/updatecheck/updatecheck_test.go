package updatecheck

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "v1.5.1", right: "1.5.0", want: 1},
		{left: "1.5.0", right: "v1.5.0", want: 0},
		{left: "v1.4.9", right: "1.5.0", want: -1},
		{left: "v2.0.0", right: "1.99.99", want: 1},
		{left: "v1.5.0-beta.1", right: "1.5.0", want: 0},
	}

	for _, tt := range tests {
		if got := compareVersions(tt.left, tt.right); got != tt.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}
