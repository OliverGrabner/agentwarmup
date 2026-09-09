# Release checklist

Target: v0.1.0. Nothing here is complete merely because it appears in the plan.

[Product scope](product.md) · [Coding plan](implementation.md) · [Provider evidence](validation.md)

## Behavior

- [ ] Every advertised provider passes the live validation record, including actual OS-scheduled execution.
- [ ] README, help, and setup say Target reset for predictions and Request succeeded for process outcomes.
- [ ] Active windows, allowance consumption, weekly limits, and machine availability are explained briefly.
- [ ] Setup sends no model request. Late requests are skipped after two minutes.
- [ ] Provider failure, pause/resume, duplicates, crashes, and timeout behavior meet the coding plan.
- [ ] Claude's effective subscription billing and isolation are rechecked against current documentation.

## Installation and lifecycle

Record results for every published OS/architecture/provider combination. Do not mark a native lifecycle tested based on cross-compilation or another architecture.

| Platform | Install | Terminal closed | Edit | Pause/resume | Update | Uninstall | Evidence |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Windows amd64 | Pending | Fake scheduled runner passed | Unit tests | Native fake passed | Unit tests | Job removal and isolated self-removal passed | [Offline evidence](testing.md) |
| Windows arm64 | Pending | Pending | Pending | Pending | Pending | Pending | |
| macOS arm64 | Pending | Pending | Pending | Pending | Pending | Pending | |
| macOS amd64 | Pending | Pending | Pending | Pending | Pending | Pending | |
| Linux amd64 | Pending | Pending | Pending | Pending | Pending | Pending | |
| Linux arm64 | Pending | Pending | Pending | Pending | Pending | Pending | |

- [ ] A clean user without Go can run the exact README install command and complete setup.
- [ ] The npm route works on Node 22 and 24 on every advertised OS, including paths with spaces and Unicode. Users without Node can complete the native route.
- [ ] Arrow keys, Enter, custom days, Escape, Ctrl-C, EOF, plain terminals, and NO_COLOR work. Cancellation restores the terminal and leaves the schedule unchanged.
- [ ] The job points to the permanent executable. Removing the launcher's temporary files and npm cache does not break scheduled execution.
- [ ] Rerunning the npm launcher updates without resetting days, time, pause state, or attempt records. The installed command works without npm.
- [ ] Piped installer input still allows interactive setup; Enter/EOF/cancel work correctly.
- [ ] Wrong platform, failed download, invalid checksum, and interrupted update leave no broken enabled job.
- [ ] Spaces and Unicode paths work; scheduler launches on Windows create no visible console.
- [ ] No root/admin requirement; supported Linux user timers work after terminal closure.
- [ ] PATH changes are minimal, upgrades preserve schedule/state, and uninstall removes owned files.
- [ ] First-run OS trust prompts are tested and accurately documented; protections are not disabled.
- [ ] At least four of five unfamiliar testers finish unaided within 60 seconds with a CLI/login already present.

## Repository and artifacts

- [x] Repository confirmed: [OliverGrabner/agentwarmup](https://github.com/OliverGrabner/agentwarmup); display name: AgentWarmup.
- [x] Module path and imports use github.com/OliverGrabner/agentwarmup.
- [x] Source excludes build binaries and contains no credentials, machine-local state, or fake badges.
- [ ] README contains only benefit/example, real installation, normal use, essential limits, and useful links.
- [ ] The current development notice is replaced only when the release behavior is real.
- [x] gofmt, go vet, unit/process tests, and Windows race checks pass locally.
- [x] CI runs on Windows, macOS, and Linux without provider credentials or live requests. [Passing run](https://github.com/OliverGrabner/agentwarmup/actions/runs/34379607953).
- [x] Local candidate packaging includes version information, archives, LICENSE, and one SHA-256 manifest. GitHub workflow execution remains pending.
- [ ] Installer and artifacts refer to the same release tag; avoid independently resolving latest twice.
- [x] The local npm tarball embeds the matching executable hashes and version, includes LICENSE, and excludes tests, credentials, and install hooks.
- [ ] Publish candidate assets before testing the matching npm package under an explicit preview tag. Verify package ownership and the intended npm tag; never let a candidate become latest.
- [ ] The exact released npm command is tested from a clean cache before adding it to the README. Keep the native installation route nearby.
- [ ] Minimum CLI versions and validated models are recorded.
- [ ] Release notes describe behavior, supported platforms, important limits, and update/removal instructions.
- [ ] MIT attribution remains intact. Downloaded binaries stay out of source control.

Release workflow: run CI, build artifacts, generate checksums, and prepare a draft release. Publish it as a prerelease candidate so clean-machine testers can use unauthenticated, release-specific download URLs. Verify installation, then promote those same verified assets to the stable release and check the final README commands. Give workflow tokens only the permissions each job needs. Use pinned action revisions and keep release credentials out of ordinary test jobs.

## Launch

- [ ] Set a concise repository description: Schedule early Codex and Claude requests for a better-timed usage reset. Mention only providers actually supported.
- [ ] Add relevant topics such as codex, claude-code, cli, and automation.
- [ ] Check the README on GitHub at desktop and mobile widths; check all links.
- [ ] Publish a brief Show HN post linking directly to the repository and using the real timing example.
- [ ] Use one actual setup capture only if it improves comprehension; no separate site or launch video.
- [ ] Share in relevant communities within their posting rules and answer installation issues promptly.

No star requirement blocks release. Track public stars and volunteered user feedback after launch; do not add product telemetry, fake activity, or promotional clutter to chase them.
