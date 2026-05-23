package resolve

import "regexp"

// smlDynamicPatterns are per-language patterns for Standard ML.
// Registered via init() into dynamicPatternsByLang.
//
// The SML extractor (internal/extractors/sml/extractor.go) emits CALLS edges
// whose ToID is either:
//   - a bare function name: "helper"
//   - a module-qualified call: "List.map", "String.tokens", "Int.toString"
//
// Two categories of patterns:
//
//  1. SML stdlib module-qualified identifiers — dotted names characteristic of
//     SML's Basis Library:
//     List: List.map, List.filter, List.foldl, List.app, etc.
//     String: String.tokens, String.concat, String.sub, etc.
//     Int: Int.toString, Int.fromString, Int.compare, etc.
//     Real: Real.toString, Real.fromString, Real.floor, etc.
//     IO: TextIO.print, TextIO.inputLine, BinIO.openIn, etc.
//     OS: OS.FileSys.*, OS.Path.*, OS.Process.*
//     Array/Vector: Array.sub, Vector.map, etc.
//
//  2. MLton / SML/NJ specific patterns — runtime/system-specific names.
//
// All patterns are gated to lang=="sml" (safer-bias rule).
var smlDynamicPatterns = []*regexp.Regexp{
	// ── List module (SML Basis) ───────────────────────────────────────────
	regexp.MustCompile(`^List\.map$`),       // List.map f lst
	regexp.MustCompile(`^List\.filter$`),    // List.filter pred lst
	regexp.MustCompile(`^List\.foldl$`),     // List.foldl f init lst
	regexp.MustCompile(`^List\.foldr$`),     // List.foldr f init lst
	regexp.MustCompile(`^List\.app$`),       // List.app f lst
	regexp.MustCompile(`^List\.appi$`),      // List.appi f lst
	regexp.MustCompile(`^List\.mapi$`),      // List.mapi f lst
	regexp.MustCompile(`^List\.find$`),      // List.find pred lst
	regexp.MustCompile(`^List\.exists$`),    // List.exists pred lst
	regexp.MustCompile(`^List\.all$`),       // List.all pred lst
	regexp.MustCompile(`^List\.null$`),      // List.null lst
	regexp.MustCompile(`^List\.hd$`),        // List.hd lst
	regexp.MustCompile(`^List\.tl$`),        // List.tl lst
	regexp.MustCompile(`^List\.last$`),      // List.last lst
	regexp.MustCompile(`^List\.length$`),    // List.length lst
	regexp.MustCompile(`^List\.nth$`),       // List.nth (lst, n)
	regexp.MustCompile(`^List\.rev$`),       // List.rev lst
	regexp.MustCompile(`^List\.revAppend$`), // List.revAppend lst1 lst2
	regexp.MustCompile(`^List\.append$`),    // List.append lst1 lst2
	regexp.MustCompile(`^List\.concat$`),    // List.concat lsts
	regexp.MustCompile(`^List\.partition$`), // List.partition pred lst
	regexp.MustCompile(`^List\.getItem$`),   // List.getItem lst
	regexp.MustCompile(`^List\.collate$`),   // List.collate cmp lst1 lst2
	regexp.MustCompile(`^List\.tabulate$`),  // List.tabulate (n, f)

	// ── ListPair module ───────────────────────────────────────────────────
	regexp.MustCompile(`^ListPair\.map$`),    // ListPair.map f (lst1, lst2)
	regexp.MustCompile(`^ListPair\.app$`),    // ListPair.app f (lst1, lst2)
	regexp.MustCompile(`^ListPair\.foldl$`),  // ListPair.foldl f init (lst1, lst2)
	regexp.MustCompile(`^ListPair\.foldr$`),  // ListPair.foldr f init (lst1, lst2)
	regexp.MustCompile(`^ListPair\.zip$`),    // ListPair.zip (lst1, lst2)
	regexp.MustCompile(`^ListPair\.unzip$`),  // ListPair.unzip pairs
	regexp.MustCompile(`^ListPair\.all$`),    // ListPair.all pred (lst1, lst2)
	regexp.MustCompile(`^ListPair\.exists$`), // ListPair.exists pred (lst1, lst2)

	// ── String module (SML Basis) ─────────────────────────────────────────
	regexp.MustCompile(`^String\.tokens$`),      // String.tokens pred s
	regexp.MustCompile(`^String\.fields$`),      // String.fields pred s
	regexp.MustCompile(`^String\.concat$`),      // String.concat lst
	regexp.MustCompile(`^String\.concatWith$`),  // String.concatWith sep lst
	regexp.MustCompile(`^String\.sub$`),         // String.sub (s, i)
	regexp.MustCompile(`^String\.substring$`),   // String.substring (s, i, n)
	regexp.MustCompile(`^String\.extract$`),     // String.extract (s, i, opt)
	regexp.MustCompile(`^String\.size$`),        // String.size s
	regexp.MustCompile(`^String\.str$`),         // String.str c
	regexp.MustCompile(`^String\.implode$`),     // String.implode chars
	regexp.MustCompile(`^String\.explode$`),     // String.explode s
	regexp.MustCompile(`^String\.map$`),         // String.map f s
	regexp.MustCompile(`^String\.app$`),         // String.app f s
	regexp.MustCompile(`^String\.compare$`),     // String.compare (s1, s2)
	regexp.MustCompile(`^String\.<$`),           // String.< (s1, s2)
	regexp.MustCompile(`^String\.<=$`),          // String.<= (s1, s2)
	regexp.MustCompile(`^String\.>$`),           // String.> (s1, s2)
	regexp.MustCompile(`^String\.>=$`),          // String.>= (s1, s2)
	regexp.MustCompile(`^String\.isPrefix$`),    // String.isPrefix prefix s
	regexp.MustCompile(`^String\.isSuffix$`),    // String.isSuffix suffix s
	regexp.MustCompile(`^String\.isSubstring$`), // String.isSubstring sub s
	regexp.MustCompile(`^String\.toUpper$`),     // String.toUpper s
	regexp.MustCompile(`^String\.toLower$`),     // String.toLower s
	regexp.MustCompile(`^String\.collate$`),     // String.collate cmp (s1, s2)
	regexp.MustCompile(`^String\.fromString$`),  // String.fromString s
	regexp.MustCompile(`^String\.toString$`),    // String.toString s

	// ── Int module (SML Basis) ────────────────────────────────────────────
	regexp.MustCompile(`^Int\.toString$`),   // Int.toString n
	regexp.MustCompile(`^Int\.fromString$`), // Int.fromString s
	regexp.MustCompile(`^Int\.compare$`),    // Int.compare (i1, i2)
	regexp.MustCompile(`^Int\.min$`),        // Int.min (i1, i2)
	regexp.MustCompile(`^Int\.max$`),        // Int.max (i1, i2)
	regexp.MustCompile(`^Int\.abs$`),        // Int.abs n
	regexp.MustCompile(`^Int\.sign$`),       // Int.sign n
	regexp.MustCompile(`^Int\.sameSign$`),   // Int.sameSign (i1, i2)
	regexp.MustCompile(`^Int\.fmt$`),        // Int.fmt radix n
	regexp.MustCompile(`^Int\.scan$`),       // Int.scan radix reader
	regexp.MustCompile(`^Int\.toInt$`),      // Int.toInt n
	regexp.MustCompile(`^Int\.fromInt$`),    // Int.fromInt n
	regexp.MustCompile(`^Int\.toLarge$`),    // Int.toLarge n
	regexp.MustCompile(`^Int\.fromLarge$`),  // Int.fromLarge n
	regexp.MustCompile(`^Int\.div$`),        // Int.div (n, m)
	regexp.MustCompile(`^Int\.mod$`),        // Int.mod (n, m)
	regexp.MustCompile(`^Int\.quot$`),       // Int.quot (n, m)
	regexp.MustCompile(`^Int\.rem$`),        // Int.rem (n, m)

	// ── Real module (SML Basis) ───────────────────────────────────────────
	regexp.MustCompile(`^Real\.toString$`),   // Real.toString r
	regexp.MustCompile(`^Real\.fromString$`), // Real.fromString s
	regexp.MustCompile(`^Real\.compare$`),    // Real.compare (r1, r2)
	regexp.MustCompile(`^Real\.min$`),        // Real.min (r1, r2)
	regexp.MustCompile(`^Real\.max$`),        // Real.max (r1, r2)
	regexp.MustCompile(`^Real\.abs$`),        // Real.abs r
	regexp.MustCompile(`^Real\.sign$`),       // Real.sign r
	regexp.MustCompile(`^Real\.floor$`),      // Real.floor r
	regexp.MustCompile(`^Real\.ceil$`),       // Real.ceil r
	regexp.MustCompile(`^Real\.round$`),      // Real.round r
	regexp.MustCompile(`^Real\.trunc$`),      // Real.trunc r
	regexp.MustCompile(`^Real\.toInt$`),      // Real.toInt mode r
	regexp.MustCompile(`^Real\.fromInt$`),    // Real.fromInt n
	regexp.MustCompile(`^Real\.toLarge$`),    // Real.toLarge r
	regexp.MustCompile(`^Real\.fromLarge$`),  // Real.fromLarge mode r
	regexp.MustCompile(`^Real\.fmt$`),        // Real.fmt spec r
	regexp.MustCompile(`^Real\.scan$`),       // Real.scan reader
	regexp.MustCompile(`^Real\.isNan$`),      // Real.isNan r
	regexp.MustCompile(`^Real\.isFinite$`),   // Real.isFinite r
	regexp.MustCompile(`^Real\.posInf$`),     // Real.posInf
	regexp.MustCompile(`^Real\.negInf$`),     // Real.negInf
	regexp.MustCompile(`^Real\.sqrt$`),       // Real.sqrt r
	regexp.MustCompile(`^Real\.exp$`),        // Real.exp r
	regexp.MustCompile(`^Real\.ln$`),         // Real.ln r
	regexp.MustCompile(`^Real\.log10$`),      // Real.log10 r
	regexp.MustCompile(`^Real\.pow$`),        // Real.pow (r, p)
	regexp.MustCompile(`^Real\.sin$`),        // Real.sin r
	regexp.MustCompile(`^Real\.cos$`),        // Real.cos r
	regexp.MustCompile(`^Real\.tan$`),        // Real.tan r
	regexp.MustCompile(`^Real\.atan$`),       // Real.atan r
	regexp.MustCompile(`^Real\.atan2$`),      // Real.atan2 (y, x)

	// ── TextIO module (SML Basis IO) ──────────────────────────────────────
	regexp.MustCompile(`^TextIO\.print$`),       // TextIO.print s
	regexp.MustCompile(`^TextIO\.inputLine$`),   // TextIO.inputLine is
	regexp.MustCompile(`^TextIO\.input$`),       // TextIO.input is
	regexp.MustCompile(`^TextIO\.input1$`),      // TextIO.input1 is
	regexp.MustCompile(`^TextIO\.inputAll$`),    // TextIO.inputAll is
	regexp.MustCompile(`^TextIO\.output$`),      // TextIO.output (os, s)
	regexp.MustCompile(`^TextIO\.output1$`),     // TextIO.output1 (os, c)
	regexp.MustCompile(`^TextIO\.flushOut$`),    // TextIO.flushOut os
	regexp.MustCompile(`^TextIO\.closeIn$`),     // TextIO.closeIn is
	regexp.MustCompile(`^TextIO\.closeOut$`),    // TextIO.closeOut os
	regexp.MustCompile(`^TextIO\.openIn$`),      // TextIO.openIn path
	regexp.MustCompile(`^TextIO\.openOut$`),     // TextIO.openOut path
	regexp.MustCompile(`^TextIO\.openAppend$`),  // TextIO.openAppend path
	regexp.MustCompile(`^TextIO\.stdIn$`),       // TextIO.stdIn
	regexp.MustCompile(`^TextIO\.stdOut$`),      // TextIO.stdOut
	regexp.MustCompile(`^TextIO\.stdErr$`),      // TextIO.stdErr
	regexp.MustCompile(`^TextIO\.endOfStream$`), // TextIO.endOfStream is

	// ── BinIO module ──────────────────────────────────────────────────────
	regexp.MustCompile(`^BinIO\.openIn$`),   // BinIO.openIn path
	regexp.MustCompile(`^BinIO\.openOut$`),  // BinIO.openOut path
	regexp.MustCompile(`^BinIO\.input$`),    // BinIO.input is
	regexp.MustCompile(`^BinIO\.output$`),   // BinIO.output (os, vec)
	regexp.MustCompile(`^BinIO\.closeIn$`),  // BinIO.closeIn is
	regexp.MustCompile(`^BinIO\.closeOut$`), // BinIO.closeOut os

	// ── IO module (generic) ───────────────────────────────────────────────
	regexp.MustCompile(`^IO\.input$`),  // IO.input
	regexp.MustCompile(`^IO\.output$`), // IO.output

	// ── Array module (SML Basis) ──────────────────────────────────────────
	regexp.MustCompile(`^Array\.array$`),    // Array.array (n, init)
	regexp.MustCompile(`^Array\.tabulate$`), // Array.tabulate (n, f)
	regexp.MustCompile(`^Array\.sub$`),      // Array.sub (arr, i)
	regexp.MustCompile(`^Array\.update$`),   // Array.update (arr, i, x)
	regexp.MustCompile(`^Array\.length$`),   // Array.length arr
	regexp.MustCompile(`^Array\.copy$`),     // Array.copy {src, dst, di}
	regexp.MustCompile(`^Array\.app$`),      // Array.app f arr
	regexp.MustCompile(`^Array\.appi$`),     // Array.appi f arr
	regexp.MustCompile(`^Array\.map$`),      // Array.map f arr
	regexp.MustCompile(`^Array\.mapi$`),     // Array.mapi f arr
	regexp.MustCompile(`^Array\.foldl$`),    // Array.foldl f init arr
	regexp.MustCompile(`^Array\.foldr$`),    // Array.foldr f init arr
	regexp.MustCompile(`^Array\.foldli$`),   // Array.foldli f init arr
	regexp.MustCompile(`^Array\.foldri$`),   // Array.foldri f init arr
	regexp.MustCompile(`^Array\.find$`),     // Array.find pred arr
	regexp.MustCompile(`^Array\.exists$`),   // Array.exists pred arr
	regexp.MustCompile(`^Array\.all$`),      // Array.all pred arr
	regexp.MustCompile(`^Array\.toList$`),   // Array.toList arr
	regexp.MustCompile(`^Array\.fromList$`), // Array.fromList lst

	// ── Vector module (SML Basis) ─────────────────────────────────────────
	regexp.MustCompile(`^Vector\.tabulate$`), // Vector.tabulate (n, f)
	regexp.MustCompile(`^Vector\.sub$`),      // Vector.sub (v, i)
	regexp.MustCompile(`^Vector\.length$`),   // Vector.length v
	regexp.MustCompile(`^Vector\.app$`),      // Vector.app f v
	regexp.MustCompile(`^Vector\.map$`),      // Vector.map f v
	regexp.MustCompile(`^Vector\.foldl$`),    // Vector.foldl f init v
	regexp.MustCompile(`^Vector\.foldr$`),    // Vector.foldr f init v
	regexp.MustCompile(`^Vector\.find$`),     // Vector.find pred v
	regexp.MustCompile(`^Vector\.exists$`),   // Vector.exists pred v
	regexp.MustCompile(`^Vector\.all$`),      // Vector.all pred v
	regexp.MustCompile(`^Vector\.toList$`),   // Vector.toList v
	regexp.MustCompile(`^Vector\.fromList$`), // Vector.fromList lst
	regexp.MustCompile(`^Vector\.concat$`),   // Vector.concat vecs

	// ── Char module (SML Basis) ───────────────────────────────────────────
	regexp.MustCompile(`^Char\.isAlpha$`),    // Char.isAlpha c
	regexp.MustCompile(`^Char\.isDigit$`),    // Char.isDigit c
	regexp.MustCompile(`^Char\.isAlphaNum$`), // Char.isAlphaNum c
	regexp.MustCompile(`^Char\.isSpace$`),    // Char.isSpace c
	regexp.MustCompile(`^Char\.isUpper$`),    // Char.isUpper c
	regexp.MustCompile(`^Char\.isLower$`),    // Char.isLower c
	regexp.MustCompile(`^Char\.toUpper$`),    // Char.toUpper c
	regexp.MustCompile(`^Char\.toLower$`),    // Char.toLower c
	regexp.MustCompile(`^Char\.ord$`),        // Char.ord c
	regexp.MustCompile(`^Char\.chr$`),        // Char.chr n
	regexp.MustCompile(`^Char\.compare$`),    // Char.compare (c1, c2)
	regexp.MustCompile(`^Char\.fromString$`), // Char.fromString s
	regexp.MustCompile(`^Char\.toString$`),   // Char.toString c

	// ── OS module (SML Basis) ─────────────────────────────────────────────
	regexp.MustCompile(`^OS\.FileSys\.getDir$`),     // OS.FileSys.getDir()
	regexp.MustCompile(`^OS\.FileSys\.setDir$`),     // OS.FileSys.setDir path
	regexp.MustCompile(`^OS\.FileSys\.mkDir$`),      // OS.FileSys.mkDir path
	regexp.MustCompile(`^OS\.FileSys\.rmDir$`),      // OS.FileSys.rmDir path
	regexp.MustCompile(`^OS\.FileSys\.isDir$`),      // OS.FileSys.isDir path
	regexp.MustCompile(`^OS\.FileSys\.access$`),     // OS.FileSys.access (path, modes)
	regexp.MustCompile(`^OS\.FileSys\.getHomeDir$`), // OS.FileSys.getHomeDir()
	regexp.MustCompile(`^OS\.FileSys\.openDir$`),    // OS.FileSys.openDir path
	regexp.MustCompile(`^OS\.FileSys\.readDir$`),    // OS.FileSys.readDir ds
	regexp.MustCompile(`^OS\.FileSys\.closeDir$`),   // OS.FileSys.closeDir ds
	regexp.MustCompile(`^OS\.Path\.concat$`),        // OS.Path.concat (base, file)
	regexp.MustCompile(`^OS\.Path\.dir$`),           // OS.Path.dir path
	regexp.MustCompile(`^OS\.Path\.file$`),          // OS.Path.file path
	regexp.MustCompile(`^OS\.Path\.ext$`),           // OS.Path.ext path
	regexp.MustCompile(`^OS\.Path\.isAbsolute$`),    // OS.Path.isAbsolute path
	regexp.MustCompile(`^OS\.Path\.isRelative$`),    // OS.Path.isRelative path
	regexp.MustCompile(`^OS\.Path\.mkAbsolute$`),    // OS.Path.mkAbsolute {path, relativeTo}
	regexp.MustCompile(`^OS\.Path\.mkRelative$`),    // OS.Path.mkRelative {path, relativeTo}
	regexp.MustCompile(`^OS\.Process\.exit$`),       // OS.Process.exit n
	regexp.MustCompile(`^OS\.Process\.getEnv$`),     // OS.Process.getEnv name
	regexp.MustCompile(`^OS\.Process\.system$`),     // OS.Process.system cmd
	regexp.MustCompile(`^OS\.Process\.success$`),    // OS.Process.success
	regexp.MustCompile(`^OS\.Process\.failure$`),    // OS.Process.failure

	// ── Option module (SML Basis) ─────────────────────────────────────────
	regexp.MustCompile(`^Option\.map$`),            // Option.map f opt
	regexp.MustCompile(`^Option\.mapPartial$`),     // Option.mapPartial f opt
	regexp.MustCompile(`^Option\.compose$`),        // Option.compose (f, g) x
	regexp.MustCompile(`^Option\.composePartial$`), // Option.composePartial (f, g) x
	regexp.MustCompile(`^Option\.filter$`),         // Option.filter pred x
	regexp.MustCompile(`^Option\.join$`),           // Option.join opt
	regexp.MustCompile(`^Option\.app$`),            // Option.app f opt
	regexp.MustCompile(`^Option\.getOpt$`),         // Option.getOpt (opt, default)
	regexp.MustCompile(`^Option\.isSome$`),         // Option.isSome opt
	regexp.MustCompile(`^Option\.isNone$`),         // Option.isNone opt
	regexp.MustCompile(`^Option\.valOf$`),          // Option.valOf opt
	regexp.MustCompile(`^Option\.collate$`),        // Option.collate cmp (opt1, opt2)

	// ── SML/NJ-specific ───────────────────────────────────────────────────
	regexp.MustCompile(`^SMLofNJ\.Cont\.callcc$`),       // SML/NJ first-class continuations
	regexp.MustCompile(`^SMLofNJ\.Cont\.throw$`),        // SML/NJ continuation throw
	regexp.MustCompile(`^SMLofNJ\.Susp\.delay$`),        // SML/NJ lazy suspension
	regexp.MustCompile(`^SMLofNJ\.Susp\.force$`),        // SML/NJ lazy force
	regexp.MustCompile(`^Control\.Print\.printDepth$`),  // SML/NJ REPL depth control
	regexp.MustCompile(`^Control\.Print\.printLength$`), // SML/NJ REPL length control

	// ── MLton-specific ────────────────────────────────────────────────────
	regexp.MustCompile(`^MLton\.share$`),          // MLton heap sharing
	regexp.MustCompile(`^MLton\.size$`),           // MLton object size
	regexp.MustCompile(`^MLton\.GC\.collect$`),    // MLton GC.collect
	regexp.MustCompile(`^MLton\.GC\.pack$`),       // MLton GC.pack
	regexp.MustCompile(`^MLton\.Thread\.new$`),    // MLton thread creation
	regexp.MustCompile(`^MLton\.Thread\.switch$`), // MLton thread switch
	regexp.MustCompile(`^MLton\.IO\.print$`),      // MLton IO.print

	// ── Math module (SML Basis) ───────────────────────────────────────────
	regexp.MustCompile(`^Math\.sqrt$`),  // Math.sqrt x
	regexp.MustCompile(`^Math\.sin$`),   // Math.sin x
	regexp.MustCompile(`^Math\.cos$`),   // Math.cos x
	regexp.MustCompile(`^Math\.tan$`),   // Math.tan x
	regexp.MustCompile(`^Math\.asin$`),  // Math.asin x
	regexp.MustCompile(`^Math\.acos$`),  // Math.acos x
	regexp.MustCompile(`^Math\.atan$`),  // Math.atan x
	regexp.MustCompile(`^Math\.atan2$`), // Math.atan2 (y, x)
	regexp.MustCompile(`^Math\.exp$`),   // Math.exp x
	regexp.MustCompile(`^Math\.pow$`),   // Math.pow (x, y)
	regexp.MustCompile(`^Math\.ln$`),    // Math.ln x
	regexp.MustCompile(`^Math\.log10$`), // Math.log10 x
	regexp.MustCompile(`^Math\.pi$`),    // Math.pi
	regexp.MustCompile(`^Math\.e$`),     // Math.e
}

func init() {
	dynamicPatternsByLang["sml"] = smlDynamicPatterns
}
