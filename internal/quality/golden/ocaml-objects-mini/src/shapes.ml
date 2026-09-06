(* Geometry shapes as OCaml classes.
   This file carries the POSITIVE case the fixture exists for (#6812):
   `inherit` inside `object ... end`, where it genuinely is class inheritance. *)

open Printf

type point = { px : float; py : float }

type bounds = { low : point; high : point }

class virtual shape (tag : string) = object (self)
  method tag = tag
  method virtual area : float
  method describe = sprintf "%s area=%f" self#tag self#area
end

class circle radius = object
  inherit shape "circle"
  val r = radius
  method area = 3.14159 *. r *. r
end

class square side = object
  inherit shape "square" as super
  val s = side
  method area = s *. s
  method describe = "square " ^ super#describe
end

(* Multiple inheritance: OCaml objects may inherit more than one parent. *)
class virtual labelled = object
  method virtual tag : string
  method banner = "[" ^ "shape" ^ "]"
end

class tagged_circle radius = object
  inherit circle radius
  inherit labelled
  method banner = "[circle]"
end

let area_of sh = sh#area

let describe_all shapes =
  List.map (fun sh -> sh#describe) shapes

let total_area shapes =
  List.fold_left (fun acc sh -> acc +. area_of sh) 0.0 shapes
