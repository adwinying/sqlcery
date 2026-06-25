// Package frecency provides a persistence and scoring engine for ranking named
// database connections by frequency + recency of opens (zoxide-style decay).
package frecency

import (
	"math"
	"sort"
	"time"
)

// halfLife is the tunable decay constant. A connection's score halves every
// halfLife of inactivity. Adjust this constant to tune recency sensitivity.
const halfLife = 21 * 24 * time.Hour // 3 weeks

// entry holds the raw (un-decayed) accumulated score for one connection name
// and the timestamp of the last recorded open.
type entry struct {
	Score      float64   `json:"score"`
	LastAccess time.Time `json:"last_access"`
}

// decayedScore returns the entry's score decayed to the given point in time.
func (e entry) decayedScore(now time.Time) float64 {
	elapsed := now.Sub(e.LastAccess)
	return e.Score * math.Pow(2, -float64(elapsed)/float64(halfLife))
}

// scorer holds the in-memory frecency state and the injected clock.
// It is the pure, filesystem-free layer that the Store wraps.
type scorer struct {
	entries map[string]entry
	now     func() time.Time
}

func newScorer(now func() time.Time) *scorer {
	return &scorer{
		entries: make(map[string]entry),
		now:     now,
	}
}

// record decays the existing score for name and then increments it by 1.
func (s *scorer) record(name string) {
	now := s.now()
	e := s.entries[name]

	// Decay the accumulated score to now, then add 1 for the new open.
	e.Score = e.decayedScore(now) + 1.0
	e.LastAccess = now

	s.entries[name] = e
}

// order returns names sorted by current decayed frecency (descending).
// Names absent from the store score 0. Ties (including all-zero) break
// alphabetically so ordering is deterministic.
func (s *scorer) order(names []string) []string {
	now := s.now()

	out := make([]string, len(names))
	copy(out, names)

	sort.SliceStable(out, func(i, j int) bool {
		si := s.entries[out[i]].decayedScore(now)
		sj := s.entries[out[j]].decayedScore(now)
		if si != sj {
			return si > sj // higher score first
		}
		return out[i] < out[j] // alphabetical tiebreak
	})

	return out
}
