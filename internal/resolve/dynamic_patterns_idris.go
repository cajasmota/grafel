package resolve

import "regexp"

// idrisDynamicPatterns are per-language patterns for Idris.
// Registered via init() into dynamicPatternsByLang.
//
// The Idris extractor (internal/extractors/idris/extractor.go) emits CALLS
// edges whose ToID is the bare function name at the call site.
//
// Three categories of patterns:
//
//  1. Idris Prelude / base library — functions in the implicit Prelude import.
//     These are similar to Haskell's Prelude but include Idris-specific names.
//
//  2. Dependent-type stdlib — decEq, rewrite helpers, Vect/Nat operations.
//     Gated to lang=="idris" since these are highly Idris-specific.
//
//  3. Common Idris library functions — Data.List, Data.Vect, Data.Maybe,
//     System.File, Control.Monad.
//
// All patterns are gated to lang=="idris" (safer-bias rule).
var idrisDynamicPatterns = []*regexp.Regexp{
	// ── Prelude ──────────────────────────────────────────────────────
	regexp.MustCompile(`^map$`),       // Prelude.map
	regexp.MustCompile(`^filter$`),    // Prelude.filter
	regexp.MustCompile(`^foldr$`),     // Prelude.foldr
	regexp.MustCompile(`^foldl$`),     // Prelude.foldl
	regexp.MustCompile(`^foldl'$`),    // strict foldl
	regexp.MustCompile(`^show$`),      // Show.show
	regexp.MustCompile(`^print$`),     // IO.print
	regexp.MustCompile(`^printLn$`),   // IO.printLn (Idris uses printLn not putStrLn)
	regexp.MustCompile(`^putStrLn$`),  // Prelude.putStrLn (also available)
	regexp.MustCompile(`^putStr$`),    // Prelude.putStr
	regexp.MustCompile(`^getLine$`),   // Prelude.getLine
	regexp.MustCompile(`^length$`),    // Prelude.length
	regexp.MustCompile(`^head$`),      // Prelude.head
	regexp.MustCompile(`^tail$`),      // Prelude.tail
	regexp.MustCompile(`^last$`),      // Prelude.last
	regexp.MustCompile(`^init$`),      // Prelude.init
	regexp.MustCompile(`^null$`),      // Prelude.null
	regexp.MustCompile(`^reverse$`),   // Prelude.reverse
	regexp.MustCompile(`^concat$`),    // Prelude.concat
	regexp.MustCompile(`^concatMap$`), // Prelude.concatMap
	regexp.MustCompile(`^zip$`),       // Prelude.zip
	regexp.MustCompile(`^zipWith$`),   // Prelude.zipWith
	regexp.MustCompile(`^replicate$`), // Prelude.replicate
	regexp.MustCompile(`^take$`),      // Prelude.take
	regexp.MustCompile(`^drop$`),      // Prelude.drop
	regexp.MustCompile(`^span$`),      // Prelude.span
	regexp.MustCompile(`^break$`),     // Prelude.break
	regexp.MustCompile(`^elem$`),      // Prelude.elem
	regexp.MustCompile(`^lookup$`),    // Prelude.lookup
	regexp.MustCompile(`^maximum$`),   // Prelude.maximum
	regexp.MustCompile(`^minimum$`),   // Prelude.minimum
	regexp.MustCompile(`^sum$`),       // Prelude.sum
	regexp.MustCompile(`^product$`),   // Prelude.product
	regexp.MustCompile(`^and$`),       // Prelude.and
	regexp.MustCompile(`^or$`),        // Prelude.or
	regexp.MustCompile(`^any$`),       // Prelude.any
	regexp.MustCompile(`^all$`),       // Prelude.all
	regexp.MustCompile(`^words$`),     // Prelude.words
	regexp.MustCompile(`^unwords$`),   // Prelude.unwords
	regexp.MustCompile(`^lines$`),     // Prelude.lines
	regexp.MustCompile(`^unlines$`),   // Prelude.unlines
	regexp.MustCompile(`^id$`),        // Prelude.id
	regexp.MustCompile(`^const$`),     // Prelude.const
	regexp.MustCompile(`^flip$`),      // Prelude.flip
	regexp.MustCompile(`^fst$`),       // Prelude.fst
	regexp.MustCompile(`^snd$`),       // Prelude.snd
	regexp.MustCompile(`^not$`),       // Prelude.not
	regexp.MustCompile(`^even$`),      // Prelude.even (via Num instance)
	regexp.MustCompile(`^odd$`),       // Prelude.odd
	regexp.MustCompile(`^abs$`),       // Prelude.abs

	// ── Monad / Applicative combinators ──────────────────────────────
	regexp.MustCompile(`^pure$`),     // Applicative.pure
	regexp.MustCompile(`^when$`),     // Monad.when (gated to idris)
	regexp.MustCompile(`^unless$`),   // Monad.unless
	regexp.MustCompile(`^sequence$`), // Monad.sequence
	regexp.MustCompile(`^mapM$`),     // Traversable.mapM
	regexp.MustCompile(`^forM$`),     // Traversable.forM (idris has traverse)
	regexp.MustCompile(`^traverse$`), // Traversable.traverse — idris-specific
	regexp.MustCompile(`^fmap$`),     // Functor.fmap
	regexp.MustCompile(`^guard$`),    // Alternative.guard
	regexp.MustCompile(`^join$`),     // Monad.join

	// ── Dependent-type stdlib ─────────────────────────────────────────
	regexp.MustCompile(`^decEq$`),   // DecEq.decEq :: (x : a) -> (y : a) -> Dec (x = y)
	regexp.MustCompile(`^rewrite$`), // rewrite equality proof (used as function in some cases)
	regexp.MustCompile(`^replace$`), // replace proof equality
	regexp.MustCompile(`^sym$`),     // symmetry of equality proof
	regexp.MustCompile(`^trans$`),   // transitivity of equality
	regexp.MustCompile(`^cong$`),    // congruence: cong f p
	regexp.MustCompile(`^Refl$`),    // Refl : x = x (used as value/constructor — gated)

	// ── Vect operations (Data.Vect) ───────────────────────────────────
	regexp.MustCompile(`^replicate$`), // Vect.replicate (covered above — dupe OK)
	regexp.MustCompile(`^index$`),     // Vect.index : Fin n -> Vect n a -> a
	regexp.MustCompile(`^updateAt$`),  // Vect.updateAt
	regexp.MustCompile(`^deleteAt$`),  // Vect.deleteAt
	regexp.MustCompile(`^insertAt$`),  // Vect.insertAt
	regexp.MustCompile(`^toList$`),    // Vect.toList
	regexp.MustCompile(`^fromList$`),  // Vect.fromList (partial)
	regexp.MustCompile(`^toVect$`),    // Data.List.toVect
	regexp.MustCompile(`^transpose$`), // Data.Vect.transpose

	// ── Nat arithmetic ────────────────────────────────────────────────
	regexp.MustCompile(`^natToInteger$`), // Data.Nat.natToInteger
	regexp.MustCompile(`^integerToNat$`), // Data.Nat.integerToNat
	regexp.MustCompile(`^finToNat$`),     // Data.Fin.finToNat

	// ── Data.Maybe ────────────────────────────────────────────────────
	regexp.MustCompile(`^fromMaybe$`), // Data.Maybe.fromMaybe
	regexp.MustCompile(`^isJust$`),    // Data.Maybe.isJust
	regexp.MustCompile(`^isNothing$`), // Data.Maybe.isNothing
	regexp.MustCompile(`^catMaybes$`), // Data.Maybe.catMaybes
	regexp.MustCompile(`^mapMaybe$`),  // Data.Maybe.mapMaybe
	regexp.MustCompile(`^maybe$`),     // Prelude.maybe

	// ── System.File ───────────────────────────────────────────────────
	regexp.MustCompile(`^readFile$`),  // System.File.readFile
	regexp.MustCompile(`^writeFile$`), // System.File.writeFile
	regexp.MustCompile(`^openFile$`),  // System.File.openFile
	regexp.MustCompile(`^closeFile$`), // System.File.closeFile
	regexp.MustCompile(`^fileError$`), // System.File.fileError

	// ── Data.String / Data.List ───────────────────────────────────────
	regexp.MustCompile(`^isPrefixOf$`),  // Data.List.isPrefixOf
	regexp.MustCompile(`^isSuffixOf$`),  // Data.List.isSuffixOf
	regexp.MustCompile(`^intercalate$`), // Data.List.intercalate
	regexp.MustCompile(`^intersperse$`), // Data.List.intersperse
	regexp.MustCompile(`^nub$`),         // Data.List.nub
	regexp.MustCompile(`^sortBy$`),      // Data.List.sortBy
	regexp.MustCompile(`^partition$`),   // Data.List.partition
	regexp.MustCompile(`^unzip$`),       // Data.List.unzip
	regexp.MustCompile(`^uncons$`),      // Data.List.uncons

	// ── Cast / convert ────────────────────────────────────────────────
	regexp.MustCompile(`^cast$`),       // cast : Cast a b => a -> b (gated to idris)
	regexp.MustCompile(`^believe_me$`), // believe_me : a -> b (unsafe cast — idris specific)
}

func init() {
	dynamicPatternsByLang["idris"] = idrisDynamicPatterns
}
