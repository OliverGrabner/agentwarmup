# Testing and maintenance

Ordinary tests use fake providers. They do not send model requests, log in, or register real scheduled jobs.

```sh
go test ./...
go vet ./...
go test -race ./...
npm --prefix npm test
node --test .github/scripts/package-npm.test.mjs
```

Race tests require a C compiler. Launcher and package tests require Node 22 or later with npm; no npm dependencies need installing. The installed Go executable needs only a supported provider CLI and the operating system scheduler.

The tests cover reset-day rollover and DST, activation and lateness, concurrent duplicate prevention, interrupted attempts, authentication/result parsing, process-tree timeouts, setup cancellation, update recovery, rollback, and uninstall coordination. `testdata/fakeprovider` is a small offline executable for process and scheduler tests.

Launcher tests cover version/platform selection, HTTPS restrictions, download limits, both executable hashes, child arguments and exit codes, cancellation, and temporary-file cleanup. Package tests run actual `npm pack` with fake executable bytes, inspect the tarball, and verify all six gzip assets. They do not download or invoke provider CLIs.

## Interactive setup preview

The opt-in terminal preview uses fake providers, fake jobs, and a temporary config directory. It can safely exercise Enter-through setup or cancellation without changing your installation. On Windows:

```powershell
go test -c -o build/cli-terminal.test.exe ./internal/cli
$env:AGENTWARMUP_TEST_TERMINAL = '1'
& ./build/cli-terminal.test.exe '-test.run=^TestTerminalPreview$' '-test.v'
Remove-Item Env:AGENTWARMUP_TEST_TERMINAL
```

Run the compiled test directly because `go test` captures its output. Use a terminal at least 80 columns by 16 rows. `TERM=dumb` deliberately selects plain prompts; the interactive preview requires a terminal with cursor control. The preview checks that terminal input mode is restored on return.

## Native scheduler checks

Real scheduler tests are opt-in. They must use a temporary application root and a root-derived job name. Read the test source before enabling the native suite; never point it at an existing installation. A passing fake-provider test proves execution and lifecycle behavior, not provider reset timing.

Build the app and offline fixture, then run the guarded scheduler test. On Windows, in PowerShell:

```powershell
go build -o build/e2e-agentwarmup.exe .
go build -o build/fakeprovider.exe ./testdata/fakeprovider
$env:AGENTWARMUP_E2E = '1'
$env:AGENTWARMUP_E2E_EXE = (Resolve-Path build/e2e-agentwarmup.exe).Path
$env:AGENTWARMUP_E2E_PROVIDER = (Resolve-Path build/fakeprovider.exe).Path
go test ./internal/job -run TestScheduledGuardedFake -v -count=1 -timeout 4m
Remove-Item Env:AGENTWARMUP_E2E, Env:AGENTWARMUP_E2E_EXE, Env:AGENTWARMUP_E2E_PROVIDER
```

The test creates and removes its own job. It checks a scheduled request, duplicate suppression, pause, and resume without replay. The separate `TestIsolatedSchedulerLifecycle` checks registration and OS enable/disable behavior.

## Recovery

Run `agentwarmup setup` after an interrupted setup or update. A small transaction journal preserves the previous settings and job state. Requests remain blocked until recovery succeeds. If uninstall was interrupted, rerun `agentwarmup uninstall`.

Application settings and attempt records live under `%LOCALAPPDATA%/AgentWarmup` on Windows, `~/Library/Application Support/AgentWarmup` on macOS, and `$XDG_CONFIG_HOME/agentwarmup` (or `~/.config/agentwarmup`) on Linux. No provider credentials are copied there.

Uninstall removes the scheduled job and owned application files. A small empty lock file beside the application directory is retained so concurrent installs cannot race with removal. It contains no settings or credentials.

The unreleased prototype used fixed scheduler names. Before migrating one of those installations, remove its old job after confirming that its command points to that prototype. The new installer does not delete a job just because its name matches. Provider CLIs and their authentication stay in place.

