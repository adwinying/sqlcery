package app

// sqlAssistSurface identifies the SQL-aware editor feature making the decision.
type sqlAssistSurface string

const (
	sqlAssistSurfaceAutocomplete       sqlAssistSurface = "autocomplete"
	sqlAssistSurfaceStatementExpansion sqlAssistSurface = "statement-expansion"
)

// sqlAssistAnalysisStrategy captures whether SQL-aware helpers rely on a full
// SQL parser or on lighter token scanning.
type sqlAssistAnalysisStrategy string

const (
	sqlAssistStrategyLightweightTokenization sqlAssistAnalysisStrategy = "lightweight-tokenization"
	sqlAssistStrategyFullParser              sqlAssistAnalysisStrategy = "full-parser"
)

// sqlAssistDecision documents the repo's chosen analysis strategy for SQL-aware
// editing helpers that need to stay close to the command mode implementation.
type sqlAssistDecision struct {
	Surface         sqlAssistSurface
	Strategy        sqlAssistAnalysisStrategy
	Rationale       string
	SupportedScopes []string
	ParserTriggers  []string
}

// sqlAssistRequirements describes behavior that would exceed the current
// lightweight tokenizer approach and should trigger a parser evaluation.
type sqlAssistRequirements struct {
	NeedsNestedStatementAwareness bool
	NeedsAliasResolution          bool
	NeedsDialectGrammarValidation bool
	NeedsASTRewrite               bool
}

func sqlAssistDecisionFor(surface sqlAssistSurface) sqlAssistDecision {
	return sqlAssistDecision{
		Surface:  surface,
		Strategy: sqlAssistStrategyLightweightTokenization,
		Rationale: "Keep SQL assistance close to command-mode behavior by scanning only the current statement and clause. " +
			"Avoid parser dependencies until a feature needs AST-level understanding.",
		SupportedScopes: []string{
			"Cursor-local completion for keywords, tables, columns, qualifiers, and slash commands.",
			"Statement Expansion driven from schema metadata and explicit user choices instead of rewriting arbitrary SQL text.",
			"Fast statement-boundary detection that ignores literals and comments.",
		},
		ParserTriggers: []string{
			"Nested subqueries, CTEs, or set operations must meaningfully affect completion or composition output.",
			"Table aliases or derived tables must be resolved instead of using direct table names only.",
			"Dialect-specific grammar validation is required before offering or composing SQL.",
			"Existing SQL must be parsed and rewritten while preserving semantics.",
		},
	}
}

func shouldEscalateSQLAssistToParser(requirements sqlAssistRequirements) bool {
	return requirements.NeedsNestedStatementAwareness ||
		requirements.NeedsAliasResolution ||
		requirements.NeedsDialectGrammarValidation ||
		requirements.NeedsASTRewrite
}
