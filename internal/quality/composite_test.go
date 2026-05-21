package quality

import (
	"testing"
)

func TestCompositeScoreFromPcts(t *testing.T) {
	tests := []struct {
		name          string
		orphanPct     float64
		bugPct        float64
		recallMissPct float64
		wantScore     float64
		wantGrade     string
	}{
		{
			name:          "perfect graph",
			orphanPct:     0,
			bugPct:        0,
			recallMissPct: 0,
			wantScore:     100.0,
			wantGrade:     "A",
		},
		{
			name:          "grade A boundary (90)",
			orphanPct:     0,
			bugPct:        20,
			recallMissPct: 0,
			// 100 - (0*0.3 + 20*0.5 + 0*0.2) = 90
			wantScore: 90.0,
			wantGrade: "A",
		},
		{
			name:          "grade B",
			orphanPct:     10,
			bugPct:        20,
			recallMissPct: 10,
			// 100 - (3 + 10 + 2) = 85
			wantScore: 85.0,
			wantGrade: "B",
		},
		{
			name:          "grade C",
			orphanPct:     20,
			bugPct:        30,
			recallMissPct: 20,
			// 100 - (6 + 15 + 4) = 75 -> just at B boundary
			wantScore: 75.0,
			wantGrade: "B",
		},
		{
			name:          "grade D",
			orphanPct:     30,
			bugPct:        60,
			recallMissPct: 30,
			// 100 - (9 + 30 + 6) = 55 → D (< 60)
			wantScore: 55.0,
			wantGrade: "D",
		},
		{
			name:          "grade F — terrible graph",
			orphanPct:     80,
			bugPct:        100,
			recallMissPct: 100,
			// 100 - (24 + 50 + 20) = 6
			wantScore: 6.0,
			wantGrade: "F",
		},
		{
			name:          "clamped below zero",
			orphanPct:     100,
			bugPct:        100,
			recallMissPct: 100,
			// 100 - (30 + 50 + 20) = 0
			wantScore: 0.0,
			wantGrade: "F",
		},
		{
			name:          "inputs clamped at 100",
			orphanPct:     200,
			bugPct:        200,
			recallMissPct: 200,
			wantScore:     0.0,
			wantGrade:     "F",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CompositeScoreFromPcts(tc.orphanPct, tc.bugPct, tc.recallMissPct)
			if got.Score != tc.wantScore {
				t.Errorf("Score: got %.1f, want %.1f", got.Score, tc.wantScore)
			}
			if got.Grade != tc.wantGrade {
				t.Errorf("Grade: got %q, want %q", got.Grade, tc.wantGrade)
			}
		})
	}
}

func TestCompositeScoreFromRatios(t *testing.T) {
	// Ratios should produce the same result as equivalent pcts.
	r := CompositeScoreFromRatios(0.10, 0.20, 0.10)
	p := CompositeScoreFromPcts(10, 20, 10)
	if r.Score != p.Score {
		t.Errorf("ratio vs pct mismatch: %.1f vs %.1f", r.Score, p.Score)
	}
}

func TestScoreGrade(t *testing.T) {
	cases := []struct {
		score float64
		grade string
	}{
		{100, "A"},
		{90, "A"},
		{89.9, "B"},
		{75, "B"},
		{74.9, "C"},
		{60, "C"},
		{59.9, "D"},
		{45, "D"},
		{44.9, "F"},
		{0, "F"},
	}
	for _, c := range cases {
		got := scoreGrade(c.score)
		if got != c.grade {
			t.Errorf("scoreGrade(%.1f) = %q, want %q", c.score, got, c.grade)
		}
	}
}
