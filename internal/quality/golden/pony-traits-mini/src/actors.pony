use "collections"

actor Worker is Named
  var count: USize = 0

  be work(item: String) =>
    count = count + 1

  fun name(): String => "worker"

primitive Zero is Named
  fun name(): String => "zero"

struct Point is Sized
  var x: F64 = 0

  fun size(): USize => 2

// class Ghost is Phantom

class Checker
  fun check(a: Worker tag, b: Worker tag): Bool =>
    a is b

  fun doc(): String => "class Spectre is Phantom"
