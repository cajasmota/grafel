package com.example.platform

// #6805 fence — the PLATFORM half. See commonMain/kotlin/Platform.kt.

actual fun platformName(): String = "android"

actual class Clock {
    actual fun nowMillis(): Long = 0L
}
