package com.example.platform

// #6805 fence — the COMMON half of a Kotlin Multiplatform expect/actual pair.
// `expect fun` / `expect class` are what internal/engine/rules/kotlin/
// frameworks/kmp.yaml turns into `Interface` entities; the androidMain twin
// turns into `Implementation` entities and the IMPLEMENTS edge joins them.
//
// Note what is deliberately NOT here: `nowMillis` carries no `expect`
// keyword. That is not an omission, it is the Kotlin language rule — members
// of an `expect class` are implicitly expected and repeating the modifier is
// a compile error. So the androidMain `actual fun nowMillis` has no `Interface`
// counterpart in the graph, which is exactly the shape that used to produce
// the `nowMillis IMPLEMENTS nowMillis` self-loop.

expect fun platformName(): String

expect class Clock {
    fun nowMillis(): Long
}
