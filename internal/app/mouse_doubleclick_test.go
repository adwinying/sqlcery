package app

import (
	"testing"
	"time"
)

func TestIsDoubleClick(t *testing.T) {
	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		prevRow  int
		prevTime time.Time
		newRow   int
		newTime  time.Time
		want     bool
	}{
		{
			name:     "same row within window",
			prevRow:  3,
			prevTime: base,
			newRow:   3,
			newTime:  base.Add(300 * time.Millisecond),
			want:     true,
		},
		{
			name:     "same row at exactly the window boundary",
			prevRow:  3,
			prevTime: base,
			newRow:   3,
			newTime:  base.Add(doubleClickWindow),
			want:     true,
		},
		{
			name:     "same row but too slow",
			prevRow:  3,
			prevTime: base,
			newRow:   3,
			newTime:  base.Add(doubleClickWindow + time.Millisecond),
			want:     false,
		},
		{
			name:     "different row within window",
			prevRow:  2,
			prevTime: base,
			newRow:   3,
			newTime:  base.Add(100 * time.Millisecond),
			want:     false,
		},
		{
			name:     "zero prevTime returns false",
			prevRow:  3,
			prevTime: time.Time{},
			newRow:   3,
			newTime:  base,
			want:     false,
		},
		{
			name:     "different row and too slow",
			prevRow:  1,
			prevTime: base,
			newRow:   5,
			newTime:  base.Add(time.Second),
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isDoubleClick(tc.prevRow, tc.prevTime, tc.newRow, tc.newTime)
			if got != tc.want {
				t.Errorf("isDoubleClick(prevRow=%d, prevTime=%v, newRow=%d, newTime=%v) = %v, want %v",
					tc.prevRow, tc.prevTime, tc.newRow, tc.newTime, got, tc.want)
			}
		})
	}
}
