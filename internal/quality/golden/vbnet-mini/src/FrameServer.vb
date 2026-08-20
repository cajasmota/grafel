
Imports System.Runtime.ExceptionServices
Imports System.Runtime.InteropServices
Imports Microsoft.Win32
Imports StaxRip.UI

Public Class DirectFrameServer
    Implements IDisposable, IFrameServer

    Property Info As ServerInfo Implements IFrameServer.Info

    Private NativeServer As INativeFrameServer

    Sub New(path As String)
        CreateAndOpen(path)
    End Sub

    <HandleProcessCorruptedStateExceptions>
    Sub CreateAndOpen(path As String)
        Try
            If path.Ext = "avs" Then
                Environment.SetEnvironmentVariable("AviSynthDLL", Package.AviSynth.Path)
                NativeServer = CreateAviSynthServer()
            Else
                NativeServer = CreateVapourSynthServer()
            End If

            NativeServer.OpenFile(path)

            Dim infoPtr = NativeServer.GetInfo()
            If infoPtr <> IntPtr.Zero Then
                Info = Marshal.PtrToStructure(Of ServerInfo)(infoPtr)
            End If
        Catch ex As Exception
            g.ShowException(ex)
            Throw New AbortException
        End Try
    End Sub

    ReadOnly Property [Error] As String Implements IFrameServer.Error
        Get
            Return Marshal.PtrToStringUni(NativeServer.GetError())
        End Get
    End Property

    ReadOnly Property FrameRate As Decimal Implements IFrameServer.FrameRate
        Get
            Return Decimal.Divide(Info.FrameRateNum, Info.FrameRateDen)
        End Get
    End Property

    Function GetFrame(position As Integer, ByRef data As IntPtr, ByRef pitch As Integer) As Integer Implements IFrameServer.GetFrame
        Return NativeServer.GetFrame(position, data, pitch)
    End Function

    <DllImport("FrameServer.dll")>
    Shared Function CreateAviSynthServer() As INativeFrameServer
    End Function

    <DllImport("FrameServer.dll")>
    Shared Function CreateVapourSynthServer() As INativeFrameServer
    End Function

    Sub Dispose() Implements IDisposable.Dispose
        If Not NativeServer Is Nothing Then
            Marshal.ReleaseComObject(NativeServer)
            NativeServer = Nothing
        End If
    End Sub
End Class

Public Interface IFrameServer
    Inherits IDisposable

    Property Info As ServerInfo
    ReadOnly Property [Error] As String
    ReadOnly Property FrameRate As Decimal
    Function GetFrame(position As Integer, ByRef data As IntPtr, ByRef pitch As Integer) As Integer
End Interface

<Guid("A933B077-7EC2-42CC-8110-91DE21116C1A")>
<InterfaceType(ComInterfaceType.InterfaceIsIUnknown)>
Public Interface INativeFrameServer
    <PreserveSig> Function OpenFile(file As String) As Integer
    <PreserveSig> Function GetFrame(position As Integer, ByRef data As IntPtr, ByRef pitch As Integer) As Integer
    <PreserveSig> Function GetInfo() As IntPtr
    <PreserveSig> Function GetError() As IntPtr
End Interface
