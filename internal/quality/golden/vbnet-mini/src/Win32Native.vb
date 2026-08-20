Option Strict On

Imports System.ComponentModel
Imports System.Runtime.InteropServices

Namespace Display_Driver_Uninstaller.Win32

	<ComVisible(False)>
	Friend Module Win32Native

		<StructLayout(LayoutKind.Explicit)>
		Friend Structure EvilInteger
			<FieldOffset(0)>
			Public Int32 As Int32
			<FieldOffset(0)>
			Public UInt32 As UInt32
		End Structure

		' <Extension()>
		Friend Function IntPtrAdd(ByVal ptr As IntPtr, ByVal offSet As Int64) As IntPtr
			Return New IntPtr(ptr.ToInt64() + offSet)
		End Function

		Friend Class StructPtr
			Implements IDisposable

			Private _disposed As Boolean
			Private _ptr As IntPtr
			Private _objSize As New EvilInteger

			Public ReadOnly Property Ptr As IntPtr
				Get
					Return _ptr
				End Get
			End Property
			Public ReadOnly Property ObjSize As Int32
				Get
					Return _objSize.Int32
				End Get
			End Property
			Public ReadOnly Property ObjSizeU As UInt32
				Get
					Return _objSize.UInt32
				End Get
			End Property

			Public Sub New(ByVal obj As Object, Optional ByVal size As UInt32 = 0UI)
				If (size <= 0UI) Then
					_objSize.Int32 = Marshal.SizeOf(obj)
				Else
					_objSize.UInt32 = size
				End If

				_ptr = Marshal.AllocHGlobal(ObjSize)
				Marshal.StructureToPtr(obj, _ptr, False)
			End Sub

			Protected Overridable Sub Dispose(ByVal disposing As Boolean)
				If Not _disposed Then
					If disposing Then

					End If

					If _ptr <> IntPtr.Zero Then
						Marshal.FreeHGlobal(Ptr)
						_ptr = IntPtr.Zero
					End If

				End If

				_disposed = True
			End Sub

			Protected Overrides Sub Finalize()
				Dispose(False)
				MyBase.Finalize()
			End Sub

			Public Sub Dispose() Implements IDisposable.Dispose
				Dispose(True)
				GC.SuppressFinalize(Me)
			End Sub
		End Class

		<StructLayout(LayoutKind.Sequential)>
		Friend Structure SYSTEMTIME
			Public wYear As UInt16
			Public wMonth As UInt16
			Public wDayOfWeek As UInt16
			Public wDay As UInt16
			Public wHour As UInt16
			Public wMinute As UInt16
			Public wSecond As UInt16
			Public wMilliseconds As UInt16

			Public Overrides Function ToString() As String
				Return String.Format("{0}/{1}/{2}  {3}:{4}:{5}", wDay.ToString(), wMonth.ToString(), wYear.ToString(), wHour.ToString(), wMinute.ToString(), wSecond.ToString)
			End Function
		End Structure
	End Module
End Namespace
