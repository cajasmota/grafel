package resolve

import "regexp"

// ocamlDynamicPatterns are per-language patterns for OCaml.
// Registered via init() into dynamicPatternsByLang.
//
// The OCaml extractor (internal/extractors/ocaml/extractor.go) emits CALLS edges
// whose ToID is either:
//   - a bare function name: "helper"
//   - a module-qualified call: "List.map", "Hashtbl.add", "Lwt.bind"
//
// Two categories of patterns:
//
//  1. OCaml stdlib module-qualified identifiers — dotted names that are
//     highly characteristic of OCaml's standard library:
//     List: List.map, List.filter, List.fold_left, etc.
//     Hashtbl: Hashtbl.create, Hashtbl.add, Hashtbl.find, etc.
//     String: String.length, String.sub, String.split_on_char, etc.
//     Option: Option.map, Option.bind, Option.value, etc.
//     Result: Result.map, Result.bind, Result.get_ok, etc.
//     Array: Array.map, Array.filter, Array.fold_left, etc.
//     Functors: Map.Make, Set.Make (stdlib functor constructors)
//     Async/Lwt: Lwt.bind, Lwt.return, Lwt_main.run, Lwt_list.map_p, etc.
//     IO: Printf.printf, Format.printf, etc.
//
//  2. OCaml-gated common names — names that in OCaml context specifically
//     refer to stdlib operations.
//
// All patterns are gated to lang=="ocaml" (safer-bias rule).
var ocamlDynamicPatterns = []*regexp.Regexp{
	// ── List module ──────────────────────────────────────────────────────
	regexp.MustCompile(`^List\.map$`),           // List.map f lst
	regexp.MustCompile(`^List\.filter$`),        // List.filter pred lst
	regexp.MustCompile(`^List\.filter_map$`),    // List.filter_map f lst
	regexp.MustCompile(`^List\.fold_left$`),     // List.fold_left f init lst
	regexp.MustCompile(`^List\.fold_right$`),    // List.fold_right f lst init
	regexp.MustCompile(`^List\.iter$`),          // List.iter f lst
	regexp.MustCompile(`^List\.iteri$`),         // List.iteri f lst
	regexp.MustCompile(`^List\.mapi$`),          // List.mapi f lst
	regexp.MustCompile(`^List\.for_all$`),       // List.for_all pred lst
	regexp.MustCompile(`^List\.exists$`),        // List.exists pred lst
	regexp.MustCompile(`^List\.find$`),          // List.find pred lst
	regexp.MustCompile(`^List\.find_opt$`),      // List.find_opt pred lst
	regexp.MustCompile(`^List\.assoc$`),         // List.assoc key lst
	regexp.MustCompile(`^List\.assoc_opt$`),     // List.assoc_opt key lst
	regexp.MustCompile(`^List\.length$`),        // List.length lst
	regexp.MustCompile(`^List\.hd$`),            // List.hd lst
	regexp.MustCompile(`^List\.tl$`),            // List.tl lst
	regexp.MustCompile(`^List\.nth$`),           // List.nth lst n
	regexp.MustCompile(`^List\.nth_opt$`),       // List.nth_opt lst n
	regexp.MustCompile(`^List\.append$`),        // List.append lst1 lst2
	regexp.MustCompile(`^List\.concat$`),        // List.concat lsts
	regexp.MustCompile(`^List\.concat_map$`),    // List.concat_map f lst
	regexp.MustCompile(`^List\.rev$`),           // List.rev lst
	regexp.MustCompile(`^List\.rev_map$`),       // List.rev_map f lst
	regexp.MustCompile(`^List\.sort$`),          // List.sort cmp lst
	regexp.MustCompile(`^List\.stable_sort$`),   // List.stable_sort cmp lst
	regexp.MustCompile(`^List\.fast_sort$`),     // List.fast_sort cmp lst
	regexp.MustCompile(`^List\.sort_uniq$`),     // List.sort_uniq cmp lst
	regexp.MustCompile(`^List\.mem$`),           // List.mem x lst
	regexp.MustCompile(`^List\.memq$`),          // List.memq x lst
	regexp.MustCompile(`^List\.flatten$`),       // List.flatten lsts
	regexp.MustCompile(`^List\.split$`),         // List.split pairs
	regexp.MustCompile(`^List\.combine$`),       // List.combine lst1 lst2
	regexp.MustCompile(`^List\.partition$`),     // List.partition pred lst
	regexp.MustCompile(`^List\.partition_map$`), // List.partition_map f lst
	regexp.MustCompile(`^List\.init$`),          // List.init n f
	regexp.MustCompile(`^List\.to_seq$`),        // List.to_seq lst
	regexp.MustCompile(`^List\.of_seq$`),        // List.of_seq seq
	regexp.MustCompile(`^List\.remove_assoc$`),  // List.remove_assoc key lst

	// ── Hashtbl module ──────────────────────────────────────────────────
	regexp.MustCompile(`^Hashtbl\.create$`),   // Hashtbl.create n
	regexp.MustCompile(`^Hashtbl\.add$`),      // Hashtbl.add tbl key val
	regexp.MustCompile(`^Hashtbl\.find$`),     // Hashtbl.find tbl key
	regexp.MustCompile(`^Hashtbl\.find_opt$`), // Hashtbl.find_opt tbl key
	regexp.MustCompile(`^Hashtbl\.mem$`),      // Hashtbl.mem tbl key
	regexp.MustCompile(`^Hashtbl\.remove$`),   // Hashtbl.remove tbl key
	regexp.MustCompile(`^Hashtbl\.replace$`),  // Hashtbl.replace tbl key val
	regexp.MustCompile(`^Hashtbl\.iter$`),     // Hashtbl.iter f tbl
	regexp.MustCompile(`^Hashtbl\.fold$`),     // Hashtbl.fold f tbl init
	regexp.MustCompile(`^Hashtbl\.length$`),   // Hashtbl.length tbl
	regexp.MustCompile(`^Hashtbl\.clear$`),    // Hashtbl.clear tbl
	regexp.MustCompile(`^Hashtbl\.copy$`),     // Hashtbl.copy tbl
	regexp.MustCompile(`^Hashtbl\.reset$`),    // Hashtbl.reset tbl
	regexp.MustCompile(`^Hashtbl\.to_seq$`),   // Hashtbl.to_seq tbl
	regexp.MustCompile(`^Hashtbl\.of_seq$`),   // Hashtbl.of_seq seq

	// ── String module ───────────────────────────────────────────────────
	regexp.MustCompile(`^String\.length$`),           // String.length s
	regexp.MustCompile(`^String\.sub$`),              // String.sub s pos len
	regexp.MustCompile(`^String\.concat$`),           // String.concat sep lst
	regexp.MustCompile(`^String\.split_on_char$`),    // String.split_on_char sep s
	regexp.MustCompile(`^String\.trim$`),             // String.trim s
	regexp.MustCompile(`^String\.uppercase_ascii$`),  // String.uppercase_ascii s
	regexp.MustCompile(`^String\.lowercase_ascii$`),  // String.lowercase_ascii s
	regexp.MustCompile(`^String\.capitalize_ascii$`), // String.capitalize_ascii s
	regexp.MustCompile(`^String\.contains$`),         // String.contains s c
	regexp.MustCompile(`^String\.get$`),              // String.get s i
	regexp.MustCompile(`^String\.make$`),             // String.make n c
	regexp.MustCompile(`^String\.init$`),             // String.init n f
	regexp.MustCompile(`^String\.copy$`),             // String.copy s (deprecated but present)
	regexp.MustCompile(`^String\.index$`),            // String.index s c
	regexp.MustCompile(`^String\.index_opt$`),        // String.index_opt s c
	regexp.MustCompile(`^String\.rindex$`),           // String.rindex s c
	regexp.MustCompile(`^String\.rindex_opt$`),       // String.rindex_opt s c
	regexp.MustCompile(`^String\.to_seq$`),           // String.to_seq s
	regexp.MustCompile(`^String\.of_seq$`),           // String.of_seq seq
	regexp.MustCompile(`^String\.starts_with$`),      // String.starts_with ~prefix s
	regexp.MustCompile(`^String\.ends_with$`),        // String.ends_with ~suffix s

	// ── Option module ───────────────────────────────────────────────────
	regexp.MustCompile(`^Option\.map$`),       // Option.map f opt
	regexp.MustCompile(`^Option\.bind$`),      // Option.bind opt f
	regexp.MustCompile(`^Option\.value$`),     // Option.value opt ~default
	regexp.MustCompile(`^Option\.get$`),       // Option.get opt
	regexp.MustCompile(`^Option\.fold$`),      // Option.fold ~none ~some opt
	regexp.MustCompile(`^Option\.iter$`),      // Option.iter f opt
	regexp.MustCompile(`^Option\.is_none$`),   // Option.is_none opt
	regexp.MustCompile(`^Option\.is_some$`),   // Option.is_some opt
	regexp.MustCompile(`^Option\.to_result$`), // Option.to_result ~none opt
	regexp.MustCompile(`^Option\.to_list$`),   // Option.to_list opt
	regexp.MustCompile(`^Option\.to_seq$`),    // Option.to_seq opt
	regexp.MustCompile(`^Option\.equal$`),     // Option.equal f opt1 opt2
	regexp.MustCompile(`^Option\.compare$`),   // Option.compare f opt1 opt2

	// ── Result module ───────────────────────────────────────────────────
	regexp.MustCompile(`^Result\.map$`),        // Result.map f r
	regexp.MustCompile(`^Result\.map_error$`),  // Result.map_error f r
	regexp.MustCompile(`^Result\.bind$`),       // Result.bind r f
	regexp.MustCompile(`^Result\.fold$`),       // Result.fold ~ok ~error r
	regexp.MustCompile(`^Result\.iter$`),       // Result.iter f r
	regexp.MustCompile(`^Result\.iter_error$`), // Result.iter_error f r
	regexp.MustCompile(`^Result\.is_ok$`),      // Result.is_ok r
	regexp.MustCompile(`^Result\.is_error$`),   // Result.is_error r
	regexp.MustCompile(`^Result\.get_ok$`),     // Result.get_ok r
	regexp.MustCompile(`^Result\.get_error$`),  // Result.get_error r
	regexp.MustCompile(`^Result\.to_option$`),  // Result.to_option r
	regexp.MustCompile(`^Result\.to_list$`),    // Result.to_list r
	regexp.MustCompile(`^Result\.to_seq$`),     // Result.to_seq r
	regexp.MustCompile(`^Result\.equal$`),      // Result.equal ok_eq err_eq r1 r2
	regexp.MustCompile(`^Result\.compare$`),    // Result.compare ok_cmp err_cmp r1 r2

	// ── Array module ────────────────────────────────────────────────────
	regexp.MustCompile(`^Array\.map$`),         // Array.map f arr
	regexp.MustCompile(`^Array\.filter$`),      // Array.filter pred arr
	regexp.MustCompile(`^Array\.filter_map$`),  // Array.filter_map f arr
	regexp.MustCompile(`^Array\.fold_left$`),   // Array.fold_left f init arr
	regexp.MustCompile(`^Array\.fold_right$`),  // Array.fold_right f arr init
	regexp.MustCompile(`^Array\.iter$`),        // Array.iter f arr
	regexp.MustCompile(`^Array\.iteri$`),       // Array.iteri f arr
	regexp.MustCompile(`^Array\.mapi$`),        // Array.mapi f arr
	regexp.MustCompile(`^Array\.for_all$`),     // Array.for_all pred arr
	regexp.MustCompile(`^Array\.exists$`),      // Array.exists pred arr
	regexp.MustCompile(`^Array\.find$`),        // Array.find pred arr
	regexp.MustCompile(`^Array\.find_opt$`),    // Array.find_opt pred arr
	regexp.MustCompile(`^Array\.length$`),      // Array.length arr
	regexp.MustCompile(`^Array\.get$`),         // Array.get arr i
	regexp.MustCompile(`^Array\.set$`),         // Array.set arr i v
	regexp.MustCompile(`^Array\.make$`),        // Array.make n x
	regexp.MustCompile(`^Array\.init$`),        // Array.init n f
	regexp.MustCompile(`^Array\.copy$`),        // Array.copy arr
	regexp.MustCompile(`^Array\.sub$`),         // Array.sub arr pos len
	regexp.MustCompile(`^Array\.append$`),      // Array.append arr1 arr2
	regexp.MustCompile(`^Array\.concat$`),      // Array.concat arrs
	regexp.MustCompile(`^Array\.sort$`),        // Array.sort cmp arr
	regexp.MustCompile(`^Array\.stable_sort$`), // Array.stable_sort cmp arr
	regexp.MustCompile(`^Array\.to_list$`),     // Array.to_list arr
	regexp.MustCompile(`^Array\.of_list$`),     // Array.of_list lst
	regexp.MustCompile(`^Array\.to_seq$`),      // Array.to_seq arr
	regexp.MustCompile(`^Array\.of_seq$`),      // Array.of_seq seq

	// ── Functor constructors (Map.Make, Set.Make) ──────────────────────
	regexp.MustCompile(`^Map\.Make$`),      // Map.Make(OrderedType)
	regexp.MustCompile(`^Map\.find$`),      // Map.find key m (after Make)
	regexp.MustCompile(`^Map\.add$`),       // Map.add key val m
	regexp.MustCompile(`^Map\.remove$`),    // Map.remove key m
	regexp.MustCompile(`^Map\.mem$`),       // Map.mem key m
	regexp.MustCompile(`^Map\.empty$`),     // Map.empty
	regexp.MustCompile(`^Map\.singleton$`), // Map.singleton key val
	regexp.MustCompile(`^Map\.iter$`),      // Map.iter f m
	regexp.MustCompile(`^Map\.fold$`),      // Map.fold f m init
	regexp.MustCompile(`^Map\.map$`),       // Map.map f m
	regexp.MustCompile(`^Map\.filter$`),    // Map.filter pred m
	regexp.MustCompile(`^Map\.mapi$`),      // Map.mapi f m
	regexp.MustCompile(`^Map\.merge$`),     // Map.merge f m1 m2
	regexp.MustCompile(`^Map\.union$`),     // Map.union f m1 m2
	regexp.MustCompile(`^Map\.bindings$`),  // Map.bindings m
	regexp.MustCompile(`^Map\.cardinal$`),  // Map.cardinal m
	regexp.MustCompile(`^Map\.find_opt$`),  // Map.find_opt key m
	regexp.MustCompile(`^Set\.Make$`),      // Set.Make(OrderedType)
	regexp.MustCompile(`^Set\.add$`),       // Set.add x s
	regexp.MustCompile(`^Set\.remove$`),    // Set.remove x s
	regexp.MustCompile(`^Set\.mem$`),       // Set.mem x s
	regexp.MustCompile(`^Set\.empty$`),     // Set.empty
	regexp.MustCompile(`^Set\.singleton$`), // Set.singleton x
	regexp.MustCompile(`^Set\.union$`),     // Set.union s1 s2
	regexp.MustCompile(`^Set\.inter$`),     // Set.inter s1 s2
	regexp.MustCompile(`^Set\.diff$`),      // Set.diff s1 s2
	regexp.MustCompile(`^Set\.iter$`),      // Set.iter f s
	regexp.MustCompile(`^Set\.fold$`),      // Set.fold f s init
	regexp.MustCompile(`^Set\.filter$`),    // Set.filter pred s
	regexp.MustCompile(`^Set\.cardinal$`),  // Set.cardinal s
	regexp.MustCompile(`^Set\.elements$`),  // Set.elements s
	regexp.MustCompile(`^Set\.of_list$`),   // Set.of_list lst
	regexp.MustCompile(`^Set\.to_list$`),   // Set.to_list s

	// ── Lwt async library ───────────────────────────────────────────────
	regexp.MustCompile(`^Lwt\.bind$`),             // Lwt.bind t f  (or >>= operator)
	regexp.MustCompile(`^Lwt\.return$`),           // Lwt.return v
	regexp.MustCompile(`^Lwt\.return_unit$`),      // Lwt.return_unit
	regexp.MustCompile(`^Lwt\.return_none$`),      // Lwt.return_none
	regexp.MustCompile(`^Lwt\.return_some$`),      // Lwt.return_some v
	regexp.MustCompile(`^Lwt\.return_ok$`),        // Lwt.return_ok v
	regexp.MustCompile(`^Lwt\.return_error$`),     // Lwt.return_error e
	regexp.MustCompile(`^Lwt\.map$`),              // Lwt.map f t
	regexp.MustCompile(`^Lwt\.both$`),             // Lwt.both t1 t2
	regexp.MustCompile(`^Lwt\.join$`),             // Lwt.join ts
	regexp.MustCompile(`^Lwt\.all$`),              // Lwt.all ts
	regexp.MustCompile(`^Lwt\.pick$`),             // Lwt.pick ts
	regexp.MustCompile(`^Lwt\.choose$`),           // Lwt.choose ts
	regexp.MustCompile(`^Lwt\.catch$`),            // Lwt.catch f handler
	regexp.MustCompile(`^Lwt\.finalize$`),         // Lwt.finalize t f
	regexp.MustCompile(`^Lwt\.try_bind$`),         // Lwt.try_bind f ok err
	regexp.MustCompile(`^Lwt\.async$`),            // Lwt.async f
	regexp.MustCompile(`^Lwt\.dont_wait$`),        // Lwt.dont_wait f
	regexp.MustCompile(`^Lwt\.pause$`),            // Lwt.pause ()
	regexp.MustCompile(`^Lwt\.fail$`),             // Lwt.fail exn
	regexp.MustCompile(`^Lwt\.fail_with$`),        // Lwt.fail_with msg
	regexp.MustCompile(`^Lwt_main\.run$`),         // Lwt_main.run t
	regexp.MustCompile(`^Lwt_list\.map_p$`),       // Lwt_list.map_p f lst
	regexp.MustCompile(`^Lwt_list\.map_s$`),       // Lwt_list.map_s f lst
	regexp.MustCompile(`^Lwt_list\.iter_p$`),      // Lwt_list.iter_p f lst
	regexp.MustCompile(`^Lwt_list\.iter_s$`),      // Lwt_list.iter_s f lst
	regexp.MustCompile(`^Lwt_list\.filter_p$`),    // Lwt_list.filter_p pred lst
	regexp.MustCompile(`^Lwt_list\.filter_s$`),    // Lwt_list.filter_s pred lst
	regexp.MustCompile(`^Lwt_list\.fold_left_s$`), // Lwt_list.fold_left_s f init lst
	regexp.MustCompile(`^Lwt_unix\.sleep$`),       // Lwt_unix.sleep t
	regexp.MustCompile(`^Lwt_unix\.openfile$`),    // Lwt_unix.openfile path flags perm
	regexp.MustCompile(`^Lwt_unix\.close$`),       // Lwt_unix.close fd
	regexp.MustCompile(`^Lwt_unix\.read$`),        // Lwt_unix.read fd buf off len
	regexp.MustCompile(`^Lwt_unix\.write$`),       // Lwt_unix.write fd buf off len
	regexp.MustCompile(`^Lwt_io\.read_line$`),     // Lwt_io.read_line ic
	regexp.MustCompile(`^Lwt_io\.write_line$`),    // Lwt_io.write_line oc s
	regexp.MustCompile(`^Lwt_io\.printf$`),        // Lwt_io.printf fmt args

	// ── Printf / Format / IO ────────────────────────────────────────────
	regexp.MustCompile(`^Printf\.printf$`),          // Printf.printf fmt args
	regexp.MustCompile(`^Printf\.sprintf$`),         // Printf.sprintf fmt args
	regexp.MustCompile(`^Printf\.fprintf$`),         // Printf.fprintf oc fmt args
	regexp.MustCompile(`^Printf\.eprintf$`),         // Printf.eprintf fmt args
	regexp.MustCompile(`^Format\.printf$`),          // Format.printf fmt args
	regexp.MustCompile(`^Format\.sprintf$`),         // Format.sprintf fmt args
	regexp.MustCompile(`^Format\.fprintf$`),         // Format.fprintf ppf fmt args
	regexp.MustCompile(`^Format\.eprintf$`),         // Format.eprintf fmt args
	regexp.MustCompile(`^Format\.pp_print_string$`), // Format.pp_print_string ppf s
	regexp.MustCompile(`^Format\.pp_print_int$`),    // Format.pp_print_int ppf n
	regexp.MustCompile(`^Format\.pp_print_list$`),   // Format.pp_print_list sep pp ppf lst

	// ── Bytes / Buffer ──────────────────────────────────────────────────
	regexp.MustCompile(`^Bytes\.create$`),      // Bytes.create n
	regexp.MustCompile(`^Bytes\.length$`),      // Bytes.length b
	regexp.MustCompile(`^Bytes\.get$`),         // Bytes.get b i
	regexp.MustCompile(`^Bytes\.set$`),         // Bytes.set b i c
	regexp.MustCompile(`^Bytes\.copy$`),        // Bytes.copy b
	regexp.MustCompile(`^Bytes\.to_string$`),   // Bytes.to_string b
	regexp.MustCompile(`^Bytes\.of_string$`),   // Bytes.of_string s
	regexp.MustCompile(`^Buffer\.create$`),     // Buffer.create n
	regexp.MustCompile(`^Buffer\.add_string$`), // Buffer.add_string b s
	regexp.MustCompile(`^Buffer\.add_char$`),   // Buffer.add_char b c
	regexp.MustCompile(`^Buffer\.contents$`),   // Buffer.contents b
	regexp.MustCompile(`^Buffer\.length$`),     // Buffer.length b
	regexp.MustCompile(`^Buffer\.clear$`),      // Buffer.clear b
}

func init() {
	dynamicPatternsByLang["ocaml"] = ocamlDynamicPatterns
}
