package db

import "testing"

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
