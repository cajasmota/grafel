package daemon

import (
	"errors"
	"testing"
	"time"
)

// acceptloop_retry_in_parse_test.go grades parseAnnouncedRetryIns — the single
// place an announced `retry_in=<d>` becomes a time.Duration for every backoff
// pin in this package (the factor pins in acceptloop_backoff_factor_test.go and
// the clamp pins in acceptloop_backoff_clamp_test.go both reach it through
// announcedAcceptBackoffs).
//
// WHY A HARNESS HELPER IS WORTH PINNING (#7183). Every backoff assertion in
// this package is an UPPER bound — "every announced wait is <=
// acceptBackoffMax". A parser that loses a value, or loses its sign, turns a
// value that should blow such a bound into one that clears it, and the result
// is a green test over broken behaviour. The old class was
// `retry_in=([0-9a-zA-Z.]+)`, which admits neither `-` nor `µ`:
//
//   - `retry_in=-5ms`  -> the class cannot start on `-`, so the literal
//     `retry_in=` never completes a match: the announcement was DROPPED from
//     the returned slice entirely, silently. (Measured on b31fe2c67: 0 matches,
//     not — as #7183's body states — a match on the positive magnitude. The
//     magnitude reading would also have been wrong; the drop is what the code
//     actually did.) Nothing in the parser noticed. The only thing that caught
//     it downstream was announcedAcceptBackoffs' `len(waits) != n` count
//     control, one assertion away, which the parser has no right to rely on.
//   - `retry_in=5µs` -> the class matched the leading `5` and stopped at `µ`,
//     capturing a TRUNCATED token. That one was fail-loud by luck:
//     time.ParseDuration("5") fails with "missing unit", so the helper
//     t.Fatalf'd. #7182's author hit exactly that while scoring a `5ms -> 5µs`
//     mutant and correctly discarded the row as a harness artefact.
//
// THE FIX AND ITS DIRECTION. The class is replaced by `\S+`: the whole
// whitespace-delimited token is captured verbatim and time.ParseDuration is the
// sole arbiter of whether it is a duration. That removes truncation and
// dropping as failure modes — a token this harness cannot understand now
// reaches ParseDuration whole and fails LOUDLY rather than being silently
// shortened into a different value. On top of that the parser rejects any
// non-positive wait: a backoff of zero or less is a bug being announced, and
// every bound in this package would clear it rather than blow.
//
// The positivity rule is deliberately NOT a restatement of the clamp. The pins
// grade the UPPER bound (<= acceptBackoffMax) and the growth ratio; this is a
// LOWER sanity bound (> 0) that no test in this package asserts. So the
// harness's soundness does not rest on the production behaviour it is grading —
// which was #7183's actual complaint: the reason a negative wait is
// unreachable today is the clamp, and the clamp is the very thing these tests
// mutate away.
//
// THE µ DECISION, STATED OUTRIGHT. `5µs` is now PARSED, at its exact value, not
// rejected. The property #7183 asks to preserve is that an announcement the
// harness cannot represent faithfully fails loudly rather than being silently
// mis-valued; faithful parsing satisfies that property more strongly than the
// old truncate-then-luckily-fail path did. The row below therefore asserts the
// exact value 5µs — which fails both against the old truncation (an error) and
// against any widening that mis-values it.

// retryInParseWant classifies the outcome a row expects.
type retryInParseWant int

const (
	// wantOK: the row parses, and wantWaits is the exact sequence.
	wantOK retryInParseWant = iota
	// wantUnparseable: the token is not a Go duration at all, and the parser
	// must say so — NOT a non-positive rejection, which would mean it had
	// managed to read a number out of it.
	wantUnparseable
	// wantNonPositive: the token IS a duration but is <= 0, and the parser
	// must reject it carrying the value it read, sign intact.
	wantNonPositive
)

