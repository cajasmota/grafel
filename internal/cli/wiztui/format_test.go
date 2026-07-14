package wiztui

import (
	"testing"
	"time"
)

// TestCommafy covers the small table the coordinator asked for: 0, 999, 1000,
// a large real-world relationship count, and the negative-sign guard.
func TestCommafy(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{3686488, "3,686,488"},
		{-3686488, "-3,686,488"},
		{-1, "-1"},
		{-999, "-999"},
	}
	for _, c := range cases {
		if got := commafy(c.in); got != c.want {
			t.Errorf("commafy(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFmtElapsed covers the compact "MmSSs" duration format used by the live
// index-screen timer.
func TestFmtElapsed(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0m00s"},
		{8 * time.Second, "0m08s"},
		{2*time.Minute + 14*time.Second, "2m14s"},
		{-5 * time.Second, "0m00s"}, // negative guard: never a negative timer
	}
	for _, c := range cases {
		if got := fmtElapsed(c.in); got != c.want {
			t.Errorf("fmtElapsed(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
