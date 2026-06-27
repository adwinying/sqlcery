package frecency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeClock is a controllable clock for deterministic tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// baseTime is an arbitrary fixed starting point used across tests.
var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// ── Scoring tests (pure scorer, no filesystem) ────────────────────────────

func TestFrequentNameRanksHigher(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// Open "alpha" three times in quick succession.
	s.record("alpha")
	clk.advance(time.Minute)
	s.record("alpha")
	clk.advance(time.Minute)
	s.record("alpha")

	// Open "beta" only once.
	clk.advance(time.Minute)
	s.record("beta")

	ordered := s.order([]string{"beta", "alpha"})
	if ordered[0] != "alpha" {
		t.Errorf("expected alpha first (higher frequency), got %v", ordered)
	}
}

func TestRecencyDecay_OldNameRanksBeforeRecent(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// Record "old" many times a long time ago.
	for i := 0; i < 10; i++ {
		s.record("old")
		clk.advance(time.Minute)
	}

	// Advance the clock by several months so "old"'s score decays heavily.
	clk.advance(180 * 24 * time.Hour)

	// Record "recent" just once, right now.
	s.record("recent")

	ordered := s.order([]string{"old", "recent"})
	if ordered[0] != "recent" {
		t.Errorf("expected recent to outrank old after heavy decay, got %v", ordered)
	}
}

func TestUnknownNamesScoreZero_SortAlphabetically(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// No records at all — all three names are unknown.
	ordered := s.order([]string{"zulu", "alpha", "mike"})
	want := []string{"alpha", "mike", "zulu"}
	for i, name := range want {
		if ordered[i] != name {
			t.Errorf("position %d: want %q got %q (full: %v)", i, name, ordered[i], ordered)
		}
	}
}

func TestTieBreakAlphabetical(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// Record each name exactly once at the same instant — identical scores.
	names := []string{"zulu", "bravo", "alpha", "mike"}
	for _, n := range names {
		s.record(n)
	}

	// All opened at the same time → identical decayed scores → alphabetical order.
	ordered := s.order(names)
	want := []string{"alpha", "bravo", "mike", "zulu"}
	for i, name := range want {
		if ordered[i] != name {
			t.Errorf("position %d: want %q got %q (full: %v)", i, name, ordered[i], ordered)
		}
	}
}

func TestOrderIsStable_RepeatedCalls(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	s.record("b")
	s.record("a")

	first := s.order([]string{"a", "b", "c"})
	second := s.order([]string{"c", "b", "a"})

	for i := range first {
		if first[i] != second[i] {
			t.Errorf("order not stable: first=%v second=%v", first, second)
		}
	}
}

func TestMixedKnownUnknown(t *testing.T) {
	clk := &fakeClock{t: baseTime}
	s := newScorer(clk.now)

	// Record "known" multiple times.
	s.record("known")
	clk.advance(time.Second)
	s.record("known")

	// "unknown-a" and "unknown-b" have never been recorded.
	ordered := s.order([]string{"unknown-b", "unknown-a", "known"})
	if ordered[0] != "known" {
		t.Errorf("expected known first, got %v", ordered)
	}
	// The two unknowns should be alphabetical.
	if ordered[1] != "unknown-a" || ordered[2] != "unknown-b" {
		t.Errorf("expected unknown-a before unknown-b, got %v", ordered)
	}
}

// ── Persistence tests (Store, uses temp filesystem) ───────────────────────

func tempPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "sqlcery", "frecency.json")
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := tempPath(t)
	clk := &fakeClock{t: baseTime}

	// Create a store, record some opens, then reload from the same path.
	store1, err := Load(path, clk.now)
	if err != nil {
		t.Fatalf("Load (first): %v", err)
	}

	if err := store1.RecordOpen("alpha"); err != nil {
		t.Fatalf("RecordOpen alpha: %v", err)
	}
	clk.advance(time.Minute)
	if err := store1.RecordOpen("alpha"); err != nil {
		t.Fatalf("RecordOpen alpha 2: %v", err)
	}
	clk.advance(time.Minute)
	if err := store1.RecordOpen("beta"); err != nil {
		t.Fatalf("RecordOpen beta: %v", err)
	}

	// Reload from disk — use the same clock so scores are comparable.
	store2, err := Load(path, clk.now)
	if err != nil {
		t.Fatalf("Load (second): %v", err)
	}

	ordered := store2.Order([]string{"beta", "alpha"})
	if ordered[0] != "alpha" {
		t.Errorf("after reload, expected alpha first (higher score), got %v", ordered)
	}
}

func TestMissingFileLoadsEmpty(t *testing.T) {
	path := tempPath(t) // file does not yet exist

	clk := &fakeClock{t: baseTime}
	store, err := Load(path, clk.now)
	if err != nil {
		t.Fatalf("Load on missing file should not error, got: %v", err)
	}

	// With an empty store, all names are unknown → alphabetical.
	ordered := store.Order([]string{"z", "a", "m"})
	want := []string{"a", "m", "z"}
	for i, name := range want {
		if ordered[i] != name {
			t.Errorf("position %d: want %q got %q", i, name, ordered[i])
		}
	}
}

func TestCorruptFileLoadsEmpty(t *testing.T) {
	path := tempPath(t)

	// Write garbage to the file.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("this is not json }{{{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	clk := &fakeClock{t: baseTime}
	store, err := Load(path, clk.now)
	if err != nil {
		t.Fatalf("Load on corrupt file should not error, got: %v", err)
	}

	// Corrupt → empty store → alphabetical.
	ordered := store.Order([]string{"z", "a"})
	if ordered[0] != "a" {
		t.Errorf("expected alphabetical order from empty/corrupt store, got %v", ordered)
	}
}

func TestRecordOpenPersistsEachTime(t *testing.T) {
	path := tempPath(t)
	clk := &fakeClock{t: baseTime}

	s, err := Load(path, clk.now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Each RecordOpen should create/update the file.
	for i := 0; i < 3; i++ {
		if err := s.RecordOpen("conn"); err != nil {
			t.Fatalf("RecordOpen %d: %v", i, err)
		}
		clk.advance(time.Minute)
	}

	// Verify the file exists and is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after RecordOpen: %v", err)
	}
	var entries map[string]entry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("file contains invalid JSON: %v", err)
	}
	if _, ok := entries["conn"]; !ok {
		t.Errorf("expected 'conn' key in persisted file, got keys: %v", mapKeys(entries))
	}
}

func mapKeys[K comparable, V any](m map[K]V) []K {
	ks := make([]K, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
