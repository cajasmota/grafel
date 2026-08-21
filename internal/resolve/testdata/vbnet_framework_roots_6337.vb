' vbnet_framework_roots_6337.vb - the POSITIVE half of the fixture pair that
' makes vbFrameworkRootNamespaces (internal/resolve/refs.go) load-bearing IN CI.
'
' vbExternalBaseTypes had a both-directions pin from #6327 day one. Its dotted
' sibling, vbFrameworkRootNamespaces, had NONE - and it gates the higher-volume
' half of the classification, because every `System.*` / `Microsoft.*` /
' `Windows.*` hierarchy target in the corpus is decided by this three-entry set
' rather than by any curated type name. A widening mutant that added `My` and
' `Forms` to the set kept the whole suite green (#6473 review, mutant W4).
'
' That is not hypothetical. `My` is VB.NET's COMPILER-GENERATED, IN-TREE
' namespace: `My.MyProject.MySettings` is code the VB compiler emits into the
' project itself. Admitting it to the root set would convert generated in-tree
' declarations into external placeholders and improve the resolution metric by
' doing so, silently. The PR's own note that `Accessibility` and `Mono` were
' "unmeasured guesses, removed" is the same failure caught by hand.
'
' The pin is SET EQUALITY over the root segments observed here:
' TestVBFrameworkRootNamespacesAreLoadBearing extracts this file with the real
' vbnet extractor, resolves it, collects the DOTTED EXTENDS / IMPLEMENTS targets
' that stay unresolved, takes the segment before the first `.`, and requires
' that set to equal vbFrameworkRootNamespaces exactly. So it fails in BOTH
' directions - a root added to the map with no clause here, and a clause here
' with no entry in the map.
'
' The must-NOT-classify half lives in vbnet_nonframework_roots_6337.vb, which
' is deliberately a separate file: if the negatives lived here, set equality
' would accept `My` the moment somebody added it to the map.
'
' Declaring classes are prefixed Zz so no name declared here can collide with a
' name referenced here.

Public Class ZzDottedSystemForm
    Inherits System.Windows.Forms.Form
End Class

Public Class ZzDottedSystemSettings
    Inherits System.Configuration.ApplicationSettingsBase
End Class

Public Class ZzDottedMicrosoftSafeHandle
    Inherits Microsoft.Win32.SafeHandles.SafeHandleZeroOrMinusOneIsInvalid
End Class

Public Class ZzDottedWindowsTextBox
    Inherits Windows.Forms.TextBox
End Class
