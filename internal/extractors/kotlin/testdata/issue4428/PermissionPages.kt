// Representative Kotlin permission source-of-truth (#4428). Mirrors the v3
// PermissionPage map: an `object` const-val group, a top-level `mapOf` const
// map, and an `enum class` with constructor values. The hyphenated literal
// values are the drift a downstream cross-graph parity-audit reads from the
// structured members_json.
package com.example.auth

object PermissionPages {
    const val CORE_ADMIN = "core-admin"
    const val BILLING = "billing"
    const val REPORTS = "reports"
}

val PAGE_LABELS = mapOf(
    "core-admin" to "Core Admin",
    "billing" to "Billing",
    "reports" to "Reports",
)

enum class PageGroup(val slug: String) {
    ADMIN("core-admin"),
    FINANCE("billing"),
}
