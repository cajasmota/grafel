# grafel one-line installer for Windows.
#
# Usage:
#   irm https://raw.githubusercontent.com/cajasmota/grafel/main/install.ps1 | iex
#
# Environment variables:
#   GRAFEL_VERSION   Release tag to install (default: latest, e.g. v0.1.0)
#   GRAFEL_FORCE     If "1", overwrite an existing install without warning.
#   GRAFEL_PREFIX    Install prefix (default: $env:USERPROFILE\.grafel)
#   GRAFEL_ARCHIVE   Path to a LOCAL release .zip to install from, instead of
#                    downloading one. Skips version resolution, download and
#                    checksum verification — you are asserting you trust the
#                    file you just pointed at. Exists so CI can execute this
#                    script end-to-end against a freshly built binary (see
#                    .github/workflows/windows-installers.yml); also usable for
#                    an offline install from a hand-downloaded release asset.

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

# Invoke-Native runs an external executable and returns its exit code, without
# letting anything the executable writes to stderr abort this script.
#
# THIS IS NOT A STYLE PREFERENCE — it is the fix for a defect that made a
# first-ever Windows install impossible (found by the first real CI run of this
# script; see .github/workflows/windows-installers.yml).
#
# The script sets `$ErrorActionPreference = 'Stop'` at the top, which is right
# for cmdlets. But when a NATIVE command's stderr is REDIRECTED — `2>$null`,
# `2>&1` — PowerShell wraps each stderr line in an ErrorRecord, and under
# 'Stop' that ErrorRecord is TERMINATING. So this:
#
#     & schtasks.exe /query /tn com.grafel.daemon 2>$null | Out-Null
#
# does not quietly discard the error, which is plainly what it was written to
# do. `schtasks /query` for a task that does not exist yet prints
# "ERROR: The system cannot find the file specified." to stderr, and the script
# dies right there with a NativeCommandError. The `2>$null` is not a mitigation,
# it is the TRIGGER: without a redirect, native stderr goes straight to the
# console and never becomes an ErrorRecord at all.
#
# The task does not exist on a first-ever install, by definition. Every
# Stop-Daemon / Restart-Daemon probe here is a "does this exist?" question asked
# of a tool that answers "no" on stderr, so all four call sites were fatal.
#
# Restoring 'Continue' around just the invocation keeps the strict default
# everywhere else. `2>&1 | Out-Null` then merges and discards the stderr text
# the caller never wanted. Works identically on Windows PowerShell 5.1 (the
# `#Requires` floor, and what `irm | iex` actually runs) and on PowerShell 7.
#
# Be clear about WHICH mechanism does the work here, because it is not the one
# it looks like: a FUNCTION creates a new scope, so `$ErrorActionPreference =
# 'Continue'` below makes a function-local SHADOW that wins for the duration of
# the call and dies with the scope. The save/restore in the `finally` is
# therefore a no-op — it restores a shadow that is about to be discarded. It is
# kept only so this function stays correct if its body is ever inlined into a
# caller's scope. Contrast the `doctor` block near the end of the script, where
# the same pattern IS load-bearing: `try` is not a scope in PowerShell, so that
# assignment lands at SCRIPT scope and would leak to every later statement if
# the `finally` did not put it back.
function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [string[]]$Arguments = @()
    )
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $FilePath @Arguments 2>&1 | Out-Null
        return $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $prevEAP
    }
}

