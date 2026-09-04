## Error hierarchy for the mini service.
##
## Every type here inherits, so this file is the fixture's positive half for
## EXTENDS (#6370): one root that hangs off the stdlib's CatchableError, and two
## leaves that hang off a base declared in this same file.

type
  AppError* = ref object of CatchableError
    code*: int

  NotFoundError* = ref object of AppError
    resource*: string

  ValidationError* = ref object of AppError
    field*: string

# type Ghost = ref object of Widget
#
# Deliberately commented out. This extractor has no comment awareness of its
# own; the `#` is what keeps `type` off the start of the line. Graded by the
# forbidden_entities row for Ghost and the forbidden edge Ghost -> Widget.

proc describe*(e: AppError): string =
  "AppError(" & $e.code & ")"

proc notFound*(resource: string): NotFoundError =
  result = NotFoundError(code: 404, resource: resource)
