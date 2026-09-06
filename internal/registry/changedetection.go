package registry

import (
	"strings"
	"time"
)

// Change-detection modes for GroupConfig.Features.ChangeDetection (#6932).
const (
	// ChangeDetectionFsnotify is the fs watcher — the default, and the
	// behaviour of every grafel before 0.3.3.
	ChangeDetectionFsnotify = "fsnotify"
	// ChangeDetectionPoll is the descriptor-free polling change detector
	// (watch.ChangePoller). Opt-in; this is the container lane.
	ChangeDetectionPoll = "poll"
	// ChangeDetectionAuto is reserved for #6932 arm B (the inotify-budget
	// probe and the announced auto-switch). Arm A accepts it and resolves it
	// to fsnotify — see PollingEnabled.
	ChangeDetectionAuto = "auto"
)

// DefaultChangePollInterval is the poll cadence used when
// features.change_poll_interval_seconds is unset or non-positive. See the
// field's doc comment in GroupConfig for the cost arithmetic behind 30 s.
const DefaultChangePollInterval = 30 * time.Second

// ChangeDetectionMode returns the normalised change-detection mode for this
// group. Whitespace and case are tolerated; anything unrecognised — including
// the empty string — resolves to ChangeDetectionFsnotify, because a typo in
// fleet.json must degrade to the historical behaviour, never to "no detector".
func (c *GroupConfig) ChangeDetectionMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Features.ChangeDetection)) {
	case ChangeDetectionPoll:
		return ChangeDetectionPoll
	case ChangeDetectionAuto:
		return ChangeDetectionAuto
	default:
		return ChangeDetectionFsnotify
	}
}

// PollingEnabled reports whether this group asks for the polling change
// detector RIGHT NOW.
//
// It is deliberately not "mode != fsnotify". ChangeDetectionAuto is a valid,
// accepted, documented value whose probe does not exist yet (#6932 arm B), and
// arm A promises that setting it changes nothing for anyone. The two questions
// — "what did the user write?" and "what runs today?" — are separate, and
// keeping them separate is what lets arm B change only this function.
func (c *GroupConfig) PollingEnabled() bool {
	return c.ChangeDetectionMode() == ChangeDetectionPoll
}

// ChangePollInterval returns the configured poll cadence, or
// DefaultChangePollInterval when unset or non-positive.
func (c *GroupConfig) ChangePollInterval() time.Duration {
	if n := c.Features.ChangePollIntervalSeconds; n > 0 {
		return time.Duration(n) * time.Second
	}
	return DefaultChangePollInterval
}
