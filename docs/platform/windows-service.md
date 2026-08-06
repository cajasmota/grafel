# grafel daemon — Windows service smoke-test guide

This document is for the Windows tester validating `grafel install` / `grafel uninstall` / `grafel status` on a real Windows machine after the `schtasks_windows.go` implementation lands.

## Prerequisites

- Windows 10 or Windows 11 (Task Scheduler 1.4 — `version="1.4"` in the XML).
- A built `grafel.exe` on the target machine. See [build instructions](#building-grafel-exe-for-windows).
- PowerShell or `cmd.exe` running as the **current user** (no administrator needed).

## Building grafel.exe for Windows

From a Unix or Windows machine with Go installed:

```bash
GOOS=windows GOARCH=amd64 go build -o grafel.exe ./cmd/grafel
```

Copy `grafel.exe` to the target Windows machine.

---

## Smoke test procedure

### 1. Verify initial state — no task registered

```powershell
schtasks /query /tn com.grafel.daemon
# Expected: ERROR: The system cannot find the file specified.
```

### 2. Install the daemon service

```powershell
.\grafel.exe install
```

Expected output (approximate):

```
grafel daemon service installed.
Status: installed=true running=true pid=<N>
```

### 3. Verify the task exists in Task Scheduler

```powershell
schtasks /query /tn com.grafel.daemon /fo list /v
```

Key fields to check:

| Field | Expected value |
|---|---|
| `Task Name` | `\com.grafel.daemon` |
| `Status` | `Running` |
| `Logon Mode` | `Interactive/Background` |
| `Run As User` | current Windows user |
| `Scheduled Task State` | `Enabled` |

### 4. Verify the daemon named pipe is connectable

```powershell
# Replace <username> with your lowercased Windows username
Test-Path "\\.\pipe\grafel-daemon-<username>"
# Expected: True
```

Or use the grafel status command:

```powershell
.\grafel.exe status
# Expected: installed=true running=true pid=<N>
```

### 5. Verify the task XML was staged to disk

```powershell
# %LOCALAPPDATA% = C:\Users\<user>\AppData\Local
ls "$env:LOCALAPPDATA\grafel\tasks\com.grafel.daemon.xml"
# Expected: file exists
```

### 6. Simulate a crash-restart (RestartOnFailure)

```powershell
# Find the PID from step 4, then kill the daemon process:
Stop-Process -Id <pid> -Force

# Wait ~90 seconds (RestartOnFailure Interval is PT1M = 1 minute)
Start-Sleep -Seconds 90

# Check if the task restarted:
schtasks /query /tn com.grafel.daemon /fo list /v | Select-String "Status"
# Expected: Status: Running
```

### 7. Verify idempotency — running install a second time

```powershell
.\grafel.exe install
# Expected: no error, returns current status without modifying anything
```

### 8. Uninstall the daemon service

```powershell
.\grafel.exe uninstall
```

Expected output (approximate):

```
grafel daemon service removed.
```

### 9. Verify the task is gone

```powershell
schtasks /query /tn com.grafel.daemon
# Expected: ERROR: The system cannot find the file specified.
```

### 10. Verify the XML file was removed

```powershell
ls "$env:LOCALAPPDATA\grafel\tasks\com.grafel.daemon.xml"
# Expected: file not found error
```

### 11. Verify logon persistence (cold-boot test)

1. Install the task: `.\grafel.exe install`
2. Log off and log back in (or restart the machine).
3. After login, run: `.\grafel.exe status`
4. Expected: `running=true` — the task fired at logon.

---

## Expected file paths

| Artifact | Path |
|---|---|
| Task XML (staged) | `%LOCALAPPDATA%\grafel\tasks\com.grafel.daemon.xml` |
| Daemon logs | `%APPDATA%\grafel\logs\` |
| Named pipe | `\\.\pipe\grafel-daemon-<lowercased-username>` |

---

## Known limitations in this release

- **No stdout/stderr log files.** Unlike macOS (plist `StandardOutPath`) and Linux (systemd journal), Windows Task Scheduler does not redirect process output to a file natively. The daemon writes logs to `%APPDATA%\grafel\logs\` via its own logger. A future issue (#933) may wire file-based log redirection via XML `<StandardOutput>` extensions.
- **No code-signing.** The binary is unsigned. Windows SmartScreen may warn on first run. Administrators can unblock the binary via `Unblock-File .\grafel.exe`.
- **UAC elevation not required.** The task runs at `LeastPrivilege` (user-level). If elevated tasks are needed in future, the installer would need to be updated — but that is out of scope for this release.
- **Crash-restart limited to 3 attempts.** After 3 failures within 1 minute, Task Scheduler stops restarting. Manual intervention via `grafel install` is required to reset the retry counter.

---

## Troubleshooting

### Task appears in schtasks but daemon not running

Check whether the binary path in the task XML is correct:

```powershell
schtasks /query /tn com.grafel.daemon /xml
```

Look at the `<Command>` element. If the path is wrong, run `.\grafel.exe uninstall && .\grafel.exe install` from the directory where `grafel.exe` lives.

### `schtasks /create` fails with access denied

This should not happen at `LeastPrivilege`. If it does, check whether Group Policy on the machine restricts Task Scheduler registration for non-admins (`Computer Configuration > Windows Settings > Security Settings > Local Policies > User Rights Assignment > Increase scheduling priority`).

### Named pipe not appearing after install

The task may have started but crashed immediately. Check the Windows Event Log:

```powershell
Get-EventLog -LogName Application -Source "Task Scheduler" -Newest 20 | Format-List
```

Also check for grafel-specific errors:

```powershell
Get-Content "$env:APPDATA\grafel\logs\daemon.log" -Tail 50
```

---

---

## Per-repo watcher smoke-test procedure (added by #933)

The watcher is a separate scheduled task — one per watched repository — and
follows the same install/uninstall/status lifecycle as the daemon service.
Each task name uses the reverse-DNS convention
`com.grafel.watcher.<group>.<repo-slug>-<hash>`.

> **Changed in #6183.** `<repo-slug>` used to be the repo's directory name
> alone, so two repos in one group with the same directory name — `api`, `web`,
> `docs` across different orgs — produced **one** task name for two repos, and
> only one of them was ever actually watched. The slug now carries `-<hash>`,
> eight hex characters derived from the repo's full path, e.g.
> `com.grafel.watcher.mygroup.myrepo-3f9c1e07`.
>
> The literal task names in the commands below are therefore **examples, not
> values you can copy**. Get the real one from the staged XML directory:
>
> ```powershell
> ls "$env:LOCALAPPDATA\grafel\tasks\com.grafel.watcher.mygroup.*.xml"
> ```
>
> Upgrading renames existing tasks. `grafel install` and `grafel update`
> deregister the old task name and delete its staged XML before creating the new
> one, so a repo ends up with exactly one task — not two. **Downgrading does
> not:** an older binary derives the un-hashed name, does not find it, and
> creates it alongside the hashed task that is still registered. If you roll
> back, delete the hashed tasks by hand:
>
> ```powershell
> schtasks /delete /tn com.grafel.watcher.mygroup.myrepo-3f9c1e07 /f
> ```

### W1. Install a watcher for a repo

```powershell
# Assuming a group named "mygroup" and a repo at C:\src\myrepo
.\grafel.exe install --group mygroup --repo C:\src\myrepo --watchers
```

Expected output (approximate):
```
watcher installed: com.grafel.watcher.mygroup.myrepo-3f9c1e07  running=true
```

### W2. Verify the watcher task exists

```powershell
schtasks /query /tn com.grafel.watcher.mygroup.myrepo-3f9c1e07 /fo list /v
```

Key fields:

| Field | Expected value |
|---|---|
| `Task Name` | `\com.grafel.watcher.mygroup.myrepo-3f9c1e07` |
| `Status` | `Running` |
| `Logon Mode` | `Interactive/Background` |

### W3. Verify the task XML was staged to disk

```powershell
ls "$env:LOCALAPPDATA\grafel\tasks\com.grafel.watcher.mygroup.myrepo-3f9c1e07.xml"
# Expected: file exists
```

### W4. Verify idempotency

```powershell
.\grafel.exe install --group mygroup --repo C:\src\myrepo --watchers
# Expected: no error; returns current status without modifying anything
```

### W5. Uninstall the watcher

```powershell
.\grafel.exe uninstall --group mygroup
```

After uninstall:

```powershell
schtasks /query /tn com.grafel.watcher.mygroup.myrepo-3f9c1e07
# Expected: ERROR: The system cannot find the file specified.

ls "$env:LOCALAPPDATA\grafel\tasks\com.grafel.watcher.mygroup.myrepo-3f9c1e07.xml"
# Expected: file not found

# Uninstall also removes any task left over from a pre-#6183 install, i.e. the
# un-hashed name. It is the last moment at which that name can be derived.
schtasks /query /tn com.grafel.watcher.mygroup.myrepo
# Expected: ERROR: The system cannot find the file specified.
```

### W6. Expected file paths (per-repo watcher)

| Artifact | Path |
|---|---|
| Task XML (staged) | `%LOCALAPPDATA%\grafel\tasks\com.grafel.watcher.<group>.<slug>-<hash>.xml` |
| Task name | `com.grafel.watcher.<group>.<slug>-<hash>` |

`<hash>` is the first 8 hex characters of SHA-256 over the repo's cleaned
absolute path (#6183). It makes the task name unique per repo PATH rather than
per directory name.

---

## Sibling issues

- **#856** — Platform parity parent epic
- **#933** — SchtasksXML watcher unit — this PR
- **#935** — CI matrix for Windows runner
- **#937** — tree-sitter CGO bindings on Windows
