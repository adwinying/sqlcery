// Package frecency — adversarial verification tests.
// These are INDEPENDENT checks written by a skeptical reviewer, not the original implementer.
// They probe edge cases the original test suite left uncovered.
package frecency

import (
	"sort"
	"testing"
	"time"
)

// TestDecayOvercomesAccumulatedFrequency verifies that a name recorded many
// times in the distant past loses to a name recorded only once recently.
// This is the canonical zoxide-style correctness check: recency must beat
// frequency when enough half-lives have elapsed.
func TestDecayOvercomesAccumulatedFrequency(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// Record "popular" 20 times in quick succession (score accumulates to ~20).
	for i := 0; i < 20; i++ {
		s.record("popular")
		clk.advance(time.Second)
	}

	// Advance 10 half-lives (10 × 3 weeks = ~210 days).
	// After 10 half-lives a score of 20 decays to 20 / 2^10 ≈ 0.0195.
	clk.advance(10 * halfLife)

	// Record "newcomer" just once, right now → score 1.0.
	s.record("newcomer")

	ordered := s.order([]string{"popular", "newcomer"})
	if ordered[0] != "newcomer" {
		t.Errorf("after 10 half-lives, newcomer (score 1) should outrank popular (score ~0.02); got %v", ordered)
	}
}

// TestOrderDoesNotMutateInputSlice confirms that Order returns a fresh slice
// and leaves the caller's slice in its original order.
func TestOrderDoesNotMutateInputSlice(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	s.record("b")
	s.record("a")

	input := []string{"z", "m", "a", "b"}
	inputCopy := make([]string, len(input))
	copy(inputCopy, input)

	_ = s.order(input)

	for i, v := range inputCopy {
		if input[i] != v {
			t.Errorf("Order mutated input slice: index %d changed from %q to %q", i, v, input[i])
		}
	}
}

// TestOrderEmptySlice confirms that Order on an empty names slice returns an
// empty slice without panicking.
func TestOrderEmptySlice(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	s.record("x") // some state in the scorer

	result := s.order([]string{})
	if result == nil {
		t.Errorf("expected non-nil empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}
}

// TestOrderDuplicateNamesNoPanic confirms that duplicate names in the input
// don't panic and that the output has the same length (each duplicate gets its
// own slot — the spec says nothing about dedup, so we just demand no crash
// and a sane length).
func TestOrderDuplicateNamesNoPanic(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	s.record("alpha")

	input := []string{"alpha", "beta", "alpha", "gamma", "beta"}
	var result []string
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Order panicked on duplicate names: %v", r)
		}
	}()
	result = s.order(input)
	if len(result) != len(input) {
		t.Errorf("expected output length %d, got %d: %v", len(input), len(result), result)
	}
}

// TestDecayAtReadTime_ScoresAreNotStale is the critical correctness probe for
// the "decay at Order time" requirement.
//
// Setup that CANNOT pass via alphabetical tie-break alone:
//   - "zzz-old" recorded twice (stored score ~2.0) at t=0.
//   - Clock advances 3 half-lives → zzz-old's live score = 2 / 2^3 = 0.25.
//   - "aaa-new" recorded once (stored score 1.0) at t=3*halfLife.
//   - Order called immediately → aaa-new live score = 1.0, zzz-old = 0.25.
//   - Correct result: aaa-new first.
//   - Stale-score bug result: zzz-old stored=2.0 > aaa-new stored=1.0 → zzz-old first.
//   - Alphabetical tie-break cannot rescue the bug because scores differ (2 vs 1).
//
// This test conclusively distinguishes live-decay from stale-score implementations.
func TestDecayAtReadTime_ScoresAreNotStale(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// Record "zzz-old" twice at t=0 — accumulated stored score ≈ 2.0.
	s.record("zzz-old")
	s.record("zzz-old")

	// Advance 3 half-lives: zzz-old's live score = 2 × 2^-3 = 0.25.
	clk.advance(3 * halfLife)

	// Record "aaa-new" once now → stored (and live) score = 1.0.
	s.record("aaa-new")

	// Order immediately. With correct live decay: aaa-new(1.0) > zzz-old(0.25).
	// With stale scores: zzz-old(2.0) > aaa-new(1.0) → zzz-old would rank first.
	ordered := s.order([]string{"zzz-old", "aaa-new"})
	if ordered[0] != "aaa-new" {
		t.Errorf("decay-at-read-time FAILED: zzz-old has stored score 2 but live score 0.25 (3 half-lives); aaa-new has live score 1.0 and must rank first. Got: %v", ordered)
	}
}

// TestOrderDoesNotMutateInternalState checks that calling Order multiple times
// with a frozen clock returns identical results — i.e., Order is read-only and
// does not write back decayed scores into the store.
func TestOrderDoesNotMutateInternalState(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	s.record("x")
	clk.advance(halfLife) // score at Order time = 0.5

	names := []string{"x", "y", "z"}

	first := s.order(names)
	second := s.order(names)
	third := s.order(names)

	// All three calls with the same frozen clock must return the same order.
	for i := range first {
		if first[i] != second[i] || second[i] != third[i] {
			t.Errorf("Order is not idempotent (possible internal mutation): %v / %v / %v", first, second, third)
		}
	}
}

// TestSingleNameList ensures Order works for a single-element slice.
func TestSingleNameList(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	result := s.order([]string{"only"})
	if len(result) != 1 || result[0] != "only" {
		t.Errorf("single-name order returned unexpected %v", result)
	}
}

// TestAllNamesUnknown_DeterministicOrder ensures that with zero state and a
// random-ish input the output is always alphabetical (deterministic sort with
// no score info).
func TestAllNamesUnknown_DeterministicOrder(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	names := []string{"zeta", "alpha", "mu", "beta", "delta"}
	result := s.order(names)

	// Build expected: sorted copy.
	expected := make([]string, len(names))
	copy(expected, names)
	sort.Strings(expected)

	for i, v := range expected {
		if result[i] != v {
			t.Errorf("position %d: expected %q got %q (full: %v)", i, v, result[i], result)
		}
	}
}
