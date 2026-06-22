@echo off
setlocal EnableExtensions EnableDelayedExpansion
REM grafel one-line installer for Windows (CMD / no PowerShell, non-admin).
REM
REM Usage:
REM   curl -fL https://raw.githubusercontent.com/cajasmota/grafel/main/install.bat -o "%TEMP%\grafel-install.bat" ^&^& "%TEMP%\grafel-install.bat"
REM
REM Environment variables:
REM   GRAFEL_VERSION   Release tag to install (default: latest, e.g. v0.1.0)
REM   GRAFEL_PREFIX    Install prefix (default: %USERPROFILE%\.grafel)
REM
REM This mirrors install.ps1: same prefix (%USERPROFILE%\.grafel\bin), same
REM release asset names, same checksums.txt verification, and the same
REM USER-PATH append. It needs only Windows 10 1803+ built-ins (curl, certutil,
REM tar, reg, setx) and never requires administrator rights.

set "REPO=cajasmota/grafel"

if defined GRAFEL_PREFIX (
    set "PREFIX=%GRAFEL_PREFIX%"
) else (
    set "PREFIX=%USERPROFILE%\.grafel"
)
set "BINDIR=%PREFIX%\bin"

REM Carriage-return literal, used to scrub the trailing CR off curl's HTTP
REM response headers when resolving the latest version.
for /f %%R in ('copy /Z "%~f0" nul') do set "CR=%%R"
set "TMPZIP=%TEMP%\grafel-%RANDOM%%RANDOM%.zip"
set "TMPSUMS=%TEMP%\grafel-%RANDOM%%RANDOM%-checksums.txt"
set "TMPEXTRACT=%TEMP%\grafel-extract-%RANDOM%%RANDOM%"

REM --- architecture detection (mirror install.ps1 Get-Arch) ---
set "PROC=%PROCESSOR_ARCHITECTURE%"
if defined PROCESSOR_ARCHITEW6432 set "PROC=%PROCESSOR_ARCHITEW6432%"

