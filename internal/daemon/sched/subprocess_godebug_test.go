package sched

import (
	"reflect"
	"testing"
)

// TestWithMadvDontNeed pins the child-env GODEBUG merge (#5954): madvdontneed=1
// is appended to any inherited GODEBUG rather than clobbering it, and exactly
// one GODEBUG entry survives (the Go runtime reads the FIRST match, so a
// duplicate key would be a silent no-op).
func TestWithMadvDontNeed(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "no inherited GODEBUG",
			in:   []string{"PATH=/bin", "GOMAXPROCS=3"},
			want: []string{"PATH=/bin", "GOMAXPROCS=3", "GODEBUG=madvdontneed=1"},
		},
		{
			name: "inherited GODEBUG is preserved and extended",
			in:   []string{"GODEBUG=http2debug=1", "PATH=/bin"},
			want: []string{"PATH=/bin", "GODEBUG=http2debug=1,madvdontneed=1"},
		},
		{
			name: "already set is not duplicated",
			in:   []string{"GODEBUG=madvdontneed=1", "PATH=/bin"},
			want: []string{"PATH=/bin", "GODEBUG=madvdontneed=1"},
		},
		{
			name: "operator opt-out is respected",
			in:   []string{"GODEBUG=madvdontneed=0", "PATH=/bin"},
			want: []string{"PATH=/bin", "GODEBUG=madvdontneed=0"},
		},
		{
			name: "empty inherited GODEBUG",
			in:   []string{"GODEBUG=", "PATH=/bin"},
			want: []string{"PATH=/bin", "GODEBUG=madvdontneed=1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withMadvDontNeed(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("withMadvDontNeed(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
