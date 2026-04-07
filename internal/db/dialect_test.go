package db

import "testing"

func TestDialectByName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantName  string
		wantError string
	}{
		{name: "sqlite", input: "sqlite", wantName: "sqlite"},
		{name: "postgres alias", input: "postgresql", wantName: "postgres"},
		{name: "mysql trims spaces", input: " mysql ", wantName: "mysql"},
		{name: "unsupported", input: "oracle", wantError: `unsupported dialect "oracle"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialect, err := DialectByName(tt.input)
			if tt.wantError != "" {
				if err == nil {
					t.Fatal("DialectByName() error = nil, want error")
				}

				if got := err.Error(); got != tt.wantError {
					t.Fatalf("DialectByName() error = %q, want %q", got, tt.wantError)
				}

				return
			}

			if err != nil {
				t.Fatalf("DialectByName() error = %v", err)
			}

			if got := dialect.Name(); got != tt.wantName {
				t.Fatalf("dialect.Name() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestDialectBehavior(t *testing.T) {
	tests := []struct {
		name             string
		dialect          Dialect
		placeholderIndex int
		wantPlaceholder  string
		parts            []string
		wantIdentifier   string
	}{
		{
			name:             "sqlite",
			dialect:          SQLiteDialect(),
			placeholderIndex: 3,
			wantPlaceholder:  "?",
			parts:            []string{"main", `order"items`},
			wantIdentifier:   `"main"."order""items"`,
		},
		{
			name:             "postgres",
			dialect:          PostgresDialect(),
			placeholderIndex: 2,
			wantPlaceholder:  "$2",
			parts:            []string{"public", "widgets"},
			wantIdentifier:   `"public"."widgets"`,
		},
		{
			name:             "mysql",
			dialect:          MySQLDialect(),
			placeholderIndex: 8,
			wantPlaceholder:  "?",
			parts:            []string{"analytics", "order`items"},
			wantIdentifier:   "`analytics`.`order``items`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dialect.Placeholder(tt.placeholderIndex); got != tt.wantPlaceholder {
				t.Fatalf("Placeholder(%d) = %q, want %q", tt.placeholderIndex, got, tt.wantPlaceholder)
			}

			if got := tt.dialect.QuoteIdentifier(tt.parts...); got != tt.wantIdentifier {
				t.Fatalf("QuoteIdentifier() = %q, want %q", got, tt.wantIdentifier)
			}
		})
	}
}
