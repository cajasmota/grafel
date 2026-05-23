package resolve

import "regexp"

// fsharpDynamicPatterns are per-language patterns for F#.
// Registered via init() into dynamicPatternsByLang.
//
// The F# extractor (internal/extractors/fsharp/extractor.go) emits CALLS edges
// whose ToID is either:
//   - a bare function name: "helper"
//   - a module-qualified call: "List.map", "Option.defaultValue"
//   - a pipe-chain target: captured as "List.filter", "Async.RunSynchronously"
//
// Two categories of patterns:
//
//  1. F#-unique stdlib module-qualified identifiers — dotted names that exist
//     only in F#'s core library and are highly unlikely to appear in other
//     languages as user-defined identifiers:
//     Collections.List: List.map, List.filter, List.fold, List.iter, etc.
//     Collections.Seq:  Seq.iter, Seq.map, Seq.filter, Seq.toList, etc.
//     Option:           Option.map, Option.bind, Option.defaultValue, etc.
//     Result:           Result.bind, Result.map, Result.mapError, etc.
//     Array:            Array.map, Array.filter, Array.fold, etc.
//     Async/Task:       Async.RunSynchronously, Async.AwaitTask, etc.
//     Giraffe HTTP:     route, routef, choose, setStatusCode, text, json, htmlString
//
//  2. F#-gated common names — common enough words requiring the gate:
//     printfn, sprintf, failwith, ignore, id, fst, snd
//
// All patterns are gated to lang=="fsharp" (safer-bias rule).
var fsharpDynamicPatterns = []*regexp.Regexp{
	// ── List module ──────────────────────────────────────────────────────
	regexp.MustCompile(`^List\.map$`),        // List.map f lst
	regexp.MustCompile(`^List\.filter$`),     // List.filter pred lst
	regexp.MustCompile(`^List\.fold$`),       // List.fold f init lst
	regexp.MustCompile(`^List\.foldBack$`),   // List.foldBack f lst init
	regexp.MustCompile(`^List\.iter$`),       // List.iter f lst
	regexp.MustCompile(`^List\.iteri$`),      // List.iteri f lst
	regexp.MustCompile(`^List\.mapi$`),       // List.mapi f lst
	regexp.MustCompile(`^List\.collect$`),    // List.collect f lst
	regexp.MustCompile(`^List\.choose$`),     // List.choose f lst
	regexp.MustCompile(`^List\.forall$`),     // List.forall pred lst
	regexp.MustCompile(`^List\.exists$`),     // List.exists pred lst
	regexp.MustCompile(`^List\.find$`),       // List.find pred lst
	regexp.MustCompile(`^List\.tryFind$`),    // List.tryFind pred lst
	regexp.MustCompile(`^List\.head$`),       // List.head lst
	regexp.MustCompile(`^List\.tail$`),       // List.tail lst
	regexp.MustCompile(`^List\.last$`),       // List.last lst
	regexp.MustCompile(`^List\.length$`),     // List.length lst
	regexp.MustCompile(`^List\.isEmpty$`),    // List.isEmpty lst
	regexp.MustCompile(`^List\.append$`),     // List.append lst1 lst2
	regexp.MustCompile(`^List\.concat$`),     // List.concat lsts
	regexp.MustCompile(`^List\.rev$`),        // List.rev lst
	regexp.MustCompile(`^List\.sort$`),       // List.sort lst
	regexp.MustCompile(`^List\.sortBy$`),     // List.sortBy key lst
	regexp.MustCompile(`^List\.sortWith$`),   // List.sortWith cmp lst
	regexp.MustCompile(`^List\.sum$`),        // List.sum lst
	regexp.MustCompile(`^List\.sumBy$`),      // List.sumBy f lst
	regexp.MustCompile(`^List\.min$`),        // List.min lst
	regexp.MustCompile(`^List\.max$`),        // List.max lst
	regexp.MustCompile(`^List\.minBy$`),      // List.minBy key lst
	regexp.MustCompile(`^List\.maxBy$`),      // List.maxBy key lst
	regexp.MustCompile(`^List\.take$`),       // List.take n lst
	regexp.MustCompile(`^List\.skip$`),       // List.skip n lst
	regexp.MustCompile(`^List\.zip$`),        // List.zip lst1 lst2
	regexp.MustCompile(`^List\.unzip$`),      // List.unzip lst
	regexp.MustCompile(`^List\.partition$`),  // List.partition pred lst
	regexp.MustCompile(`^List\.distinct$`),   // List.distinct lst
	regexp.MustCompile(`^List\.distinctBy$`), // List.distinctBy key lst
	regexp.MustCompile(`^List\.groupBy$`),    // List.groupBy key lst
	regexp.MustCompile(`^List\.countBy$`),    // List.countBy key lst
	regexp.MustCompile(`^List\.pairwise$`),   // List.pairwise lst
	regexp.MustCompile(`^List\.windowed$`),   // List.windowed n lst
	regexp.MustCompile(`^List\.init$`),       // List.init n f
	regexp.MustCompile(`^List\.replicate$`),  // List.replicate n x
	regexp.MustCompile(`^List\.ofSeq$`),      // List.ofSeq seq
	regexp.MustCompile(`^List\.toSeq$`),      // List.toSeq lst
	regexp.MustCompile(`^List\.ofArray$`),    // List.ofArray arr
	regexp.MustCompile(`^List\.toArray$`),    // List.toArray lst
	regexp.MustCompile(`^List\.indexed$`),    // List.indexed lst
	regexp.MustCompile(`^List\.item$`),       // List.item i lst
	regexp.MustCompile(`^List\.nth$`),        // List.nth lst i (legacy)

	// ── Seq module ───────────────────────────────────────────────────────
	regexp.MustCompile(`^Seq\.map$`),          // Seq.map f seq
	regexp.MustCompile(`^Seq\.filter$`),       // Seq.filter pred seq
	regexp.MustCompile(`^Seq\.fold$`),         // Seq.fold f init seq
	regexp.MustCompile(`^Seq\.iter$`),         // Seq.iter f seq
	regexp.MustCompile(`^Seq\.iteri$`),        // Seq.iteri f seq
	regexp.MustCompile(`^Seq\.collect$`),      // Seq.collect f seq
	regexp.MustCompile(`^Seq\.choose$`),       // Seq.choose f seq
	regexp.MustCompile(`^Seq\.forall$`),       // Seq.forall pred seq
	regexp.MustCompile(`^Seq\.exists$`),       // Seq.exists pred seq
	regexp.MustCompile(`^Seq\.find$`),         // Seq.find pred seq
	regexp.MustCompile(`^Seq\.tryFind$`),      // Seq.tryFind pred seq
	regexp.MustCompile(`^Seq\.head$`),         // Seq.head seq
	regexp.MustCompile(`^Seq\.length$`),       // Seq.length seq
	regexp.MustCompile(`^Seq\.isEmpty$`),      // Seq.isEmpty seq
	regexp.MustCompile(`^Seq\.toList$`),       // Seq.toList seq
	regexp.MustCompile(`^Seq\.toArray$`),      // Seq.toArray seq
	regexp.MustCompile(`^Seq\.ofList$`),       // Seq.ofList lst
	regexp.MustCompile(`^Seq\.ofArray$`),      // Seq.ofArray arr
	regexp.MustCompile(`^Seq\.take$`),         // Seq.take n seq
	regexp.MustCompile(`^Seq\.skip$`),         // Seq.skip n seq
	regexp.MustCompile(`^Seq\.zip$`),          // Seq.zip seq1 seq2
	regexp.MustCompile(`^Seq\.append$`),       // Seq.append seq1 seq2
	regexp.MustCompile(`^Seq\.concat$`),       // Seq.concat seqs
	regexp.MustCompile(`^Seq\.sort$`),         // Seq.sort seq
	regexp.MustCompile(`^Seq\.sortBy$`),       // Seq.sortBy key seq
	regexp.MustCompile(`^Seq\.groupBy$`),      // Seq.groupBy key seq
	regexp.MustCompile(`^Seq\.distinct$`),     // Seq.distinct seq
	regexp.MustCompile(`^Seq\.distinctBy$`),   // Seq.distinctBy key seq
	regexp.MustCompile(`^Seq\.sum$`),          // Seq.sum seq
	regexp.MustCompile(`^Seq\.sumBy$`),        // Seq.sumBy f seq
	regexp.MustCompile(`^Seq\.min$`),          // Seq.min seq
	regexp.MustCompile(`^Seq\.max$`),          // Seq.max seq
	regexp.MustCompile(`^Seq\.init$`),         // Seq.init n f
	regexp.MustCompile(`^Seq\.initInfinite$`), // Seq.initInfinite f
	regexp.MustCompile(`^Seq\.unfold$`),       // Seq.unfold f state
	regexp.MustCompile(`^Seq\.scan$`),         // Seq.scan f init seq
	regexp.MustCompile(`^Seq\.pairwise$`),     // Seq.pairwise seq
	regexp.MustCompile(`^Seq\.windowed$`),     // Seq.windowed n seq
	regexp.MustCompile(`^Seq\.indexed$`),      // Seq.indexed seq
	regexp.MustCompile(`^Seq\.item$`),         // Seq.item i seq
	regexp.MustCompile(`^Seq\.nth$`),          // Seq.nth seq i (legacy)

	// ── Array module ─────────────────────────────────────────────────────
	regexp.MustCompile(`^Array\.map$`),        // Array.map f arr
	regexp.MustCompile(`^Array\.filter$`),     // Array.filter pred arr
	regexp.MustCompile(`^Array\.fold$`),       // Array.fold f init arr
	regexp.MustCompile(`^Array\.foldBack$`),   // Array.foldBack f arr init
	regexp.MustCompile(`^Array\.iter$`),       // Array.iter f arr
	regexp.MustCompile(`^Array\.iteri$`),      // Array.iteri f arr
	regexp.MustCompile(`^Array\.mapi$`),       // Array.mapi f arr
	regexp.MustCompile(`^Array\.collect$`),    // Array.collect f arr
	regexp.MustCompile(`^Array\.choose$`),     // Array.choose f arr
	regexp.MustCompile(`^Array\.forall$`),     // Array.forall pred arr
	regexp.MustCompile(`^Array\.exists$`),     // Array.exists pred arr
	regexp.MustCompile(`^Array\.find$`),       // Array.find pred arr
	regexp.MustCompile(`^Array\.tryFind$`),    // Array.tryFind pred arr
	regexp.MustCompile(`^Array\.length$`),     // Array.length arr
	regexp.MustCompile(`^Array\.isEmpty$`),    // Array.isEmpty arr
	regexp.MustCompile(`^Array\.append$`),     // Array.append arr1 arr2
	regexp.MustCompile(`^Array\.concat$`),     // Array.concat arrs
	regexp.MustCompile(`^Array\.rev$`),        // Array.rev arr
	regexp.MustCompile(`^Array\.sort$`),       // Array.sort arr
	regexp.MustCompile(`^Array\.sortBy$`),     // Array.sortBy key arr
	regexp.MustCompile(`^Array\.sortWith$`),   // Array.sortWith cmp arr
	regexp.MustCompile(`^Array\.sum$`),        // Array.sum arr
	regexp.MustCompile(`^Array\.sumBy$`),      // Array.sumBy f arr
	regexp.MustCompile(`^Array\.init$`),       // Array.init n f
	regexp.MustCompile(`^Array\.create$`),     // Array.create n x
	regexp.MustCompile(`^Array\.zeroCreate$`), // Array.zeroCreate n
	regexp.MustCompile(`^Array\.copy$`),       // Array.copy arr
	regexp.MustCompile(`^Array\.sub$`),        // Array.sub arr start len
	regexp.MustCompile(`^Array\.take$`),       // Array.take n arr
	regexp.MustCompile(`^Array\.skip$`),       // Array.skip n arr
	regexp.MustCompile(`^Array\.zip$`),        // Array.zip arr1 arr2
	regexp.MustCompile(`^Array\.unzip$`),      // Array.unzip arr
	regexp.MustCompile(`^Array\.partition$`),  // Array.partition pred arr
	regexp.MustCompile(`^Array\.distinct$`),   // Array.distinct arr
	regexp.MustCompile(`^Array\.groupBy$`),    // Array.groupBy key arr
	regexp.MustCompile(`^Array\.toList$`),     // Array.toList arr
	regexp.MustCompile(`^Array\.toSeq$`),      // Array.toSeq arr
	regexp.MustCompile(`^Array\.ofList$`),     // Array.ofList lst
	regexp.MustCompile(`^Array\.ofSeq$`),      // Array.ofSeq seq
	regexp.MustCompile(`^Array\.indexed$`),    // Array.indexed arr

	// ── Option module ────────────────────────────────────────────────────
	regexp.MustCompile(`^Option\.map$`),          // Option.map f opt
	regexp.MustCompile(`^Option\.bind$`),         // Option.bind f opt
	regexp.MustCompile(`^Option\.defaultValue$`), // Option.defaultValue def opt
	regexp.MustCompile(`^Option\.defaultWith$`),  // Option.defaultWith f opt
	regexp.MustCompile(`^Option\.filter$`),       // Option.filter pred opt
	regexp.MustCompile(`^Option\.count$`),        // Option.count opt
	regexp.MustCompile(`^Option\.fold$`),         // Option.fold f init opt
	regexp.MustCompile(`^Option\.foldBack$`),     // Option.foldBack f opt init
	regexp.MustCompile(`^Option\.forall$`),       // Option.forall pred opt
	regexp.MustCompile(`^Option\.exists$`),       // Option.exists pred opt
	regexp.MustCompile(`^Option\.iter$`),         // Option.iter f opt
	regexp.MustCompile(`^Option\.isNone$`),       // Option.isNone opt
	regexp.MustCompile(`^Option\.isSome$`),       // Option.isSome opt
	regexp.MustCompile(`^Option\.get$`),          // Option.get opt
	regexp.MustCompile(`^Option\.toArray$`),      // Option.toArray opt
	regexp.MustCompile(`^Option\.toList$`),       // Option.toList opt
	regexp.MustCompile(`^Option\.toNullable$`),   // Option.toNullable opt
	regexp.MustCompile(`^Option\.ofNullable$`),   // Option.ofNullable n
	regexp.MustCompile(`^Option\.ofObj$`),        // Option.ofObj obj
	regexp.MustCompile(`^Option\.toObj$`),        // Option.toObj opt
	regexp.MustCompile(`^Option\.flatten$`),      // Option.flatten opt
	regexp.MustCompile(`^Option\.orElse$`),       // Option.orElse alt opt
	regexp.MustCompile(`^Option\.orElseWith$`),   // Option.orElseWith f opt
	regexp.MustCompile(`^Option\.contains$`),     // Option.contains v opt

	// ── Result module ────────────────────────────────────────────────────
	regexp.MustCompile(`^Result\.map$`),          // Result.map f r
	regexp.MustCompile(`^Result\.mapError$`),     // Result.mapError f r
	regexp.MustCompile(`^Result\.bind$`),         // Result.bind f r
	regexp.MustCompile(`^Result\.isOk$`),         // Result.isOk r
	regexp.MustCompile(`^Result\.isError$`),      // Result.isError r
	regexp.MustCompile(`^Result\.defaultValue$`), // Result.defaultValue def r
	regexp.MustCompile(`^Result\.defaultWith$`),  // Result.defaultWith f r
	regexp.MustCompile(`^Result\.count$`),        // Result.count r
	regexp.MustCompile(`^Result\.fold$`),         // Result.fold ok err r
	regexp.MustCompile(`^Result\.foldBack$`),     // Result.foldBack ok err r init
	regexp.MustCompile(`^Result\.iter$`),         // Result.iter f r
	regexp.MustCompile(`^Result\.iterError$`),    // Result.iterError f r
	regexp.MustCompile(`^Result\.exists$`),       // Result.exists pred r
	regexp.MustCompile(`^Result\.forall$`),       // Result.forall pred r
	regexp.MustCompile(`^Result\.filter$`),       // Result.filter pred err r
	regexp.MustCompile(`^Result\.toArray$`),      // Result.toArray r
	regexp.MustCompile(`^Result\.toList$`),       // Result.toList r
	regexp.MustCompile(`^Result\.toOption$`),     // Result.toOption r
	regexp.MustCompile(`^Result\.ofOption$`),     // Result.ofOption err opt

	// ── Map module ───────────────────────────────────────────────────────
	regexp.MustCompile(`^Map\.add$`),         // Map.add k v m
	regexp.MustCompile(`^Map\.remove$`),      // Map.remove k m
	regexp.MustCompile(`^Map\.find$`),        // Map.find k m
	regexp.MustCompile(`^Map\.tryFind$`),     // Map.tryFind k m
	regexp.MustCompile(`^Map\.containsKey$`), // Map.containsKey k m
	regexp.MustCompile(`^Map\.map$`),         // Map.map f m
	regexp.MustCompile(`^Map\.filter$`),      // Map.filter pred m
	regexp.MustCompile(`^Map\.fold$`),        // Map.fold f init m
	regexp.MustCompile(`^Map\.iter$`),        // Map.iter f m
	regexp.MustCompile(`^Map\.toList$`),      // Map.toList m
	regexp.MustCompile(`^Map\.toSeq$`),       // Map.toSeq m
	regexp.MustCompile(`^Map\.toArray$`),     // Map.toArray m
	regexp.MustCompile(`^Map\.ofList$`),      // Map.ofList pairs
	regexp.MustCompile(`^Map\.ofSeq$`),       // Map.ofSeq pairs
	regexp.MustCompile(`^Map\.ofArray$`),     // Map.ofArray pairs
	regexp.MustCompile(`^Map\.isEmpty$`),     // Map.isEmpty m
	regexp.MustCompile(`^Map\.count$`),       // Map.count m
	regexp.MustCompile(`^Map\.keys$`),        // Map.keys m
	regexp.MustCompile(`^Map\.values$`),      // Map.values m
	regexp.MustCompile(`^Map\.merge$`),       // Map.merge m1 m2
	regexp.MustCompile(`^Map\.mergeWith$`),   // Map.mergeWith f m1 m2
	regexp.MustCompile(`^Map\.partition$`),   // Map.partition pred m
	regexp.MustCompile(`^Map\.exists$`),      // Map.exists pred m
	regexp.MustCompile(`^Map\.forall$`),      // Map.forall pred m
	regexp.MustCompile(`^Map\.pick$`),        // Map.pick f m
	regexp.MustCompile(`^Map\.tryPick$`),     // Map.tryPick f m
	regexp.MustCompile(`^Map\.change$`),      // Map.change k f m
	regexp.MustCompile(`^Map\.findKey$`),     // Map.findKey pred m
	regexp.MustCompile(`^Map\.tryFindKey$`),  // Map.tryFindKey pred m

	// ── Set module ───────────────────────────────────────────────────────
	regexp.MustCompile(`^Set\.add$`),        // Set.add x s
	regexp.MustCompile(`^Set\.remove$`),     // Set.remove x s
	regexp.MustCompile(`^Set\.contains$`),   // Set.contains x s
	regexp.MustCompile(`^Set\.union$`),      // Set.union s1 s2
	regexp.MustCompile(`^Set\.intersect$`),  // Set.intersect s1 s2
	regexp.MustCompile(`^Set\.difference$`), // Set.difference s1 s2
	regexp.MustCompile(`^Set\.isSubset$`),   // Set.isSubset s1 s2
	regexp.MustCompile(`^Set\.isEmpty$`),    // Set.isEmpty s
	regexp.MustCompile(`^Set\.count$`),      // Set.count s
	regexp.MustCompile(`^Set\.toList$`),     // Set.toList s
	regexp.MustCompile(`^Set\.toSeq$`),      // Set.toSeq s
	regexp.MustCompile(`^Set\.toArray$`),    // Set.toArray s
	regexp.MustCompile(`^Set\.ofList$`),     // Set.ofList lst
	regexp.MustCompile(`^Set\.ofSeq$`),      // Set.ofSeq seq
	regexp.MustCompile(`^Set\.ofArray$`),    // Set.ofArray arr
	regexp.MustCompile(`^Set\.map$`),        // Set.map f s
	regexp.MustCompile(`^Set\.filter$`),     // Set.filter pred s
	regexp.MustCompile(`^Set\.fold$`),       // Set.fold f init s
	regexp.MustCompile(`^Set\.foldBack$`),   // Set.foldBack f s init
	regexp.MustCompile(`^Set\.iter$`),       // Set.iter f s
	regexp.MustCompile(`^Set\.forall$`),     // Set.forall pred s
	regexp.MustCompile(`^Set\.exists$`),     // Set.exists pred s
	regexp.MustCompile(`^Set\.partition$`),  // Set.partition pred s
	regexp.MustCompile(`^Set\.minElement$`), // Set.minElement s
	regexp.MustCompile(`^Set\.maxElement$`), // Set.maxElement s

	// ── Async / Task patterns ─────────────────────────────────────────────
	regexp.MustCompile(`^Async\.RunSynchronously$`),   // Async.RunSynchronously async
	regexp.MustCompile(`^Async\.AwaitTask$`),          // Async.AwaitTask task
	regexp.MustCompile(`^Async\.AwaitWaitHandle$`),    // Async.AwaitWaitHandle wh
	regexp.MustCompile(`^Async\.Start$`),              // Async.Start async
	regexp.MustCompile(`^Async\.StartAsTask$`),        // Async.StartAsTask async
	regexp.MustCompile(`^Async\.Parallel$`),           // Async.Parallel asyncs
	regexp.MustCompile(`^Async\.Sequential$`),         // Async.Sequential asyncs
	regexp.MustCompile(`^Async\.Sleep$`),              // Async.Sleep ms
	regexp.MustCompile(`^Async\.Catch$`),              // Async.Catch async
	regexp.MustCompile(`^Async\.TryCancelled$`),       // Async.TryCancelled async handler
	regexp.MustCompile(`^Async\.CancellationToken$`),  // Async.CancellationToken
	regexp.MustCompile(`^Async\.CancelDefaultToken$`), // Async.CancelDefaultToken
	regexp.MustCompile(`^Async\.Ignore$`),             // Async.Ignore async
	regexp.MustCompile(`^Async\.map$`),                // Async.map f async

	// ── String / Printf ──────────────────────────────────────────────────
	regexp.MustCompile(`^String\.concat$`),             // String.concat sep strs
	regexp.MustCompile(`^String\.split$`),              // String.split sep str
	regexp.MustCompile(`^String\.join$`),               // String.Join sep strs (BCL)
	regexp.MustCompile(`^String\.IsNullOrEmpty$`),      // String.IsNullOrEmpty s
	regexp.MustCompile(`^String\.IsNullOrWhiteSpace$`), // String.IsNullOrWhiteSpace s

	// ── Giraffe HTTP handlers ────────────────────────────────────────────
	// These are highly Giraffe-specific and unlikely to appear in other languages.
	regexp.MustCompile(`^route$`),           // route "/path"
	regexp.MustCompile(`^routef$`),          // routef "/path/%i" handler
	regexp.MustCompile(`^routex$`),          // routex "pattern" handler
	regexp.MustCompile(`^routeStartsWith$`), // routeStartsWith prefix handler
	regexp.MustCompile(`^choose$`),          // choose [handler1; handler2]
	regexp.MustCompile(`^setStatusCode$`),   // setStatusCode 404
	regexp.MustCompile(`^setHttpHeader$`),   // setHttpHeader name value
	regexp.MustCompile(`^text$`),            // text "response body"
	regexp.MustCompile(`^json$`),            // json obj
	regexp.MustCompile(`^htmlString$`),      // htmlString "<html>..."
	regexp.MustCompile(`^htmlFile$`),        // htmlFile path
	regexp.MustCompile(`^negotiateWith$`),   // negotiateWith rules
	regexp.MustCompile(`^negotiate$`),       // negotiate obj
	regexp.MustCompile(`^redirectTo$`),      // redirectTo permanent url
	regexp.MustCompile(`^GET$`),             // GET >=>
	regexp.MustCompile(`^POST$`),            // POST >=>
	regexp.MustCompile(`^PUT$`),             // PUT >=>
	regexp.MustCompile(`^PATCH$`),           // PATCH >=>
	regexp.MustCompile(`^DELETE$`),          // DELETE >=>
	regexp.MustCompile(`^HEAD$`),            // HEAD >=>
	regexp.MustCompile(`^OPTIONS$`),         // OPTIONS >=>
	regexp.MustCompile(`^subRoute$`),        // subRoute prefix handler
	regexp.MustCompile(`^subRoutef$`),       // subRoutef fmt handler
	regexp.MustCompile(`^warbler$`),         // warbler (Giraffe task combinators)

	// ── F#-gated common names ────────────────────────────────────────────
	// These are common English words but in F# they specifically refer to
	// F# stdlib operations.
	regexp.MustCompile(`^printfn$`),    // printfn fmt args — F# stdout
	regexp.MustCompile(`^sprintf$`),    // sprintf fmt args — F# string format
	regexp.MustCompile(`^failwith$`),   // failwith msg — F# exception helper
	regexp.MustCompile(`^failwithf$`),  // failwithf fmt args
	regexp.MustCompile(`^invalidArg$`), // invalidArg paramName msg
	regexp.MustCompile(`^invalidOp$`),  // invalidOp msg
	regexp.MustCompile(`^nullArg$`),    // nullArg paramName
	regexp.MustCompile(`^raise$`),      // raise exn
	regexp.MustCompile(`^reraise$`),    // reraise()
	regexp.MustCompile(`^ignore$`),     // ignore x — F# value discard
	regexp.MustCompile(`^id$`),         // id x — identity function
	regexp.MustCompile(`^fst$`),        // fst (a, b) — tuple first
	regexp.MustCompile(`^snd$`),        // snd (a, b) — tuple second
	regexp.MustCompile(`^not$`),        // not b — boolean negate
	regexp.MustCompile(`^typeof$`),     // typeof<'T>
	regexp.MustCompile(`^typedefof$`),  // typedefof<'T>
	regexp.MustCompile(`^sizeof$`),     // sizeof<'T>
	regexp.MustCompile(`^nameof$`),     // nameof x
	regexp.MustCompile(`^box$`),        // box x — F# boxing
	regexp.MustCompile(`^unbox$`),      // unbox<'T> x — F# unboxing
	regexp.MustCompile(`^upcast$`),     // upcast x
	regexp.MustCompile(`^downcast$`),   // downcast<'T> x
}

func init() {
	dynamicPatternsByLang["fsharp"] = fsharpDynamicPatterns
}
