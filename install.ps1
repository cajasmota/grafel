# grafel one-line installer for Windows.
#
# Usage:
#   irm https://raw.githubusercontent.com/cajasmota/grafel/main/install.ps1 | iex
#
# Environment variables:
#   GRAFEL_VERSION   Release tag to install (default: latest, e.g. v0.1.0)
#   GRAFEL_FORCE     If "1", overwrite an existing install without warning.
#   GRAFEL_PREFIX    Install prefix (default: $env:USERPROFILE\.grafel)

#Requires -Version 5.1

$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Repo   = 'cajasmota/grafel'
$Prefix = if ($env:GRAFEL_PREFIX) { $env:GRAFEL_PREFIX } else { Join-Path $env:USERPROFILE '.grafel' }
$BinDir = Join-Path $Prefix 'bin'
$TmpDir = Join-Path $env:TEMP ("grafel-install-" + [Guid]::NewGuid().ToString('N'))

function Write-Info($msg) { Write-Host $msg }
function Fail($msg) { Write-Error $msg; exit 1 }

function Get-Arch {
    $procArch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $procArch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($procArch) {
        'AMD64' { return 'x86_64' }
        'ARM64' {
            # No native windows/arm64 release artifact is published: the release
            # build uses CGO (tree-sitter) and GitHub's x64 Windows runners have
            # no windows-arm64 C cross-toolchain, so an arm64 leg is not buildable
            # in CI. Windows on ARM64 runs x64 binaries transparently via
            # emulation, so install the x86_64 archive instead (#5274).
            Write-Info "  note: no native windows/arm64 build is published; installing the x86_64 build (runs under Windows ARM64 x64 emulation)."
            return 'x86_64'
        }
        'x86'   {
            if ([Environment]::Is64BitOperatingSystem) { return 'x86_64' }
            Fail "unsupported architecture: x86 (32-bit)"
        }
        default { Fail "unsupported architecture: $procArch" }
    }
}

# Test-Version validates a resolved tag strictly: it must start with 'v',
# contain at least one digit, and contain no '/' (which would mean a URL
# fragment leaked in). This guarantees we can never build a download URL from
# garbage and instead fail with the clear GRAFEL_VERSION hint.
function Test-Version($v) {
    if (-not $v) { return $false }
    return ($v -match '^v[0-9][A-Za-z0-9.+_-]*$')
}

function Resolve-Version {
    # Explicit override is the fast path and skips all network resolution.
    if ($env:GRAFEL_VERSION -and $env:GRAFEL_VERSION -ne 'latest') {
        return $env:GRAFEL_VERSION
    }

    $ver = $null

    # Prefer the GitHub releases API: it returns the tag in a single JSON field
    # (tag_name), which is far more reliable than scraping a redirect header.
    try {
        $api = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing -ErrorAction Stop
        if ($api -and $api.tag_name) { $ver = $api.tag_name.Trim() }
    } catch {
        $ver = $null
    }

    # Fallback: parse the /releases/latest redirect target when the API is
    # unreachable or rate-limited.
    if (-not (Test-Version $ver)) {
        $url = "https://github.com/$Repo/releases/latest"
        try {
            $resp = Invoke-WebRequest -Uri $url -UseBasicParsing -MaximumRedirection 0 -ErrorAction SilentlyContinue
        } catch {
            $resp = $_.Exception.Response
        }
        $loc = $null
        if ($resp -and $resp.Headers) {
            if ($resp.Headers['Location']) { $loc = $resp.Headers['Location'] }
            elseif ($resp.Headers.Location) { $loc = $resp.Headers.Location }
        }
        if (-not $loc) {
            # Last resort: follow redirects and read the final URI.
            try {
                $resp2 = Invoke-WebRequest -Uri $url -UseBasicParsing
                $loc = $resp2.BaseResponse.ResponseUri.AbsoluteUri
            } catch { $loc = $null }
        }
        if ($loc -and ($loc -match '/tag/([^/]+)/?$')) { $ver = $Matches[1] }
    }

    # Strict validation: never proceed with a URL fragment or other junk.
    if (-not (Test-Version $ver)) {
        Fail "failed to resolve a valid latest release tag (got '$ver'). Set GRAFEL_VERSION explicitly (e.g. v0.1.0)."
    }
    return $ver
}

function Get-FileWithRetry($Uri, $OutFile) {
    for ($i = 1; $i -le 3; $i++) {
        try {
            Invoke-WebRequest -Uri $Uri -OutFile $OutFile -UseBasicParsing
            return
        } catch {
            if ($i -eq 3) { Fail "failed to download $Uri : $_" }
            Start-Sleep -Seconds 2
        }
    }
}

function Verify-Checksum($ArchivePath, $ArchiveName, $ChecksumsPath) {
    $line = Select-String -Path $ChecksumsPath -Pattern ([regex]::Escape($ArchiveName) + '\s*$') | Select-Object -First 1
    if (-not $line) { Fail "checksum for $ArchiveName not found in checksums.txt" }
    $expected = ($line.Line -split '\s+')[0].ToLower()
    $actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) {
        Fail "checksum mismatch for $ArchiveName (expected $expected, got $actual)"
    }
}