func TestParseAnnouncedRetryIns(t *testing.T) {
	cases := []struct {
		name string
		logs string
		want retryInParseWant
		// waits is checked only for wantOK.
		waits []time.Duration
		// parsed is checked only for wantNonPositive: the SIGNED value the
		// parser read out of the token. This is the artefact that separates
		// "correctly signed then rejected" from "read as its magnitude".
		parsed time.Duration
	}{
		{
			name:  "a single positive announcement is read at its exact value",
			logs:  `level=WARN msg="accept: transient error, backing off" retry_in=5ms err=EMFILE`,
			want:  wantOK,
			waits: []time.Duration{5 * time.Millisecond},
		},
		{
			name: "several announcements are returned in emission order",
			logs: "retry_in=5ms\nretry_in=10ms\nretry_in=20ms\n",
			want: wantOK,
			waits: []time.Duration{
				5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond,
			},
		},
		{
			name:  "a log stream with no announcement yields no waits and no error",
			logs:  "level=INFO msg=\"accept: listener closed\"\n",
			want:  wantOK,
			waits: nil,
		},
		{
			name:  "a composite duration is not truncated at its first unit",
			logs:  `retry_in=1h2m3s`,
			want:  wantOK,
			waits: []time.Duration{time.Hour + 2*time.Minute + 3*time.Second},
		},
		{
			// THE µ ROW. Under the old class this captured "5" and failed;
			// asserting the exact value pins that it is neither truncated to a
			// unitless 5 nor mis-valued as 5ns.
			name:  "a microsecond announcement is carried at its full value, not truncated at the micro sign",
			logs:  `retry_in=5µs`,
			want:  wantOK,
			waits: []time.Duration{5 * time.Microsecond},
		},
		{
			// NEWLY ADMITTED BY `\S+`, AND THEREFORE GRADED HERE. The widening
			// admits exactly two classes the old char class rejected: a leading
			// sign, and a micro sign. Both are correctly valued, and an accept
			// that nothing pins is the accept-direction hole the eight
			// forbidden rows below cannot see. Under the old class this input
			// produced 0 matches, so the row is also an extra killer for a
			// revert of the regex arm.
			name:  "a leading plus sign is accepted and valued, not rejected and not stripped",
			logs:  `retry_in=+5ms`,
			want:  wantOK,
			waits: []time.Duration{5 * time.Millisecond},
		},
		{
			// The OTHER micro sign. time.ParseDuration accepts U+03BC GREEK
			// SMALL LETTER MU as well as U+00B5 MICRO SIGN, so both are new
			// accepts under `\S+` and both are pinned to the same exact value.
			// Testing only U+00B5 would leave half the admitted alphabet
			// ungraded.
			name:  "the greek-mu spelling of microseconds is carried at the same exact value as the micro sign",
			logs:  "retry_in=5\u03bcs",
			want:  wantOK,
			waits: []time.Duration{5 * time.Microsecond},
		},
		{
			// THE MUST-HAVE ROW (#7183). Under the old class this returned an
			// empty slice and NO error: the announcement vanished.
			name:   "a negative announcement is read with its sign and rejected, not read as its magnitude",
			logs:   `retry_in=-5ms`,
			want:   wantNonPositive,
			parsed: -5 * time.Millisecond,
		},
		{
			// The drop is easiest to see when it is not the only value: the old
			// parser returned [5ms 20ms] here, no error, one wait short and
			// with the offending value gone.
			name:   "a negative announcement between two positive ones is not silently dropped",
			logs:   "retry_in=5ms\nretry_in=-5ms\nretry_in=20ms\n",
			want:   wantNonPositive,
			parsed: -5 * time.Millisecond,
		},
		{
			name:   "a negative microsecond announcement is rejected with its sign, exercising both holes at once",
			logs:   `retry_in=-5µs`,
			want:   wantNonPositive,
			parsed: -5 * time.Microsecond,
		},
		{
			name:   "a zero announcement is rejected: a backoff of zero is not a wait",
			logs:   `retry_in=0s`,
			want:   wantNonPositive,
			parsed: 0,
		},
		// ---- the widening direction: things that are NOT durations must not
		// become waits. Broadening a character class is where this repo's
		// serious defects come from, so each of these is a forbidden row.
		{
			name: "a bare number with no unit is not a duration",
			logs: `retry_in=5`,
			want: wantUnparseable,
		},
		{
			name: "a word is not a duration",
			logs: `retry_in=banana`,
			want: wantUnparseable,
		},
		{
			name: "a unit with no magnitude is not a duration",
			logs: `retry_in=ms`,
			want: wantUnparseable,
		},
		{
			name: "scientific notation is not a Go duration",
			logs: `retry_in=1e3ms`,
			want: wantUnparseable,
		},
		{
			name: "an upper-case unit is not a Go duration",
			logs: `retry_in=5MS`,
			want: wantUnparseable,
		},
		{
			name: "a lone sign is not a duration, and is not a non-positive value either",
			logs: `retry_in=-`,
			want: wantUnparseable,
		},
		{
			name: "a doubled sign is not a duration",
			logs: `retry_in=--5ms`,
			want: wantUnparseable,
		},
		{
			// No-silent-truncation, stated as a rule: trailing junk makes the
			// whole token unparseable rather than being shaved off to leave a
			// plausible-looking 5ms. The old class did shave it off.
			name: "trailing junk makes the token unparseable rather than being shaved off",
			logs: `retry_in=5ms,`,
			want: wantUnparseable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAnnouncedRetryIns(tc.logs)

			var np *nonPositiveRetryInError
			isNonPositive := errors.As(err, &np)

			switch tc.want {
			case wantOK:
				if err != nil {
					t.Fatalf("parseAnnouncedRetryIns(%q) returned error %v, want the waits %v parsed cleanly", tc.logs, err, tc.waits)
				}
				if len(got) != len(tc.waits) {
					t.Fatalf("parseAnnouncedRetryIns(%q) returned %d waits (%v), want %d (%v)", tc.logs, len(got), got, len(tc.waits), tc.waits)
				}
				for i := range tc.waits {
					if got[i] != tc.waits[i] {
						t.Errorf("parseAnnouncedRetryIns(%q) wait %d is %v, want exactly %v", tc.logs, i, got[i], tc.waits[i])
					}
				}
			case wantUnparseable:
				if err == nil {
					t.Fatalf("parseAnnouncedRetryIns(%q) accepted a token that is not a duration and returned %v, want a parse failure", tc.logs, got)
				}
				if isNonPositive {
					t.Fatalf("parseAnnouncedRetryIns(%q) rejected the token as a non-positive DURATION (%v); it is not a duration at all, so the failure must come from the duration parse", tc.logs, np.Parsed)
				}
				if len(got) != 0 {
					t.Errorf("parseAnnouncedRetryIns(%q) returned %d waits (%v) alongside its error, want none", tc.logs, len(got), got)
				}
			case wantNonPositive:
				if err == nil {
					t.Fatalf("parseAnnouncedRetryIns(%q) returned %v with no error, want the non-positive announcement rejected", tc.logs, got)
				}
				if !isNonPositive {
					t.Fatalf("parseAnnouncedRetryIns(%q) failed with %v, want a *nonPositiveRetryInError carrying the value it read", tc.logs, err)
				}
				if np.Parsed != tc.parsed {
					t.Errorf("parseAnnouncedRetryIns(%q) read the announcement as %v, want exactly %v — reading a negative wait as its positive magnitude is the #7183 defect, and a value that clears every upper bound in this package instead of blowing it", tc.logs, np.Parsed, tc.parsed)
				}
				if len(got) != 0 {
					t.Errorf("parseAnnouncedRetryIns(%q) returned %d waits (%v) alongside its rejection, want none", tc.logs, len(got), got)
				}
			}
		})
	}
}
