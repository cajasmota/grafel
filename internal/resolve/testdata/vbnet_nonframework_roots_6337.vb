' vbnet_nonframework_roots_6337.vb - the NEGATIVE half of the
' vbFrameworkRootNamespaces pin. Every dotted hierarchy target here MUST stay
' unclassified: each root is a name application code legitimately owns, so
' admitting it to vbFrameworkRootNamespaces would turn in-tree declarations
' into external placeholders.
'
' `My` is the load-bearing one. It is not merely "a name somebody might use" -
' it is the namespace the VB.NET COMPILER GENERATES into every project
' (`My.MyProject.MySettings`, `My.Application`, `My.Resources`). It is in-tree
' by construction, and it is exactly the root the #6473 widening mutant added.
'
' `Forms` is the near-miss: `Windows.Forms.TextBox` IS in the root set on the
' authority of its `Windows` root, and the tempting generalisation is to admit
' the second segment too. That would claim any application namespace called
' `Forms`, which is a common name for a folder of dialogs.
'
' See vbnet_framework_roots_6337.vb for the positive half and for why the two
' halves must not share a file.

Public Class ZzDottedMySettings
    Inherits My.MyProject.MySettings
End Class

Public Class ZzDottedMyApplication
    Inherits My.Application.BaseForm
End Class

Public Class ZzDottedFormsBase
    Inherits Forms.DialogBase
End Class

Public Class ZzDottedAppRoot
    Inherits StaxRip.UI.DialogBase
End Class

Public Class ZzDottedAccessibility
    Inherits Accessibility.Widgets.Base
End Class

Public Class ZzDottedMono
    Inherits Mono.Helpers.Thing
End Class
