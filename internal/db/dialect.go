package db

import (
	"fmt"
	"strings"
)

type Dialect interface {
	Name() string
	Placeholder(index int) string
	QuoteIdentifier(parts ...string) string
	ValueLiteral(value ResultValue) string
}

func DialectByName(name string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sqlite":
		return SQLiteDialect(), nil
	case "postgres", "postgresql":
		return PostgresDialect(), nil
	case "mysql":
		return MySQLDialect(), nil
	default:
		return nil, fmt.Errorf("unsupported dialect %q", name)
	}
}

func SQLiteDialect() Dialect {
	return dialect{
		name:            "sqlite",
		identifierQuote: `"`,
		placeholder: func(int) string {
			return "?"
		},
	}
}

func PostgresDialect() Dialect {
	return dialect{
		name:            "postgres",
		identifierQuote: `"`,
		placeholder: func(index int) string {
			if index < 1 {
				index = 1
			}
			return fmt.Sprintf("$%d", index)
		},
	}
}

func MySQLDialect() Dialect {
	return dialect{
		name:            "mysql",
		identifierQuote: "`",
		placeholder: func(int) string {
			return "?"
		},
	}
}

type dialect struct {
	name            string
	identifierQuote string
	placeholder     func(index int) string
}

func (d dialect) Name() string {
	return d.name
}

func (d dialect) Placeholder(index int) string {
	return d.placeholder(index)
}

func (d dialect) ValueLiteral(value ResultValue) string {
	return renderValueLiteral(d.name, value)
}

func (d dialect) QuoteIdentifier(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}

		escaped := strings.ReplaceAll(part, d.identifierQuote, d.identifierQuote+d.identifierQuote)
		quoted = append(quoted, d.identifierQuote+escaped+d.identifierQuote)
	}

	return strings.Join(quoted, ".")
}
