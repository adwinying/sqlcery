package app

import "testing"

func TestSQLAssistDecisionUsesLightweightTokenization(t *testing.T) {
	for _, surface := range []sqlAssistSurface{sqlAssistSurfaceAutocomplete, sqlAssistSurfaceStatementExpansion} {
		decision := sqlAssistDecisionFor(surface)
		if decision.Strategy != sqlAssistStrategyLightweightTokenization {
			t.Fatalf("sqlAssistDecisionFor(%q).Strategy = %q, want %q", surface, decision.Strategy, sqlAssistStrategyLightweightTokenization)
		}
		if len(decision.SupportedScopes) == 0 {
			t.Fatalf("sqlAssistDecisionFor(%q).SupportedScopes = empty, want documented boundaries", surface)
		}
		if len(decision.ParserTriggers) == 0 {
			t.Fatalf("sqlAssistDecisionFor(%q).ParserTriggers = empty, want escalation rules", surface)
		}
	}
}

func TestShouldEscalateSQLAssistToParser(t *testing.T) {
	if shouldEscalateSQLAssistToParser(sqlAssistRequirements{}) {
		t.Fatal("shouldEscalateSQLAssistToParser() = true, want false for lightweight use cases")
	}

	tests := []sqlAssistRequirements{
		{NeedsNestedStatementAwareness: true},
		{NeedsAliasResolution: true},
		{NeedsDialectGrammarValidation: true},
		{NeedsASTRewrite: true},
	}

	for _, tc := range tests {
		if !shouldEscalateSQLAssistToParser(tc) {
			t.Fatalf("shouldEscalateSQLAssistToParser(%+v) = false, want true", tc)
		}
	}
}
