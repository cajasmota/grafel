package wiztui

import (
	"strconv"
	"strings"
	"time"
)

// commafy formats n with thousands separators (e.g. 3686488 -> "3,686,488"),
// so large entity/relationship/file counts stay readable on the index screen.
// Negative numbers keep their sign in front of the grouped digits.
func commafy(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	l := len(s)
	var b strings.Builder
	b.Grow(l + l/3 + 1)
	for i, c := range s {
		if i > 0 && (l-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// fmtElapsed formats a duration as a compact "MmSSs" string (e.g. "2m14s",
// "0m08s") for the live index-screen timer. Unlike internal/cli's fmtDuration
// (which omits the minutes segment under a minute), this always shows both
// segments — a stable width reads better in a ticking header. Negative
// durations (e.g. a clock hiccup) clamp to zero rather than ever displaying a
// negative timer.
func fmtElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	total := int(d.Seconds())
	m := total / 60
	s := total % 60
	return strconv.Itoa(m) + "m" + pad2(s) + "s"
}

// pad2 zero-pads n to at least two digits (n is always in [0,60)).
func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
