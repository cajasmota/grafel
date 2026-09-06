(* Objects built inside function bodies — the axis where `inherit` sits inside
   a `let` body rather than at a class declaration — plus three identifiers
   that vary how a depth walker keying on `end` mis-tokenises them. #6812

   `backend` and `legend` END with `end`, so Go's `\bend\b` re-run against the
   remaining slice DOES match them. `append_all` does not: `_` is a word
   character to Go's `\b`, so `end_all` never matches. The three are here to
   put both outcomes of that hazard in the corpus, and the difference between
   them is the point — an earlier version spelled all three with a trailing
   `_word` and so contained only the non-triggering case while claiming to
   vary the position. *)

open Buffer

type level = Debug | Info | Error

class virtual stream_writer prefix = object
  method virtual write : string -> unit
  method prefix = prefix
end

(* Nested object inside a function body, with `inherit` in it. *)
let make_logger prefix =
  object (self)
    inherit stream_writer prefix
    val mutable count = 0
    method write msg = print_string (prefix ^ msg)
    method log msg = self#write msg
    method total = count
  end

(* Nested object with NO `inherit` — the negative control for the axis above. *)
let make_counter start =
  object
    val mutable n = start
    method bump = n <- n + 1
    method value = n
  end

let append_all buf parts =
  List.iter (fun p -> Buffer.add_string buf p) parts

let backend lvl =
  match lvl with
  | Debug -> "debug"
  | Info -> "info"
  | Error -> "error"

let legend parts = String.concat ", " parts
