// Package proto provides the Protocol Buffers grammar via a vendored C grammar,
// wrapped as a ts.Language for the official tree-sitter adapter (B2 cutover,
// #5418, ADR 0023). Unlike the go-get providers (e.g. golang/), proto has no Go
// binding published anywhere — mitchellh/tree-sitter-proto is a frozen 2021
// grammar — so its src/parser.c is vendored directly into this package and the
// cgo binding calls the exported C symbol instead of importing a module's
// Language(). See docs/treesitter-cutover-plan.md §3.
//
// ABI pin. The vendored parser.c emits LANGUAGE_VERSION 15, inside the v0.25.0
// runtime's accepted 13–15 window (ADR 0023 §1), so it loads and parses without
// regeneration. The grammar is frozen upstream, so the snapshot needs no churn.
// proto has no external scanner.
//
// ABI-15 rollout (#5473 Phase 2). parser.c + tree_sitter/ headers were
// REGENERATED at the same vendored source ref with `tree-sitter generate
// --abi 15` (CLI 0.26.9; a minimal tree-sitter.json was added for the grammars
// that lacked one, which the 0.26 CLI requires to emit ABI 15). node-types are
// unchanged apart from metadata, so the extractor is intact; only the parse
// tables move to LANGUAGE_VERSION 15, matching the rest of the registry under
// the go-tree-sitter v0.25.0 runtime. The hand-written scanner.c is ABI-neutral
// and unchanged.
// Vendored source — license/attribution (license-audit gate):
//
//	source: github.com/mitchellh/tree-sitter-proto
//	ref:    42d82fa18f8afe59b5fc0b16c207ee4f84cb185f (master, 2021-06-12)
//	files:  parser.c, tree_sitter/parser.h
//	license: MIT (Copyright (c) 2021 Mitchell Hashimoto)
//	SPDX-License-Identifier: MIT
package proto

// #cgo CFLAGS: -I${SRCDIR} -std=c11
// #include <tree_sitter/parser.h>
// TSLanguage *tree_sitter_proto(void);
import "C"

import (
	"unsafe"

	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the proto grammar as a ts.Language bound to the official
// adapter, by wrapping the vendored C grammar's exported language pointer.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(unsafe.Pointer(C.tree_sitter_proto())))
}
