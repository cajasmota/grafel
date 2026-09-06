(* Modules, functors, includes and class types.
   Everything in this file LOOKS like inheritance to a keyword scanner and,
   except for the class-type `inherit`, is not class inheritance at all. #6812 *)

open List

module type Comparable = sig
  type t
  val compare : t -> t -> int
end

module Base = struct
  let name = "base"
  let describe () = "base container"
end

module Extended = struct
  (* `include` splices a module's contents in. It is NOT class inheritance,
     and it is not an import either — Base is already in scope. *)
  include Base
  let extra = "extra"
end

module Make (C : Comparable) = struct
  type item = C.t
  let sort_items xs = List.sort C.compare xs
  let append_item xs x = List.append xs [x]
end

(* A class TYPE with `inherit`: the keyword appears outside `object ... end`
   in the value sense, yet still denotes a class-type relation. This is the
   context a regex depth walker cannot tell apart from the object form. *)
class type printer = object
  method print : string -> unit
end

class type verbose_printer = object
  inherit printer
  method verbose : bool
end

let render_with (p : printer) msg = p#print msg

let compare_lengths a b = Stdlib.compare (List.length a) (List.length b)
