## Repository types: a RootObj-rooted base and one in-file subtype, plus a
## plain object with no `of` clause at all.

import tables, strutils

type
  Repository* = ref object of RootObj
    name*: string

  MemoryRepository* = ref object of Repository
    items*: Table[string, string]

  Config* = object
    host*: string
    port*: int
    # Config deliberately declares no base. The next words are a decoy for any
    # producer that is comment-blind: of PhantomBase

proc get*(r: MemoryRepository, key: string): string =
  result = r.items.getOrDefault(key, "")

proc put*(r: MemoryRepository, key: string, value: string) =
  r.items[key] = value

proc label*(c: Config): string =
  c.host & ":" & $c.port

proc normalise*(key: string): string =
  key.strip().toLowerAscii()

const
  httpPort* = 80
  httpsPort* = 443

proc scheme*(c: Config): string =
  ## A `case` STATEMENT whose branches are spelled `of`, sitting AFTER the last
  ## type declaration in the file. Config declares no base, so a producer that
  ## scrubbed comments but still searched FORWARD from Config would bind Config
  ## to httpPort here.
  case c.port
  of httpPort: "http"
  of httpsPort: "https"
  else: "other"
