' vbnet_external_basetypes_6327.vb - the fixture that makes
' vbExternalBaseTypes (internal/resolve/refs.go) load-bearing IN CI.
'
' One clause per allow-list entry, and NOTHING else. The pin is set equality:
' TestVBExternalBaseTypesAreLoadBearing extracts this file with the real vbnet
' extractor, resolves it, collects the bare EXTENDS / IMPLEMENTS targets that
' stay unresolved, and requires that set to equal vbExternalBaseTypes exactly.
' So it fails in BOTH directions - an entry added to the map with no clause
' here, and a clause here with no entry in the map.
'
' WHY A FIXTURE AND NOT THE CORPUS. The first version of that test derived the
' set from $GRAFEL_VBNET_CORPUS and skipped when it was unset. That variable
' appears nowhere in .github/ or the Makefile, so neither direction ever ran in
' CI: a bogus entry passed, and deleting a real one passed. A gate that
' silently does not run is the #6363 shape, and this is the second time this
' epic has hit it.
'
' The clauses were generated once from the map and are now INDEPENDENT of it.
' Editing the map without editing this file is exactly what the test catches,
' so do not regenerate this file to make a failure go away - decide whether the
' entry or the clause is the wrong one.
'
' The declaring classes are prefixed ZzBase / ZzImpl so that no name declared
' here can collide with a base type named here; the arm's nameExists guard
' would otherwise legitimately refuse to classify it.

Public Class ZzBaseButton
    Inherits Button
End Class

Public Class ZzBaseCheckBox
    Inherits CheckBox
End Class

Public Class ZzBaseComboBox
    Inherits ComboBox
End Class

Public Class ZzBaseCommonDialog
    Inherits CommonDialog
End Class

Public Class ZzBaseContextMenuStrip
    Inherits ContextMenuStrip
End Class

Public Class ZzBaseControl
    Inherits Control
End Class

Public Class ZzBaseDataGridView
    Inherits DataGridView
End Class

Public Class ZzBaseFlowLayoutPanel
    Inherits FlowLayoutPanel
End Class

Public Class ZzBaseForm
    Inherits Form
End Class

Public Class ZzBaseGroupBox
    Inherits GroupBox
End Class

Public Class ZzBaseLabel
    Inherits Label
End Class

Public Class ZzBaseListBox
    Inherits ListBox
End Class

Public Class ZzBaseListView
    Inherits ListView
End Class

Public Class ZzBasePanel
    Inherits Panel
End Class

Public Class ZzBasePropertyGrid
    Inherits PropertyGrid
End Class

Public Class ZzBaseRichTextBox
    Inherits RichTextBox
End Class

Public Class ZzBaseTabControl
    Inherits TabControl
End Class

Public Class ZzBaseTabPage
    Inherits TabPage
End Class

Public Class ZzBaseTableLayoutPanel
    Inherits TableLayoutPanel
End Class

Public Class ZzBaseTextBox
    Inherits TextBox
End Class

Public Class ZzBaseToolStrip
    Inherits ToolStrip
End Class

Public Class ZzBaseToolStripButton
    Inherits ToolStripButton
End Class

Public Class ZzBaseToolStripMenuItem
    Inherits ToolStripMenuItem
End Class

Public Class ZzBaseToolStripSystemRenderer
    Inherits ToolStripSystemRenderer
End Class

Public Class ZzBaseTrackBar
    Inherits TrackBar
End Class

Public Class ZzBaseTreeView
    Inherits TreeView
End Class

Public Class ZzBaseUserControl
    Inherits UserControl
End Class

Public Class ZzBaseVScrollBar
    Inherits VScrollBar
End Class

Public Class ZzImplIWin32Window
    Implements IWin32Window
End Class

Public Class ZzBaseApplicationException
    Inherits ApplicationException
End Class

Public Class ZzBaseAttribute
    Inherits Attribute
End Class

Public Class ZzBaseCategoryAttribute
    Inherits CategoryAttribute
End Class

Public Class ZzBaseComponent
    Inherits Component
End Class

Public Class ZzBaseDisplayNameAttribute
    Inherits DisplayNameAttribute
End Class

Public Class ZzBaseEventArgs
    Inherits EventArgs
End Class

Public Class ZzBaseException
    Inherits Exception
End Class

Public Class ZzImplICustomTypeDescriptor
    Implements ICustomTypeDescriptor
End Class

Public Class ZzImplIExtenderProvider
    Implements IExtenderProvider
End Class

Public Class ZzImplINotifyPropertyChanged
    Implements INotifyPropertyChanged
End Class

Public Class ZzBasePropertyDescriptor
    Inherits PropertyDescriptor
End Class

Public Class ZzBaseStringConverter
    Inherits StringConverter
End Class

Public Class ZzBaseTypeConverter
    Inherits TypeConverter
End Class

Public Class ZzBaseCollectionBase
    Inherits CollectionBase
End Class

Public Class ZzImplIComparable
    Implements IComparable
End Class

Public Class ZzImplIComparer
    Implements IComparer
End Class

Public Class ZzImplIDisposable
    Implements IDisposable
End Class

Public Class ZzImplIEnumerable
    Implements IEnumerable
End Class

Public Class ZzImplIEquatable
    Implements IEquatable
End Class

Public Class ZzBaseList
    Inherits List
End Class

Public Class ZzBaseCultureInfo
    Inherits CultureInfo
End Class

Public Class ZzBaseDependencyObject
    Inherits DependencyObject
End Class

Public Class ZzImplIValueConverter
    Implements IValueConverter
End Class

Public Class ZzBaseUITypeEditor
    Inherits UITypeEditor
End Class

Public Class ZzImplIDeserializationCallback
    Implements IDeserializationCallback
End Class

Public Class ZzBaseSafeHandle
    Inherits SafeHandle
End Class

Public Class ZzBaseSafeHandleMinusOneIsInvalid
    Inherits SafeHandleMinusOneIsInvalid
End Class

Public Class ZzBaseSerializationBinder
    Inherits SerializationBinder
End Class

Public Class ZzBaseServiceBase
    Inherits ServiceBase
End Class
