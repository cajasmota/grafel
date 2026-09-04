use "collections"

trait Named
  fun name(): String

interface Sized
  fun size(): USize

trait Loud is Named
  fun shout(): String => "HEY"
