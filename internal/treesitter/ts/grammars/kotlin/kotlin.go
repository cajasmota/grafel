// Package kotlin provides the Kotlin grammar via a vendored C grammar, wrapped
// as a ts.Language for the official tree-sitter adapter (B2 cutover, #5418, ADR
// 0023). fwcd/tree-sitter-kotlin commits its generated src/parser.c, but that
// file lives outside the nested bindings/go module boundary, so a `go get` of
// the binding cannot reach it (the batch-2 blocker). Vendoring the C (parser.c
// + scanner.c + tree_sitter/ headers) into this package and compiling against
// the official runtime is the clean path. See docs/treesitter-cutover-plan.md
// §3/§4.
//
// ABI pin. The vendored parser.c emits LANGUAGE_VERSION 15, inside the v0.25.0
// runtime's accepted 13–15 window (ADR 0023 §1), so it loads and parses without
// regeneration. kotlin has an external scanner (scanner.c), compiled into this
// package by cgo alongside parser.c.
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
//	source: github.com/fwcd/tree-sitter-kotlin
//	ref:    e1a2d5ad1f61f5740677183cd4125bb071cd2f30 (0.3.8, 2024-08-03)
//	files:  parser.c, scanner.c, tree_sitter/{parser,alloc,array}.h
//	license: MIT (Copyright (c) 2019 fwcd)
//	SPDX-License-Identifier: MIT
package kotlin

// #cgo CFLAGS: -I${SRCDIR} -std=c11
// #include <tree_sitter/parser.h>
// TSLanguage *tree_sitter_kotlin(void);
import "C"

import (
	"unsafe"

	tsofficial "github.com/tree-sitter/go-tree-sitter"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/treesitter/ts/official"
)

// Language returns the Kotlin grammar as a ts.Language bound to the official
// adapter, by wrapping the vendored C grammar's exported language pointer.
func Language() ts.Language {
	return official.WrapLanguage(tsofficial.NewLanguage(unsafe.Pointer(C.tree_sitter_kotlin())))
}
