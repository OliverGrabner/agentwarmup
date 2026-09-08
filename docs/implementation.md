# Implementation plan

Target: a polished first release of AgentWarmup, with one local scheduler and independently validated Codex and Claude adapters. The implementation is a development preview; live provider and release acceptance remain pending.

Product behavior lives in [product.md](product.md). Provider evidence lives in [validation.md](validation.md). Publication requirements live in [pre-publish.md](pre-publish.md).

## 1. Decisions

| Area | Decision |
| --- | --- |
| Delivery | npm launcher for Node users; PowerShell/POSIX installers for users without Node; all install the same Go executable |
| Runtime | User's computer and existing provider CLI login; no application backend |
| Providers | Codex and Claude Code; each must pass its own validation before being advertised |
| Schedule | One target reset time and set of reset days, shared by selected providers |
| Execution | OS invokes a short internal check every minute; provider processes start only when due |
| Late runs | Up to two minutes; skip older occurrences, with no catch-up or retries |
| Interface | Small terminal setup and status display; no dashboard, account, or telemetry |
| Updates | Rerun the installer; preserve settings and replace the scheduled executable |
| Public name | AgentWarmup; repository: [OliverGrabner/agentwarmup](https://github.com/OliverGrabner/agentwarmup); executable: agentwarmup |

A little more implementation work is justified when it removes work from every installation. Keep the code ordinary: a few small packages, explicit errors, and no general plugin framework.

## 2. Validate before implementing the release

Implement and test with fake providers while live validation is pending. On 2026-09-08 the project owner moved the live experiments from a coding prerequisite to a strict release gate. Complete the idle-window, active-window, isolation, and scheduled experiments in [validation.md](validation.md) before advertising or publishing validated provider support. A successful subprocess is not evidence of the desired reset.

For each adapter, establish:

1. The exact CLI version, subscription plan, model, arguments, and effective authentication method.
2. Two independent idle-window requests that start the relevant five-hour allowance.
3. An active-window request that leaves the existing reset unchanged.
4. The effect on the allowance the user actually exhausts, including any separate model limit.
5. Subscription authentication works with integrations and inherited instructions disabled.
6. The final response is OK, no tools or hooks run, and no conversation history is retained.

Resolve model and flag choices here, then freeze them in the adapter and its fixtures. Codex's first candidate is gpt-6-astra with low reasoning. Select Claude's exact model only after confirming which limit it affects; do not assume a cheaper model starts every model-specific allowance.

If an adapter fails these checks, exclude it from the release. If neither works, stop the reset-scheduling release. Never silently substitute an API request, another model, or a different allowance to make a test pass.

Installer implementation and private development builds may proceed. Release claims and stable publication depend on this gate passing.

## 3. Repository layout

Target layout; add source files when their implementation starts, rather than committing empty scaffolding.

    .github/
      release.json
      scripts/
        release.ps1
        package-npm.mjs
        package-npm.test.mjs
      workflows/
        ci.yml
        release.yml
      ISSUE_TEMPLATE/
        bug.yml
    docs/
      product.md
      implementation.md
      validation.md
      pre-publish.md
    internal/
      cli/
        cli.go
        setup.go
        setup_choices.go
        terminal.go
        terminal_windows.go
        terminal_unix.go
        terminal_test.go
        status.go
        cli_test.go
      config/
        config.go
        paths.go
        config_test.go
      schedule/
        schedule.go
        timezone.go
        timezone_windows.go
        timezone_unix.go
        schedule_test.go
      provider/
        provider.go
        codex.go
        claude.go
        environment.go
        provider_test.go
        testdata/
      runner/
        runner.go
        state.go
        lock_windows.go
        lock_unix.go
        process_windows.go
        process_unix.go
        runner_test.go
      job/
        job.go
        windows.go
        darwin.go
        linux.go
        job_test.go
        e2e_test.go
      install/
        install.go
        windows.go
        unix.go
        install_test.go
    testdata/
      fakeprovider/
        main.go
    npm/
      package.json
      manifest.json
      README.md
      bin/agentwarmup.js
      lib/launcher.js
      test/launcher.test.js
    install.sh
    install.ps1
    main.go
    go.mod
    go.sum
    .editorconfig
    .gitattributes
    .gitignore
    AGENTS.md
    CLAUDE.md
    LICENSE
    README.md

main.go remains the thin entry point. Keep config and schedule independent of providers and UI. The provider package owns CLI detection, authentication checks, invocation descriptions, and result classification. The runner owns execution, deadlines, and persisted outcomes. The job package only registers and queries OS jobs. The install package owns AgentWarmup files and PATH changes.

Use the standard library for most work. Add small, maintained dependencies only for a concrete need such as portable terminal detection or OS locking; record why. No CGO requirement. go.sum exists only if dependencies require it.

The keyboard UI uses `golang.org/x/term` for portable terminal detection and raw-input restoration, plus `golang.org/x/sys/windows` to preserve console output modes. The npm launcher uses only Node's built-in modules.

## 4. Setup and command contract

Public commands:

| Command | Behavior |
| --- | --- |
| agentwarmup | Status when configured; setup only when configuration is absent |
| agentwarmup setup | Create or edit the schedule and selected providers |
| agentwarmup pause | Disable future requests for all selected providers |
| agentwarmup resume | Re-enable future requests; never run immediately |
| agentwarmup uninstall | Remove the job, owned files, and owned PATH entry |
| --help / --version | Brief help and build version |

Keep the existing status alias. Accept the old internal warmup command as an alias of the guarded check during prototype migration. Do not expose a public run-now command.

Setup sequence:

1. Check OS support and user-scheduler availability before modifying anything.
2. Detect supported CLIs in parallel using paths and version checks, without model requests.
3. If one eligible provider is present, select it. If several are present, offer Both, Codex, or Claude with arrow keys and Enter; default to Both. Preserve previous selections on edit.
4. Explain missing or unsupported providers only when relevant. Offer the selected provider's official login flow when signed out, then return to setup. Never install a provider or change its login silently.
5. Offer a few reset-time presets and a custom time; default to 10:00 AM. Offer weekdays, every day, or custom days; default to weekdays. Custom days use Space to toggle and Enter to continue. Display the detected timezone.
6. Print the next actual trigger and target reset, selected providers, and the awake/online/logged-in requirement.
7. Confirm, commit settings and job registration, then print the change command.

Do not ask about models, reasoning effort, cron, catch-up, paths, or logging in the normal flow. A failed prerequisite gets one explanation and one actionable command.

Input rules:

- Support 10:00, 10am, 9:30 AM, and 17:00.
- Support weekdays, everyday, weekends, and comma-separated days.
- Use canonical values for edit defaults. Monday-Friday display text must not become an invalid parser default.
- EOF or cancellation aborts setup; never silently accepts defaults or spins on invalid input.
- Redirected stdin must not accidentally become the setup answers or provider prompt.
- Configuration errors produce repair instructions. A corrupt file does not trigger first-run setup.
- Use color only on a capable terminal, honor NO_COLOR, and provide ASCII fallbacks.
- One final confirmation; no decorative progress delays or repeated consent prompts.
- Keep menus in normal scrollback. Restore terminal mode on success, errors, Escape, Ctrl-C, and EOF. Use numbered/text prompts when interactive cursor control is unavailable.

### npm delivery and setup implementation

Approved on 2026-09-08. Implement locally; publishing remains a separate step after the existing release checks.

1. Add a small npm launcher with no runtime dependencies or install hooks. Require Node 22 or later only for this install route. Without arguments it runs the executable's installer: first run opens setup, later runs update while preserving settings.
2. Package the existing Go builds as single-file gzip downloads. Each npm release embeds its exact version, GitHub repository, asset names, and SHA-256 hashes of both compressed and executable bytes. Never resolve a second latest release during installation.
3. Download through HTTPS, check size/time limits and hashes, then run the executable with the user's terminal attached. Use a unique temporary directory and clean it after exit. The OS job must refer to the permanent installed executable, so clearing npm's cache does not affect scheduling.
4. Add the compact keyboard setup described above. Keep authentication, timezone detection, schedule calculation, confirmation, and transactional installation in Go, shared by every install route.
5. Generate a reviewable npm tarball alongside release assets. Keep the source package private and unresolved until packaging; do not automatically publish to npm. Preserve stable provider/platform gates and use an explicit preview npm tag for candidate testing.
6. Test launcher downloads, corrupt payloads, platform selection, cancellation, forwarded arguments, package contents, and installed-path independence with fakes. Test keyboard navigation, defaults, custom values, terminal restoration, and cancellation without registering a real job.
7. Keep current README instructions honest until artifacts exist. Move local build instructions into development documentation, retain the ASCII example, and show the real npm command only after publication and installation acceptance.

The npm registry and GitHub releases distribute files; AgentWarmup operates no backend. Native installers remain available for people without Node. The normal released path is one command, provider selection only when needed, reset time, days, and one schedule confirmation.

Status must query the OS job as well as configuration. Show paused, enabled, or needs repair; next trigger; target reset; and a separate last outcome per provider. Say Request succeeded rather than Reset verified unless actual provider reset evidence was obtained. Display Target reset for predictions everywhere.

## 5. Configuration and local files

Use schema version 1 with these fields:

| Field | Purpose |
| --- | --- |
| schemaVersion | Strict migration and compatibility checks |
| resetTime | Normalized HH:MM |
| resetDays | Canonical weekday list |
| timezone | IANA location detected at setup |
| enabled | Global pause state |
| providers | Selected IDs and non-secret executable/config-location metadata |
| effectiveFrom | UTC commit time; no occurrence earlier than this is eligible |

Persist only necessary, non-secret provider settings: resolved executable/launcher paths, effective config root when nondefault, and auth-store selection if needed. Never persist arbitrary environment snapshots, account emails, API keys, cookies, or provider credential files.

Keep application-owned files under a stable per-user root:

| OS | Root |
| --- | --- |
| Windows | %LOCALAPPDATA%/AgentWarmup |
| macOS | ~/Library/Application Support/AgentWarmup |
| Linux | $XDG_CONFIG_HOME/agentwarmup, otherwise ~/.config/agentwarmup |

Root contents: bin/, config.json, per-provider state files, locks/, and separate empty run directories. Bound retained state to recent occurrence records plus the last result; retain at least eight days so clock changes cannot re-enable a recently attempted occurrence. Prune only outside that interval.

Use restrictive user permissions, unique temporary files, file sync, and platform-appropriate atomic replacement. If state cannot be read or persisted safely, do not send a request. A fixed shared .tmp filename is not safe for concurrent writes.

Load the prototype schema without side effects. During explicit setup, migrate Codex path, time, days, and pause state; detect timezone and discard catch-up after clearly saying late requests will now be skipped. Preserve the previous config for rollback until the transaction succeeds.

Do not change schedule activation time on a routine software update. Changing setup or resuming advances effectiveFrom so an occurrence from before the change cannot run. Preserve occurrence records across edits.

## 6. Time calculation

Keep a pure occurrence engine and embed time/tzdata. Store a timezone explicitly; the scheduler never decides the user's reset calendar.

    Occurrence
      ResetAt    time.Time
      TriggerAt  time.Time
      Key        string

Resolve a selected reset date and wall time into ResetAt, then compute TriggerAt by subtracting the adapter's validated five-hour duration. Generate keys from provider ID plus the reset instant in UTC, never from process start time.

Timezone rules:

- Detect local IANA identity using OS timezone configuration; on Windows use a tested Windows-to-IANA mapping with documented provenance.
- Do not infer a timezone from its current UTC offset or abbreviations such as CST.
- If detection is inconclusive, ask once for an IANA timezone. Never silently choose UTC.
- Keep the saved zone when the user travels. Status can note a local-zone mismatch and direct them to setup.
- On an ambiguous reset wall time, use the earlier instant.
- Skip a nonexistent reset wall time and show the next valid occurrence.
- Apply the selected days to the reset date, including triggers on the preceding day.
- Verify wall-time candidates by converting back into the selected location; do not accept time.Date normalization as gap handling.

Due means TriggerAt <= now <= TriggerAt + 2 minutes and TriggerAt >= effectiveFrom. No request before its trigger. Compare instants for deadlines; use the monotonic clock for process timeout.

## 7. One OS job

Register a lightweight check every minute. On the normal idle path it reads local files, computes due occurrences, and exits without launching any provider CLI or making network requests.

This centralizes DST, late-run rules, and multiple-provider scheduling. Native weekly schedules would require separate dynamic trigger updates to preserve five elapsed hours at DST boundaries.

| OS | Registration contract |
| --- | --- |
| Windows | Task Scheduler repeating trigger, current interactive user, least privilege, ignore overlapping checks |
| macOS | LaunchAgent with a 60-second interval, current GUI login session |
| Linux | systemd user service and minute calendar timer, no root or lingering requirement |

Request an immediate lightweight check on registration/login where supported. effectiveFrom prevents a setup-time request. OS delivery is best effort; the application applies the two-minute guard on every invocation.

Do not request wake, keep-awake, or log-in changes in v1. Do not gate the Windows check on network availability: it still needs to record missed or failed runs. On battery, run the same lightweight check without changing power settings.

Use structured serialization for task XML/plist and correct systemd argument escaping. Test spaces, Unicode, ampersands, percent signs, and home directories outside the usual locations. Never interpolate executable paths into an unescaped shell command.

Windows scheduled checks and their child processes must open no console windows. Background process launch behavior gets a real-machine acceptance check.

Provide Install, Inspect, and Uninstall operations with idempotent registration. Real tests use unique job names and isolated roots; never replace a user's existing AgentWarmup job.

## 8. Provider adapters

Keep a closed registry for two providers. No dynamically loaded adapters or user-supplied prompts.

    Adapter
      Detect(ctx) -> Installation
      CheckAuth(ctx, installation) -> AuthStatus
      Login(ctx, installation) -> error
      Request(installation, runDir) -> Invocation
      Classify(exitCode, stdout, stderr) -> Result

Invocation contains executable, argv, cwd, and the carefully prepared environment. The runner executes it. AuthStatus distinguishes subscription, signed out, API/cloud billing, unsupported, and unknown.

Detection supports stable native installs and tested npm launchers. Prefer a real executable. If a shim needs an interpreter, represent interpreter and argv explicitly and test it; do not assume direct .cmd invocation works everywhere. Rediscover once if a previously stored executable disappears, but never update the provider automatically.

Check authentication using the same config location and effective environment as the scheduled request. Use documented status commands, parse only necessary fields, and discard raw status output. Never use credential-file existence as proof of subscription access. Probes must not log users out or change their global configuration. CLI login status may not detect every revoked token; runtime auth failures remain possible.

### Codex

Use codex login status and the existing normal login flow. Candidate request: codex exec, gpt-6-astra, low reasoning, ephemeral history, read-only sandbox, and skip-git-repo-check in an empty owned directory.

The final arguments must also disable inherited instructions, hooks, skills, plugins, MCP integrations, and configuration-driven provider overrides. Confirm supported flags against the minimum supported CLI. Ignoring user config must not accidentally change the credential store or config root.

Preserve OS-managed enterprise restrictions. If isolation cannot coexist with required managed configuration, report unsupported configuration rather than bypassing it.

Reference: [execution](https://learn.chatgpt.com/docs/non-interactive-mode), [authentication](https://learn.chatgpt.com/docs/auth).

### Claude

Use claude auth status JSON and claude auth login. Use a fresh claude -p request with the model and low-cost settings established by validation.

Do not use --bare: the current documentation says it ignores subscription OAuth and the system keychain. Validate an explicit combination of setting-source controls, disabled hooks/memory/instructions, disabled tools and MCP, and no session persistence while preserving the saved subscription login. If the required isolation cannot be achieved, this adapter does not ship.

The prompt alone is not a tool restriction. Do not use permission-bypass flags. Inspect structured result errors even if a process exits successfully.

Reference: [CLI](https://code.claude.com/docs/en/cli-reference), [programmatic usage](https://code.claude.com/docs/en/headless).

### Environment and outcomes

Strip billing/provider override variables from the child environment, including OpenAI API overrides and Claude API/token/cloud-provider selectors; Windows environment names are case-insensitive. Do not mutate the user's environment. Preserve the necessary home/config locations, system executable lookup, and supported proxy/CA settings. Reject an effective auth method that remains API-backed or unknown.

Pass stdin as a closed empty stream, not inherited terminal input. Use separate bounded output buffers; retain only classified, redacted diagnostics up to 200 characters, never raw provider JSON or transcripts. Do not save account identifiers.

Result categories: succeeded, auth_required, rate_limited, model_unavailable, timeout, failed, missed, interrupted. Store timestamps and the occurrence identity. An unknown error stays failed; do not guess auth from any arbitrary appearance of the word "auth".

## 9. Execution and duplicate prevention

At each check:

1. Read and validate config; return immediately when disabled.
2. Compute due and recently missed occurrences for each selected provider.
3. Acquire the provider's nonblocking OS lock. Failure to acquire means another check is handling it.
4. Reload config and state under the lock. Recheck enabled state, activation time, lateness, and occurrence identity.
5. If already attempted or terminally skipped, exit.
6. If too late, persist missed once and exit. Record the latest missed occurrence after a long absence; do not replay history.
7. Validate execution prerequisites within the remaining admission interval.
8. Persist and sync the attempted marker before launch. If this fails, launch nothing.
9. Start the exact provider invocation within the due interval, with its own two-minute timeout.
10. Classify and persist the result, then release the lock.

Run due providers independently so one provider's timeout cannot make the other miss its trigger. A failed Codex request must not prevent a due Claude request. Use separate locks and state files.

Use kernel-released locks, not stale PID-file guesses. A crash after persisting attempted leaves an interrupted/unknown result that is never automatically retried. Exactly-once delivery cannot be guaranteed; prefer a missed request over a duplicate after an uncertain crash.

Enforce timeout on the entire child process tree: Windows Job Object and a Unix process group or an equivalent tested mechanism. Kill and reap descendants. Pause prevents future admissions; if a request is already underway, state that it may complete.

An active provider allowance is not automatically movable. If supported non-generative status exposes a current active window, skip it and explain. Otherwise send the one scheduled request and record only request success, with reset explicitly unverified. Do not add private quota API polling in v1.

## 10. Installation, updates, and removal

Publish archives for Windows, macOS, and Linux, on amd64 and arm64, subject to real validation and current provider support. Each archive contains the executable and license. Native installation requires no Go, Node, or Python; the npm route requires Node 22 or later during installation. Provider CLIs remain prerequisites. Stable builds include only independently validated adapters via the ReleaseProviders build setting; this permits a Codex-first release. Preview builds can exercise both adapters.

Installers must:

- Detect OS/architecture, reject unsupported combinations plainly, and resolve one stable release tag.
- Download the matching archive and SHA-256 manifest from that same release.
- Verify before execution; use unique temporary directories and extract only expected files.
- Use user-owned paths without sudo or administrator elevation.
- Install into the stable root and make agentwarmup available through a minimal user PATH entry or owned link.
- Preserve unrelated PATH entries and shell-profile content. Any profile block must be marked and removable.
- Launch setup using the new executable's absolute path in the current terminal.
- Read prompts from the terminal correctly even when the installer itself was piped into a shell.
- Show one useful error on download, checksum, unsupported platform, or registration failure.
- Support an explicit destination for isolated installer tests and advanced users; default setup shows no path question.

Keep the repository slug OliverGrabner/agentwarmup, asset naming, supported targets, and minimum provider versions in .github/release.json. Derive the version from the release tag, then emit a resolved release.json asset. Archive names follow agentwarmup_VERSION_OS_ARCH.zip on Windows and .tar.gz elsewhere; publish checksums.txt and both installers alongside them. Release-attached installers must resolve to those same versioned assets, even if a new release appears mid-install. Publish real install commands only after release artifacts exist and those commands have been tested.

On update, preserve config/state, temporarily quiesce the scheduled check, stage the new executable, verify its version, replace it, and restore the previous job state. A failed update restores the prior executable/job. Do not make an update rerun scheduling questions. Refuse automatic downgrade to a build that cannot read the saved schema.

The implementation uses a durable lifecycle journal for interrupted transactions and an operation lock outside the removable application directory. Uninstall records a unique token; Windows deferred cleanup checks that token and the same operation lock before removing files. A failed rollback keeps request admission blocked.

Windows cannot reliably replace or delete a running image. Use a narrowly scoped hidden helper for deferred replacement/removal, with explicit owned paths and bounded waiting. Test from both the installed executable and a downloaded copy.

Setup edits are transactional: stage the proposed config, keep provider admission disabled while verifying the replacement job, and commit the enabled config with effectiveFrom only after that succeeds. Preserve the old working config/job for rollback. Coordinate admission and lifecycle writes so a check cannot read a half-applied change. On failure, restore the old state and report the rollback. A first-run cancellation leaves no enabled job.

Uninstall confirms once, disables/removes the job, handles in-flight work, removes owned PATH changes, and deletes only the validated application root. Do not remove provider CLIs or credentials. Report partial cleanup honestly if any step fails.

Test first-run trust behavior on Windows and macOS. Prefer signing/notarization when available; otherwise accurately document observed OS prompts near manual downloads. Never disable OS protections. Checksums verify download integrity; they do not replace signing.

## 11. Tests and acceptance

| Layer | Required evidence |
| --- | --- |
| Schedule | Midnight rollover, Sunday/Monday, both DST transitions, gaps/folds, non-hour offsets, saved-zone travel, 0/119/120/121-second lateness |
| CLI | One/both providers, missing/login/API modes, Enter-through edit, invalid input, EOF, cancellation, redirected stdin, non-color terminals |
| Runner | Parallel checks produce one attempt per provider; crash before/after launch; unwritable/corrupt state; independent provider failures; no retries |
| Provider | Exact argv/env/cwd, closed stdin, status parsing, OK, auth failure, rate limit, unavailable model, structured error, large output, timeout with child |
| Lifecycle | Register/query, closed-terminal execution, pause/resume, edit rollback, upgrade preservation, interrupted upgrade, complete uninstall |
| Install | Clean machine without Go, paths with spaces/Unicode, existing PATH entry, bad checksum, wrong architecture, failed download, piped setup |
| Real usage | Two idle windows and one active window for every advertised provider/model/plan combination |
| Usability | Five unfamiliar testers; at least four finish unaided within 60 seconds when a supported CLI/login already exists |

Use one small Go fake provider for process tests on every OS. It can report status, record non-secret invocation data, sleep, spawn a child, and return selected errors. It must never contact providers.

Run deterministic tests in CI on Windows, macOS, and Linux. Check gofmt, go vet, and go test; run race checks where supported. Cross-compilation proves a build, not scheduler or authentication compatibility. Track physical/VM lifecycle results separately. CI must never use subscription credentials or send live warmups.

## 12. Implementation sequence

| Step | Deliverable | Exit condition |
| --- | --- | --- |
| Release gate | Provider validation and compatibility record | Idle/active windows and isolated subscription requests pass, or the provider is excluded; implementation may proceed with fakes |
| 1 | Pure schedule engine and schema | DST, rollover, migration, and activation tests pass |
| 2 | Provider adapters and fake process | Authentication/isolation/result fixtures pass on supported OSes |
| 3 | Guarded runner and state | Concurrency, crash, timeout, and late-run tests pass |
| 4 | OS jobs and lifecycle | Isolated scheduler tests pass on each OS; live scheduled provider validation completes |
| 5 | Terminal setup and status | Normal setup takes only selection/time/days/confirmation; failure paths are clear |
| 6 | Binary installation and update | Clean-machine install and removal work; no language toolchain needed |
| 7 | README and release candidate | Public instructions match actual behavior and all links/assets resolve |
| 8 | Unfamiliar-user trial and publication | Usability target met; release checklist complete |

Bring existing parser tests and scrubbing cases forward where useful. Replace the prototype's clock arithmetic, auth-file check, after-run-only dedupe, and catch-up flow. Move internal/codex into the provider package only when the new adapter is ready. Move cli/warmup execution into runner, keeping command compatibility during migration.

No rewrite for its own sake. Keep working OS serialization/helpers when they satisfy the new contracts.

## 13. Scope after release

Native cloud scheduling is the first expansion to investigate for closed-laptop users. It must prove allowance activation, supported creation/edit/removal, understandable prerequisites, and reasonable timing. A documented handoff to provider UI is preferable to writing undocumented internal automation records.

Skills/plugins can later call the same executable. A GUI, wake support, remote runners, extra schedule profiles, and usage optimization wait for demonstrated demand. None is needed to complete this release.
