## The over-firing half of the fixture.
##
## `of` is four different things in Nim: an inheritance clause, an object-
## variant branch, a `case` statement branch, and a runtime type test. All four
## appear here and only the first is inheritance.
##
## Which of them the gate actually FIRES on is recorded per row in
## expected.json, not claimed here: Payload (variant branch) and Cursor (type
## test) are the two this file contributes that a single-edit mutant trips.
## Node deliberately carries a real base AND a variant section on one
## declaration, which is why its variant rows are absence fences rather than
## mutant-killed rows — its first `of` is its base.

type
  NodeKind* = enum
    nkInt, nkStr

  Node* = ref object of RootObj
    name*: string
    case kind*: NodeKind
    of nkInt:
      intVal*: int
    of nkStr:
      strVal*: string

  Payload* = object
    ## No base at all, and a variant section immediately after the `object`
    ## keyword. This is the type that catches an unanchored search: Node's
    ## first `of` is its real base, so a producer that scans the whole
    ## declaration still gets Node right and only Payload wrong.
    case tag*: NodeKind
    of nkInt:
      num*: int
    of nkStr:
      text*: string

  Span* = object of RootObj
    ## The only NON-ref inheriting type in the fixture: `object of` without
    ## `ref`. Without it a producer that handled only `ref object` would still
    ## score 100%.
    lo*: int
    hi*: int

  Cursor* = object
    pos*: int

proc atNode*(x: RootRef): bool =
  ## A runtime type TEST — the third thing Nim spells `of`. Cursor is the type
  ## declared immediately above, so a forward search from Cursor lands here.
  x of Node

proc describe*(n: Node): string =
  case n.kind
  of nkInt: "int:" & $n.intVal
  of nkStr: "str:" & n.strVal

proc isNode*(x: RootRef): bool =
  x of Node
