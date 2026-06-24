// Package grafelassets embeds repo-root asset trees that ship inside the
// grafel binary so they are available even when grafel is installed from a
// released tarball that contains only the executable (no repo checkout
// alongside it).
//
// Today this carries the user-invocable grafel skills (the /grafel-* slash
// commands). They live at the repo root in skills/ — the single source of
// truth — and are embedded here so `grafel install` can materialise them on a
// binary-only install where there is no on-disk skills/ directory next to the
// binary to copy from (issue #5503).
//
// go:embed paths cannot traverse "..", so the only package that can embed the
// repo-root skills/ tree directly is one rooted at the module root. This file
// is that package; the install lifecycle consumes SkillsFS via
// internal/install/skilllink.
package grafelassets

import "embed"

// SkillsFS holds the bundled grafel skills, rooted so that paths look like
// "skills/<skill-name>/...". The "all:" prefix ensures dotfiles and files in
// otherwise-ignored directories are embedded too.
//
//go:embed all:skills
var SkillsFS embed.FS
