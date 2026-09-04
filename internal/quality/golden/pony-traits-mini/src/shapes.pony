use "collections"

class Circle is Named
  let radius: F64

  new create(r: F64) =>
    radius = r

  fun name(): String => "circle"

class Square is (Named & Sized)
  let side: F64

  new create(s: F64) =>
    side = s

  fun name(): String => "square"

  fun size(): USize => 4

class Box[A: Any val] is Sized
  fun size(): USize => 1

class Plain
  let label: String

  new create(l: String) =>
    label = l

  fun same(other: Plain box): Bool =>
    this is other
