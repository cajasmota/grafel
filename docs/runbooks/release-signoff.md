# Runbook — real-machine release sign-off

**Scope:** the OS-service-supervision checks a human must run on **real,
logged-in machines** before tagging a grafel release. CI
([`.github/workflows/acceptance.yml`](../../.github/workflows/acceptance.yml))
already proves install → `grafel selftest` → uninstall on ubuntu / windows /
macos. This runbook covers only the **gaps CI cannot prove**:

- GitHub macOS runners are **headless**, so launchd **gui-session** supervision
  (the `com.grafel.daemon` LaunchAgent under `gui/$UID`) is never exercised —
  including the [#5222](https://github.com/cajasmota/grafel/issues/5222)
  `osusergo` regression where the daemon hangs on the cgo OpenDirectory
  `os/user` lookup under a launchd gui session.
- CI runners are **ephemeral** — they cannot **reboot** and prove the service
  comes back (launchd `KeepAlive`/`RunAtLoad`, systemd `Restart=on-failure` +
  linger, Windows Scheduled-Task persistence).

CI exercises the *functional* ladder via `grafel selftest` (its own isolated
in-process daemon, no service manager). This runbook is the *supervision*
ladder. Run it on real hardware for each release.

> Related: [RELEASING.md](../RELEASING.md) (versioning + tag process),
> [platform/windows-service.md](../platform/windows-service.md) (detailed
> Windows Task-Scheduler smoke test), epic
> [#4457](https://github.com/cajasmota/grafel/issues/4457).

---

## 0. Pre-tag gate (do this first, off any machine)

- [ ] `acceptance.yml` has a **green** `workflow_dispatch` run on the release
      commit across **all three** OSes (ubuntu-latest, windows-latest,
      macos-14). Re-dispatch if the last run predates the commit.
- [ ] `CHANGELOG.md` has a `[X.Y.Z] — YYYY-MM-DD` section; `[Unreleased]` is
      drained.
- [ ] `go build ./...` and `go vet ./...` are clean locally.
- [ ] Version string is correct: `grafel --version` on a freshly built binary
      matches the tag to be cut.

---

## 1. macOS — real Mac, logged into a **GUI session**

> Must be a physical/VM Mac with an **interactive login** (not SSH-only, not a
> headless runner). The launchd job is a **LaunchAgent** in the user's
> `gui/$UID` domain; without a gui session there is nothing to supervise.

Service facts (per [ADR-0017](../adrs/0017-single-binary-daemon-architecture.md)):
- Plist: `~/Library/LaunchAgents/com.grafel.daemon.plist`
- `RunAtLoad=true`, `KeepAlive=true`; logs to `~/.grafel/logs/daemon.log`
- Dashboard listener default: `http://127.0.0.1:47274/`

Checklist:

- [ ] **Install:** `grafel install` completes without error and reports the
      daemon service installed.
- [ ] **#5222 osusergo bind check** — daemon comes up under launchd and
      **binds the dashboard port within ~15s** (a hang here is the
      OpenDirectory/`os/user` regression):
      ```bash
      launchctl print gui/$(id -u)/com.grafel.daemon   # job present, state=running, no spin
      for i in $(seq 1 15); do
        curl -fsS http://127.0.0.1:47274/ >/dev/null 2>&1 && { echo "bound in ${i}s"; break; }
        sleep 1
      done
      grafel status            # daemon running, pid present
      ```
- [ ] **Functional:** `grafel selftest` exits 0.
- [ ] **Doctor:** `grafel doctor` reports the service healthy.
- [ ] **Reboot persistence (KeepAlive/RunAtLoad):** reboot the Mac, log back
      into the GUI session, **without** running any grafel command verify the
      daemon auto-restarted:
      ```bash
      launchctl print gui/$(id -u)/com.grafel.daemon   # running again
      curl -fsS http://127.0.0.1:47274/ >/dev/null && echo "auto-restarted OK"
      grafel status
      ```
- [ ] **Clean uninstall:** `grafel uninstall --yes --purge --remove-binary`
      removes everything:
      ```bash
      grafel uninstall --yes --purge --remove-binary
      launchctl print gui/$(id -u)/com.grafel.daemon 2>&1 | grep -qi 'could not find' && echo "launchd job gone"
      test ! -e ~/Library/LaunchAgents/com.grafel.daemon.plist && echo "plist gone"
      test ! -d ~/.grafel && echo "~/.grafel gone"
      # MCP entry removed from each AI-tool config (e.g. ~/.claude/.claude.json):
      grep -L grafel ~/.claude/.claude.json
      ```

---

## 2. Windows — real machine

> The daemon is supervised by a **Scheduled Task** named `com.grafel.daemon`
> (not a Windows Service); runs as the current user, no administrator needed.
> Full field-by-field smoke test:
> [platform/windows-service.md](../platform/windows-service.md). This runbook
> adds the **reboot** check the ephemeral CI runner cannot do.

Run in PowerShell as the **current user**:

- [ ] **Install:** `.\grafel.exe install` (or `grafel install` if on PATH)
      reports `installed=true running=true pid=<N>`.
- [ ] **Task registered + running + binds:**
      ```powershell
      schtasks /query /tn com.grafel.daemon /fo list /v | Select-String "Status"   # Running
      grafel status
      (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:47274/).StatusCode      # 200
      ```
- [ ] **Functional:** `grafel selftest` exits 0.
- [ ] **Doctor:** `grafel doctor` reports the service healthy.
- [ ] **Reboot persistence (CI cannot test this):** reboot Windows, log back in,
      and **without** running any grafel command:
      ```powershell
      schtasks /query /tn com.grafel.daemon /fo list /v | Select-String "Status"   # Running again
      (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:47274/).StatusCode      # 200
      ```
- [ ] **Clean uninstall:**
      ```powershell
      grafel uninstall --yes --purge --remove-binary
      schtasks /query /tn com.grafel.daemon    # ERROR: cannot find the file specified
      Test-Path "$env:USERPROFILE\.grafel"     # False
      ```
      Confirm the MCP entry is gone from each AI-tool config (e.g.
      `%USERPROFILE%\.claude\.claude.json`).

---

## 3. Linux — real machine / systemd box

> The daemon is supervised by a **systemd user unit**
> `~/.config/systemd/user/grafel.service` with `Restart=on-failure`,
> `WantedBy=default.target` (per [ADR-0017](../adrs/0017-single-binary-daemon-architecture.md)).
> For the service to survive a **reboot / logout** you need **linger** enabled
> for the user.

- [ ] **Install:** `grafel install` completes and reports the service installed.
- [ ] **Unit active + binds:**
      ```bash
      systemctl --user is-active grafel.service          # active
      grafel status
      curl -fsS http://127.0.0.1:47274/ >/dev/null && echo "bound OK"
      ```
- [ ] **Functional:** `grafel selftest` exits 0.
- [ ] **Doctor:** `grafel doctor` reports the service healthy.
- [ ] **Persistence across re-login/reboot:** ensure linger is on
      (`loginctl enable-linger "$USER"`), then either reboot or
      `loginctl terminate-user "$USER"` and log back in, and **without**
      running any grafel command:
      ```bash
      systemctl --user is-active grafel.service          # active again
      curl -fsS http://127.0.0.1:47274/ >/dev/null && echo "persisted OK"
      ```
- [ ] **Clean uninstall:**
      ```bash
      grafel uninstall --yes --purge --remove-binary
      systemctl --user is-active grafel.service 2>&1 | grep -qi inactive && echo "unit stopped"
      test ! -e ~/.config/systemd/user/grafel.service && echo "unit file gone"
      test ! -d ~/.grafel && echo "~/.grafel gone"
      ```

---

## 4. Reindex / memory env knobs (note in the sign-off if changed from default)

These affect daemon behaviour under supervision; the daemon picks them up from
its environment at start. Verify defaults unless a release intentionally changes
them (`grafel doctor` prints the first two):

| Env var | Default | Effect |
|---|---|---|
| `GRAFEL_INCREMENTAL_REINDEX` | **on** | Incremental reindex on file change (vs full re-extract). |
| `GRAFEL_SUBPROCESS_INDEXER` | **off** | Run indexing in a subprocess (memory isolation). |
| `GRAFEL_DAEMON_MEMLIMIT_MB` | unset / soft-cap | Soft RSS cap for the daemon; `off`/`0`/negative disables. |

If a release changes any default, re-run the per-OS reboot-persistence check
with the new value exported in the service environment.

---

## 5. Sign-off table

Fill one row per OS. Ship only when every cell is ✅ (or has a tracked waiver).

| OS | Installed | Daemon binds (~15s) | `selftest` 0 | Reboot-persist | Clean uninstall | Tester / date |
|---|:--:|:--:|:--:|:--:|:--:|---|
| macOS (gui session) |   |   |   |   |   |   |
| Windows |   |   |   |   |   |   |
| Linux (systemd + linger) |   |   |   |   |   |   |

**Rollback note:** if any row fails, **do not tag**. If a tag/release was
already published, mark the GitHub Release as a draft (or delete the tag),
revert the offending commit on `main`, and re-run the pre-tag gate from §0. A
failed launchd **bind** on macOS specifically points at the #5222 `osusergo`
build (`go build -tags osusergo`) — confirm the release artifact was built with
that tag before re-cutting.