## Release evidence

Checked on Windows amd64, 2026-09-08:

- Unit and offline process tests, `go vet`, and the race detector passed.
- All six Windows/macOS/Linux amd64/arm64 targets cross-compiled.
- The native scheduler invoked the actual guarded runner with the offline fixture at `2026-09-08T19:34:00Z`. Exactly one request was recorded; repeated checks and pause/resume sent no extra request. Its isolated job was removed.
- Deferred Windows self-removal passed from a copied executable inside its own test installation. The helper removed that installation after exit and preserved a neighboring sentinel file. This did not modify user PATH or provider settings.
- Full local preview packaging passed: six native archives, six gzip executables, the npm tarball, installers, and checksums were generated. Every checksum verified; the Windows archive and npm package both reported `0.1.0-rc.1`. No script policy was changed.
- All 13 launcher tests and 15 packaging tests passed on Node 22.14.0 / npm 10.9.2. The generated tarball ran through `npx` from a fresh isolated cache in offline mode for help/version; this did not install a schedule.
- Real Windows terminal checks with fakes passed for defaults, custom time/day selection, Escape, and Ctrl-C, including restoration of input/output console modes. A fake setup launched through the npm wrapper also passed defaults and Ctrl-C, and its temporary executable was removed.
- The setup regression checks that job registration receives the permanent installed executable path. Live cache-removal/scheduled-execution acceptance remains pending.

These local results do not include a published npm download/install, clean-machine setup, macOS/Linux native execution, or a real subscription request. Local preview artifacts are under `build/release-preview/` and are excluded from source control.

On 2026-09-09, [GitHub CI passed](https://github.com/OliverGrabner/agentwarmup/actions/runs/34379607953) on Windows, macOS, and Linux with Node 22, plus launcher/package checks on Linux with Node 24. The fixes account for temporary-directory aliases in package verification and test fixtures; installer symlink checks remain enforced.

## Published preview checks

Checked [v0.1.0-rc.2](https://github.com/OliverGrabner/agentwarmup/releases/tag/v0.1.0-rc.2) on 2026-09-09:

- The [release workflow](https://github.com/OliverGrabner/agentwarmup/actions/runs/34380977113) passed. All 16 downloaded assets matched `checksums.txt`; the package contains six files and no dependencies or installation hooks.
- [Published-package checks](https://github.com/OliverGrabner/agentwarmup/actions/runs/34381525596) passed on Windows x64 and macOS ARM64 with Node 22.23.2, and Linux x64 with Node 22.23.2 and 24.20.0. Each used a fresh npm cache, verified the release tarball, and ran native status through the launcher. No application or scheduler job was created; the temporary executable was removed.
- The exact GitHub-package URL in the README also downloaded and ran native status locally through `npx`, using a fresh cache.
- Windows x64 installation through the published launcher passed with an isolated fake Codex. Installed setup/edit, status, pause/resume, same-version update, and uninstall passed. Update preserved settings and activation time. Uninstall removed the owned files, job, and PATH entry; existing jobs and user PATH were unchanged afterward. No request occurred during these lifecycle commands.
- Windows Task Scheduler separately ran the published executable with the offline fixture at **17:12:00 UTC**. Exactly one request was recorded; duplicate checks and pause/resume sent no extra request. Its isolated job was removed.
- npm's publication dry run passed with the explicit `preview` tag. Actual registry publication was rejected; npm account authentication/permissions remain unresolved. The public GitHub tarball works independently of registry publication.

Local sanitized reports are under `build/installer-acceptance/` and downloads under `build/published-rc2/`, both ignored by Git. These checks do not establish clean-machine OS trust behavior, full native lifecycle on macOS/Linux, other architectures, or provider reset timing.

Live validation is tracked separately in [validation.md](validation.md). Native platform acceptance and clean-machine installation are tracked in [pre-publish.md](pre-publish.md). Cross-compilation does not establish native compatibility, and neither a successful request nor a passing fake establishes the reset promise.