if /I "%PROC%"=="AMD64" (
    set "ARCH=x86_64"
) else if /I "%PROC%"=="ARM64" (
    REM No native windows/arm64 release artifact is published: the release build
    REM uses CGO (tree-sitter) and GitHub's Windows runners have no
    REM windows-arm64 C cross-toolchain. Windows on ARM64 runs x64 binaries
    REM transparently via emulation, so install the x86_64 archive (#5274).
    echo   note: no native windows/arm64 build is published; installing the x86_64 build ^(runs under Windows ARM64 x64 emulation^).
    set "ARCH=x86_64"
) else if /I "%PROC%"=="x86" (
    REM 32-bit cmd on 64-bit Windows is handled by PROCESSOR_ARCHITEW6432 above;
    REM a genuine 32-bit OS is unsupported.
    echo error: unsupported architecture: x86 ^(32-bit^). grafel ships 64-bit only.
    goto :fail
) else (
    echo error: unsupported architecture: %PROC%
    goto :fail
)

REM --- resolve version (mirror install.ps1 Resolve-Version) ---
if defined GRAFEL_VERSION (
    if /I not "%GRAFEL_VERSION%"=="latest" (
        set "VERSION=%GRAFEL_VERSION%"
    )
)
if not defined VERSION (
    REM /releases/latest 302-redirects to /releases/tag/<version>. curl -sI
    REM prints the redirect target in the "location:" header; parse the tag.
    REM The header value is the FULL url, e.g.
    REM   location: https://github.com/<repo>/releases/tag/vX.Y.Z<CR>
    REM tokens=2 with a single space delim keeps the whole url (incl. its ':'
    REM and '/') as one token. We must NOT use %%~nx here: that modifier treats
    REM its arg as a '\'-separated Windows path, but the value is a '/'-separated
    REM URL, so on some Windows builds it returns garbage (e.g. "LOC:=") and
    REM yields a 404 download. Instead strip everything up to and including
    REM "/tag/" with a delayed-expansion substring, leaving just the tag.
    for /f "tokens=1,* delims= " %%H in ('curl -fsSLI "https://github.com/%REPO%/releases/latest" 2^>nul ^| findstr /I "^location:"') do (
        set "LOC=%%I"
    )
    if defined LOC (
        REM scrub the trailing CR that HTTP headers carry FIRST, so the tag has
        REM no stray CR, THEN slice off the URL prefix up to and incl. "/tag/".
        if defined CR set "LOC=!LOC:%CR%=!"
        REM only slice if the marker is actually present (substring replace is a
        REM no-op when "/tag/" is absent, which would leave the full URL — we
        REM guard against that below by validating the resulting VERSION).
        set "VERSION=!LOC:*/tag/=!"
    )
)

REM --- fallback: GitHub releases API if the redirect parse failed or produced
REM     something that still contains a URL separator (no real tag has '/'). ---
set "BADVER="
if defined VERSION if not "!VERSION:/=!"=="!VERSION!" set "BADVER=1"
if not defined VERSION set "BADVER=1"
if defined BADVER (
    set "VERSION="
    for /f "tokens=2 delims=:, " %%T in ('curl -fsSL "https://api.github.com/repos/%REPO%/releases/latest" 2^>nul ^| findstr /I /C:"tag_name"') do (
        if not defined VERSION (
            set "RAW=%%T"
            REM strip surrounding double quotes from the JSON string value.
            set "RAW=!RAW:"=!"
            set "VERSION=!RAW!"
        )
    )
)

if not defined VERSION (
    echo error: failed to resolve latest release tag. Set GRAFEL_VERSION explicitly ^(e.g. v0.1.0^).
    goto :fail
)

REM --- sanity-guard the resolved VERSION before building any download URL ---
REM A valid tag looks like v<digits>... : it starts with 'v' and contains a
REM digit, and must NOT contain a '/' (which would mean we kept part of a URL).
REM Reject anything else with the explicit GRAFEL_VERSION hint instead of
REM letting a garbage version produce a confusing 404.
set "VERBAD="
if not "!VERSION:~0,1!"=="v" set "VERBAD=1"
if not "!VERSION:/=!"=="!VERSION!" set "VERBAD=1"
set "HASDIGIT="
for %%D in (0 1 2 3 4 5 6 7 8 9) do if not "!VERSION:%%D=!"=="!VERSION!" set "HASDIGIT=1"
if not defined HASDIGIT set "VERBAD=1"
if defined VERBAD (
    echo error: resolved an invalid release tag ^("!VERSION!"^). Set GRAFEL_VERSION explicitly ^(e.g. v0.1.0^).
    goto :fail
)

REM strip a leading "v" for the asset filename (grafel_<ver>_windows_<arch>.zip)
set "VERNOV=%VERSION%"
if "%VERNOV:~0,1%"=="v" set "VERNOV=%VERNOV:~1%"

set "ARCHIVE=grafel_%VERNOV%_windows_%ARCH%.zip"
set "ARCHIVE_URL=https://github.com/%REPO%/releases/download/%VERSION%/%ARCHIVE%"
set "CHECKSUMS_URL=https://github.com/%REPO%/releases/download/%VERSION%/checksums.txt"

echo grafel installer
echo   version: %VERSION%
echo   target:  windows/%ARCH%
echo   prefix:  %PREFIX%

if exist "%BINDIR%\grafel.exe" echo   upgrading existing install at %BINDIR%

if not exist "%BINDIR%" mkdir "%BINDIR%"

REM --- download archive (curl, 3 retries) ---
echo downloading %ARCHIVE_URL%
curl -fL --retry 3 -o "%TMPZIP%" "%ARCHIVE_URL%"
if errorlevel 1 (
    echo error: failed to download %ARCHIVE_URL%
    goto :fail
)

echo downloading checksums.txt
curl -fL --retry 3 -o "%TMPSUMS%" "%CHECKSUMS_URL%"
if errorlevel 1 (
    echo error: failed to download %CHECKSUMS_URL%
    goto :fail
)

REM --- verify SHA256 against checksums.txt ---
echo verifying SHA256
set "EXPECTED="
for /f "usebackq tokens=1,2" %%A in ("%TMPSUMS%") do (
    if /I "%%B"=="%ARCHIVE%" set "EXPECTED=%%A"
)
if not defined EXPECTED (
    echo error: checksum for %ARCHIVE% not found in checksums.txt
    goto :fail
)

set "ACTUAL="
REM certutil prints the hash on the line AFTER the "SHA256 hash of file" banner.
for /f "skip=1 tokens=* delims=" %%H in ('certutil -hashfile "%TMPZIP%" SHA256') do (
    if not defined ACTUAL (
        set "LINE=%%H"
        REM the hash line has no spaces; the "CertUtil:" trailer line does.
        if not "!LINE:CertUtil=!"=="!LINE!" (
            rem skip the trailer
        ) else (
            set "ACTUAL=!LINE: =!"
        )
    )
)
if not defined ACTUAL (
    echo error: failed to compute SHA256 of %TMPZIP%
    goto :fail
)

REM normalize case for comparison (certutil is lower-case; checksums.txt too).
if /I not "%EXPECTED%"=="%ACTUAL%" (
    echo error: checksum mismatch for %ARCHIVE%
    echo   expected: %EXPECTED%
    echo   actual:   %ACTUAL%
    goto :fail
)

REM --- extract (tar ships with Windows 10 1803+) ---
echo extracting
if not exist "%TMPEXTRACT%" mkdir "%TMPEXTRACT%"
tar -xf "%TMPZIP%" -C "%TMPEXTRACT%"
if errorlevel 1 (
    echo error: failed to extract %TMPZIP%
    goto :fail
)

REM locate grafel.exe in the extracted tree (top-level per release.yml).
set "BINSRC="
if exist "%TMPEXTRACT%\grafel.exe" set "BINSRC=%TMPEXTRACT%\grafel.exe"
if not defined BINSRC (
    for /f "delims=" %%F in ('dir /b /s "%TMPEXTRACT%\grafel.exe" 2^>nul') do (
        if not defined BINSRC set "BINSRC=%%F"
    )
)
if not defined BINSRC (
    echo error: archive did not contain grafel.exe
    goto :fail
)

copy /Y "%BINSRC%" "%BINDIR%\grafel.exe" >nul
if errorlevel 1 (
    echo error: failed to copy grafel.exe into %BINDIR%
    goto :fail
)

REM --- add %BINDIR% to the USER PATH (non-admin, never touches system PATH) ---
REM `grafel install` registers MCP/hooks/watchers but does NOT manage the OS
REM PATH, so the installer owns it here, exactly like install.ps1's
REM Add-ToUserPath.
set "USERPATH="
for /f "usebackq tokens=2,*" %%A in (`reg query HKCU\Environment /v Path 2^>nul ^| findstr /I "Path"`) do set "USERPATH=%%B"

set "ALREADY="
if defined USERPATH (
    REM case-insensitive substring check, padded with ; so we match whole entries.
    set "PADDED=;!USERPATH!;"
    set "NEEDLE=;%BINDIR%;"
    if /I not "!PADDED:%NEEDLE%=!"=="!PADDED!" set "ALREADY=1"
)

if defined ALREADY (
    echo   PATH already contains %BINDIR%
) else (
    if defined USERPATH (
        REM trim a single trailing ; then append.
        if "!USERPATH:~-1!"==";" set "USERPATH=!USERPATH:~0,-1!"
        setx Path "!USERPATH!;%BINDIR%" >nul
    ) else (
        setx Path "%BINDIR%" >nul
    )
    if errorlevel 1 (
        echo   warning: could not update USER PATH automatically.
        echo   add this folder to PATH manually: %BINDIR%
    ) else (
        echo   added %BINDIR% to your USER PATH
    )
)
REM make grafel.exe resolvable in THIS session for the install step below.
set "PATH=%PATH%;%BINDIR%"

echo.
REM --- register MCP / hooks / watchers (does NOT touch PATH) ---
"%BINDIR%\grafel.exe" install
REM `grafel install` may exit non-zero before any group exists; don't abort the
REM whole installer on that — the binary is on disk and on PATH.

echo.
"%BINDIR%\grafel.exe" --version 2>nul

echo.
echo Done. grafel is installed at %BINDIR%\grafel.exe
echo Restart your shell ^(or open a new terminal^) so PATH picks up %BINDIR%, then run:
echo   grafel --version
echo   grafel wizard
call :cleanup
endlocal
exit /b 0

:fail
call :cleanup
echo.
echo install failed.
endlocal
exit /b 1

:cleanup
if exist "%TMPZIP%" del /f /q "%TMPZIP%" >nul 2>&1
if exist "%TMPSUMS%" del /f /q "%TMPSUMS%" >nul 2>&1
if exist "%TMPEXTRACT%" rmdir /s /q "%TMPEXTRACT%" >nul 2>&1
goto :eof
