// Package hcl provides the HCL grammar via a vendored C grammar, wrapped as a
// ts.Language for the official tree-sitter adapter (B2 cutover, #5418, ADR
// 0023). tree-sitter-grammars/tree-sitter-hcl commits a src/parser.c, but the
// ABI-14 tags (v1.1.0/v1.1.1) ship no usable go-get path and v1.2.0's committed
// parser.c is ABI 15 (SIGSEGVs against the v0.24.0 runtime). So the committed C
// is not directly vendorable (the batch-4b blocker). The parser.c here was
// regenerated from the v1.1.1 grammar.js (which require()s make_grammar.js, also
// vendored at generate time) with the tree-sitter CLI v0.23.x line — which emits
// LANGUAGE_VERSION 15 — then vendored (parser.c + the external scanner.c +
// tree_sitter/ headers) into this package and compiled against the official
// runtime. See docs/treesitter-cutover-plan.md §3/§4/§5.
//
// ABI pin. The regenerated parser.c emits LANGUAGE_VERSION 15, inside the
// v0.25.0 runtime's accepted 13–15 window (ADR 0023 §1), so it loads and parses
// without further work. hcl has an external scanner (scanner.c), compiled into
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
//	source: github.com/tree-sitter-grammars/tree-sitter-hcl
//	ref:    009def4ae38ec30e5b40beeae26efe93484ab286 (v1.1.1)
//	files:  parser.c, scanner.c, tree_sitter/{parser,alloc,array}.h
//	regenerated-with: tree-sitter-cli v0.23.2 (committed parser.c was ABI 15)
//	license: Apache-2.0
//	SPDX-License-Identifier: Apache-2.0
package hcl

// #cgo CFLAGS: -I${SRCDIR} -std=c11
// #include <tree_sitter/parser.h>
// TSLanguage *tree_sitter_hcl(void);
import "C"

import (
	"unsafe"

	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the HCL grammar as a ts.Language bound to the official
// adapter, by wrapping the vendored C grammar's exported language pointer.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(unsafe.Pointer(C.tree_sitter_hcl())))
}