# Stop-Daemon stops a running grafel daemon BEFORE the new binary is copied.
# Windows cannot overwrite an .exe that is open in a running process, so on an
# upgrade the Copy-Item would otherwise fail ("being used by another process")
# while the registered daemon holds grafel.exe. Best-effort: a missing task or
# no running process is fine and never aborts the installer.
function Stop-Daemon {
    $taskName = 'com.grafel.daemon'
    if (Get-Command schtasks.exe -ErrorAction SilentlyContinue) {
        # `/end` on a task that is registered but NOT running also answers on
        # stderr, so this was fatal on the upgrade path even when the task
        # existed. Exit code ignored on purpose: not-running is a success here.
        Invoke-Native -FilePath 'schtasks.exe' -Arguments @('/end', '/tn', $taskName) | Out-Null
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
    # `grafel restart`, NOT `grafel install` (#6163). This hint is printed
    # exactly when the daemon is known not to be healthy, which is the
    # condition in which RunCopy's step-4 restart is most likely to fail — and
    # its failure rolls step 3 back, restoring the MCP host configs from
    # snapshots, which on a first-ever install deletes .claude.json and every
    # foreign server in it (#6168). Bare `grafel install` from a console also
    # writes .gitignore + four git hooks into whatever directory the user is
    # standing in (#6162) and blocks on the TTY tool-selection wizard.
    $hint = "run 'grafel restart' to finish the update"

    if (-not (Get-Command schtasks.exe -ErrorAction SilentlyContinue)) {
        return $false
    }

    # Detect a registered task (schtasks /query exits non-zero when absent).
    # On a first-ever install it is absent and schtasks says so on stderr —
    # which, before Invoke-Native, killed the installer here on EVERY new
    # Windows machine. See the Invoke-Native header.
    if ((Invoke-Native -FilePath 'schtasks.exe' -Arguments @('/query', '/tn', $taskName)) -ne 0) {
        return $false
    }

    # Stop then start the task so it re-launches the new binary. Only /run's
    # exit code decides the outcome: /end on an already-stopped task is not a
    # failure, it is the state we want.
    Invoke-Native -FilePath 'schtasks.exe' -Arguments @('/end', '/tn', $taskName) | Out-Null
    if ((Invoke-Native -FilePath 'schtasks.exe' -Arguments @('/run', '/tn', $taskName)) -ne 0) {
        Write-Info "warning: failed to restart the grafel daemon; $hint"
        return $false
    }
    return $true
}

# --- main ---

$arch    = Get-Arch

# GRAFEL_ARCHIVE short-circuits version resolution entirely: there is no tag to
# resolve when the archive is already on disk. Resolve to a full path NOW, while
# the caller's working directory is still current — everything below runs after
# several directory-independent steps, and a relative path resolved late is a
# silent "file not found" on a machine we cannot debug.
$localArchive = $null
if ($env:GRAFEL_ARCHIVE) {
    if (-not (Test-Path -LiteralPath $env:GRAFEL_ARCHIVE -PathType Leaf)) {
        Fail "GRAFEL_ARCHIVE is set to '$($env:GRAFEL_ARCHIVE)' but that is not an existing file."
    }
    $localArchive = (Resolve-Path -LiteralPath $env:GRAFEL_ARCHIVE).ProviderPath
}

# The version label is cosmetic in the local-archive case — it names a scratch
# file in $TmpDir and prints in the banner. It is still shaped like a real tag so
# nothing downstream has to special-case it.
$version = if ($localArchive) { 'v0.0.0-local' } else { Resolve-Version }
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

    if ($localArchive) {
        # No download, and therefore nothing to verify a checksum AGAINST: the
        # published checksums.txt describes released assets, and this archive is
        # by definition not one. Copying into $TmpDir rather than reading in
        # place keeps the extract step and the `finally` cleanup identical to
        # the download path, so the two paths diverge here and nowhere else.
        Write-Info "using local archive $localArchive"
        Write-Info "  (GRAFEL_ARCHIVE is set: skipping download and checksum verification)"
        Copy-Item -LiteralPath $localArchive -Destination $archivePath -Force
    } else {
        Write-Info "downloading $archiveUrl"
        Get-FileWithRetry -Uri $archiveUrl -OutFile $archivePath

        Write-Info "downloading checksums.txt"
        Get-FileWithRetry -Uri $checksumsUrl -OutFile $checksumsPath

        Write-Info "verifying SHA256"
        Verify-Checksum -ArchivePath $archivePath -ArchiveName $archiveName -ChecksumsPath $checksumsPath
    }

    Write-Info "extracting"
    $extractDir = Join-Path $TmpDir 'extract'
    New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

    $binSrc = Get-ChildItem -Path $extractDir -Recurse -Filter 'grafel.exe' | Select-Object -First 1
    if (-not $binSrc) { Fail "archive did not contain grafel.exe" }

    # On an upgrade, stop the running daemon BEFORE overwriting the binary —
    # Windows cannot replace an .exe that is open in a running process. The
    # daemon is re-registered/restarted by Restart-Daemon further down.
    # Best-effort by contract (see Stop-Daemon's header): a missing task or no
    # running process must never abort an upgrade. Wrapped here as well as
    # inside, so a future native call added to Stop-Daemon cannot re-break the
    # upgrade path the way the schtasks probes broke the first-install path.
    if (Test-Path $existing) {
        # Speaks for the same reason the Restart-Daemon catch does: if this
        # throws, the very next statement is a Copy-Item over an .exe that may
        # still be running, and "file in use" is a lot easier to act on when the
        # reason the stop failed was not swallowed one line earlier.
        try { Stop-Daemon } catch {
            Write-Info "warning: could not stop the running grafel daemon ($($_.Exception.Message)); continuing"
        }
    }

    Copy-Item -Path $binSrc.FullName -Destination $existing -Force

    # Record the .exe we just placed, BEFORE reporting doctor output (#6163).
    #
    # Without this the script shipped a bug it then printed. Copy-Item replaces
    # the binary but nothing rewrites ~/.grafel/install.json, whose CLI record
    # is only ever written by `grafel install`. RunQuickDoctor compares
    # state.CLI.SHA256 against the binary at state.CLI.Path, so after every
    # upgrade the recorded SHA is the PREVIOUS binary's and
    #   "grafel doctor: binary updated since last install …"
    # is prefixed onto EVERY subsequent grafel command, permanently — including
    # the `doctor` call three lines below. This is the same defect install.sh
    # already fixed with record_install_state, and the same remedy.
    #
    # Then register the MCP server (#6169). The script placed a binary and
    # registered nothing, so a first-ever Windows install produced a grafel no
    # AI coding tool could see — grafel's entire interface is the MCP server.
    #
    # Both are narrow by construction: they write install.json and the MCP host
    # configs and nothing else. Running bare `grafel install` here instead
    # would restart the daemon on a 60s budget (duplicating and racing
    # Restart-Daemon below), append /.grafel/ to the .gitignore of whatever
    # directory the user launched this from and write four git hooks there
    # (#6162), and — on an interactive console, where stdin is a TTY — block on
    # the tool-selection wizard mid-install.
    #
    # Best-effort, like every other post-download step: neither may abort an
    # installer whose binary is already on disk.
    try { & $existing install --refresh-state } catch { }
    try { & $existing install --register-mcp } catch { }

    Add-ToUserPath -Dir $BinDir

    Write-Info ""
    # The point of this call is to SHOW the user doctor's output, so unlike the
    # probes above it must not swallow stdout. The `2>$null` that used to be
    # here did two bad things: under $ErrorActionPreference='Stop' it turned any
    # doctor warning on stderr into a terminating error (see Invoke-Native), and
    # the catch then silently downgraded the whole report to `--version`. So the
    # more grafel had to say, the less the user saw. Drop the redirect, relax
    # the preference around the call, and decide on the EXIT CODE instead.
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $existing doctor
        if ($LASTEXITCODE -ne 0) { & $existing --version }
    } catch {
        try { & $existing --version } catch { }
    } finally {
        $ErrorActionPreference = $prevEAP
    }

    # If a daemon is already registered, restart it so it picks up the new
    # binary. Best-effort: Restart-Daemon never aborts the installer — a promise
    # the code did not keep until Invoke-Native landed, and which this try/catch
    # now enforces at the call site too rather than trusting the callee.
    #
    # The catch MUST speak. Restart-Daemon warns on its own only for the one
    # failure it anticipates (a non-zero `schtasks /run`); anything else —
    # schtasks.exe off PATH raising CommandNotFoundException, which propagates
    # straight through Invoke-Native's try/FINALLY since that has no catch —
    # unwinds past that warning entirely. A silent catch here would then let the
    # installer exit 0 on an UPGRADE where Stop-Daemon already killed the
    # daemon, and fall through to the first-install epilogue that tells the user
    # to go run `grafel wizard`. Dead daemon, cheerful success message, wrong
    # instructions. Best-effort means "does not abort", never "says nothing".
    $daemonRestarted = $false
    try {
        $daemonRestarted = Restart-Daemon
    } catch {
        Write-Info "warning: could not restart the grafel daemon ($($_.Exception.Message)); run 'grafel restart' to finish the update"
    }

    Write-Info ""
    if ($daemonRestarted) {
        Write-Info "grafel updated and daemon restarted."
    } else {
        Write-Info "grafel installed and registered with your AI coding tools."
        Write-Info "Restart Claude Code to load the grafel MCP tools, then run `"grafel wizard`" to set up your first group."
    }
    Write-Info "(open a new terminal so PATH picks up $BinDir)"
}
finally {
    if (Test-Path $TmpDir) {
        Remove-Item -Recurse -Force -Path $TmpDir -ErrorAction SilentlyContinue
    }
}