function Add-ToUserPath($Dir) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) { $current = '' }
    $entries = $current -split ';' | Where-Object { $_ -ne '' }
    if ($entries -contains $Dir) { return }
    $new = if ($current.TrimEnd(';')) { $current.TrimEnd(';') + ';' + $Dir } else { $Dir }
    [Environment]::SetEnvironmentVariable('Path', $new, 'User')
    # Update current session too.
    $env:Path = $env:Path.TrimEnd(';') + ';' + $Dir
}

# Stop-Daemon stops a running grafel daemon BEFORE the new binary is copied.
# Windows cannot overwrite an .exe that is open in a running process, so on an
# upgrade the Copy-Item would otherwise fail ("being used by another process")
# while the registered daemon holds grafel.exe. Best-effort: a missing task or
# no running process is fine and never aborts the installer.
function Stop-Daemon {
    $taskName = 'com.grafel.daemon'
    if (Get-Command schtasks.exe -ErrorAction SilentlyContinue) {
        & schtasks.exe /end /tn $taskName 2>$null | Out-Null
    }
    # Kill any lingering process holding the exe (ignore "not found").
    try {
        Get-Process -Name 'grafel' -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    } catch { }
}

# Restart-Daemon restarts an already-registered grafel daemon so it runs the
# freshly-installed binary. Best-effort: a missing tool or a failed restart
# prints a hint and returns $false, but never aborts the installer.
# grafel install registers a Task Scheduler task named 'com.grafel.daemon'.
function Restart-Daemon {
    $taskName = 'com.grafel.daemon'
    $hint = "re-run 'grafel install' or restart the daemon to finish the update"

    if (-not (Get-Command schtasks.exe -ErrorAction SilentlyContinue)) {
        return $false
    }

    # Detect a registered task (schtasks /query exits non-zero when absent).
    & schtasks.exe /query /tn $taskName 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
        return $false
    }

    # Stop then start the task so it re-launches the new binary.
    & schtasks.exe /end /tn $taskName 2>$null | Out-Null
    & schtasks.exe /run /tn $taskName 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Info "warning: failed to restart the grafel daemon; $hint"
        return $false
    }
    return $true
}

# --- main ---

$arch    = Get-Arch
$version = Resolve-Version
$verNoV  = $version.TrimStart('v')

$archiveName  = "grafel_${verNoV}_windows_${arch}.zip"
$archiveUrl   = "https://github.com/$Repo/releases/download/$version/$archiveName"
$checksumsUrl = "https://github.com/$Repo/releases/download/$version/checksums.txt"

Write-Info "grafel installer"
Write-Info "  version: $version"
Write-Info "  target:  windows/$arch"
Write-Info "  prefix:  $Prefix"

$existing = Join-Path $BinDir 'grafel.exe'
if ((Test-Path $existing) -and $env:GRAFEL_FORCE -ne '1') {
    Write-Info "  upgrading existing install at $BinDir"
}

New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

try {
    $archivePath   = Join-Path $TmpDir $archiveName
    $checksumsPath = Join-Path $TmpDir 'checksums.txt'

    Write-Info "downloading $archiveUrl"
    Get-FileWithRetry -Uri $archiveUrl -OutFile $archivePath

    Write-Info "downloading checksums.txt"
    Get-FileWithRetry -Uri $checksumsUrl -OutFile $checksumsPath

    Write-Info "verifying SHA256"
    Verify-Checksum -ArchivePath $archivePath -ArchiveName $archiveName -ChecksumsPath $checksumsPath

    Write-Info "extracting"
    $extractDir = Join-Path $TmpDir 'extract'
    New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

    $binSrc = Get-ChildItem -Path $extractDir -Recurse -Filter 'grafel.exe' | Select-Object -First 1
    if (-not $binSrc) { Fail "archive did not contain grafel.exe" }

    # On an upgrade, stop the running daemon BEFORE overwriting the binary —
    # Windows cannot replace an .exe that is open in a running process. The
    # daemon is re-registered/restarted by Restart-Daemon further down.
    if (Test-Path $existing) { Stop-Daemon }

    Copy-Item -Path $binSrc.FullName -Destination $existing -Force

    Add-ToUserPath -Dir $BinDir

    Write-Info ""
    try {
        & $existing doctor 2>$null
    } catch {
        try { & $existing --version } catch { }
    }

    # If a daemon is already registered, restart it so it picks up the new
    # binary. Best-effort: Restart-Daemon never aborts the installer.
    $daemonRestarted = Restart-Daemon

    Write-Info ""
    if ($daemonRestarted) {
        Write-Info "grafel updated and daemon restarted."
    } else {
        Write-Info "grafel installed. Run `"grafel wizard`" to set up your first group."
    }
    Write-Info "(open a new terminal so PATH picks up $BinDir)"
}
finally {
    if (Test-Path $TmpDir) {
        Remove-Item -Recurse -Force -Path $TmpDir -ErrorAction SilentlyContinue
    }
}
