package com.example.config

// Representative Scala source-of-truth constant collections + enumerations
// for #4432. Each shape below is expected to surface as a SCOPE.Enum value-set.

// (2) object holding a GROUP of constant vals
object Pages {
  val CoreAdmin = "core-admin"
  val Billing: String = "billing"
  val Reports = "reports"
}

// (1) top-level const Map literal (string keys, mixed literal/dynamic values)
val Routes = Map(
  "home" -> "/",
  "admin" -> "/admin",
  "fallback" -> defaultRoute()
)

// (3) Scala 3 enum
enum Color {
  case Red, Green, Blue
}

// (4) sealed-trait + case-object enumeration
sealed trait Status
case object Active extends Status
case object Inactive extends Status
case object Pending extends Status
