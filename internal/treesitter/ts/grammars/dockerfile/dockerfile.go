// Package dockerfile provides the Dockerfile grammar via a vendored C grammar,
// wrapped as a ts.Language for the official tree-sitter adapter (B2 cutover,
// #5418, ADR 0023). camdencheek/tree-sitter-dockerfile's published binding is
// already official-style, but its module go.mod couples to smacker at the
// module level; vendoring the C (parser.c + scanner.c + tree_sitter/ headers)
// into this package and compiling against the official runtime bypasses that.
// See docs/treesitter-cutover-plan.md §4.
//
// ABI pin. The vendored parser.c emits LANGUAGE_VERSION 15, inside the v0.25.0
// runtime's accepted 13–15 window (ADR 0023 §1), so it loads and parses without
// regeneration. dockerfile has an external scanner (scanner.c), compiled into
// this package by cgo alongside parser.c.
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
//	source: github.com/camdencheek/tree-sitter-dockerfile
//	ref:    868e44ce378deb68aac902a9db68ff82d2299dd0 (v0.2.0, 2024-05-09)
//	files:  parser.c, scanner.c, tree_sitter/{parser,alloc,array}.h
//	license: MIT (Copyright (c) 2021 Camden Cheek)
//	SPDX-License-Identifier: MIT
package dockerfile

// #cgo CFLAGS: -I${SRCDIR} -std=c11
// #include <tree_sitter/parser.h>
// TSLanguage *tree_sitter_dockerfile(void);
import "C"

import (
	"unsafe"

	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the Dockerfile grammar as a ts.Language bound to the official
// adapter, by wrapping the vendored C grammar's exported language pointer.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(unsafe.Pointer(C.tree_sitter_dockerfile())))
}
