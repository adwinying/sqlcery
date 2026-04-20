package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tilde only",
			input: "~",
			want:  home,
		},
		{
			name:  "tilde with path",
			input: "~/Downloads/app.db",
			want:  filepath.Join(home, "Downloads/app.db"),
		},
		{
			name:  "absolute path unchanged",
			input: "/tmp/app.db",
			want:  "/tmp/app.db",
		},
		{
			name:  "relative path unchanged",
			input: "app.db",
			want:  "app.db",
		},
		{
			name:  "tilde in middle unchanged",
			input: "/path/to/~/app.db",
			want:  "/path/to/~/app.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandHomePath(tt.input)
			if err != nil {
				t.Fatalf("expandHomePath(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("expandHomePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
