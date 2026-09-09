# Provider validation

Status: two deliberate Codex invocations recorded; neither provider has passed validation. Codex returned startup warnings that still need investigation. Fake-process checks do not establish the reset promise.

Complete this record before advertising a provider or publishing a stable release. Implementation and fake-provider testing may proceed while these experiments are pending, as approved by the project owner on 2026-09-08. See [implementation.md](implementation.md) for the execution contract.

## Compatibility record

| Provider | CLI version | OS | Plan | Exact model | Relevant allowance | Result |
| --- | --- | --- | --- | --- | --- | --- |
| Codex | 0.153.0 candidate | Windows amd64 | ChatGPT Plus | gpt-6-astra candidate | Active 300-minute window observed | Warning events; request and reset unverified |
| Claude | Isolated 2.1.169 candidate; installed 2.1.92 rejected | Windows amd64 | Pro subscription auth confirmed with isolated arguments | claude-sonnet-4-6 candidate | Not verified | No model request |

These are compatibility candidates, not passing combinations. Local authentication probes returned only non-secret status fields; they do not establish isolated request access. Record exact OS/CLI/plan/model combinations when experiments run. A build artifact does not establish compatibility.

## Observations on 2026-09-09

Both Codex invocations used the [candidate arguments](provider-notes.md) from `provider.Request`, an empty owned working directory, closed stdin, the existing file-based subscription login, and a 120-second process timeout. Each deliberate attempt was recorded before launch; neither was retried automatically.

| Invocation | Start UTC | End UTC | Observation |
| --- | --- | --- | --- |
| Production runner | 16:56:49.561 | 16:56:53.769 | Reported an isolation failure; raw output was discarded |
| Separate diagnostic | 17:00:20.609 | 17:00:30.390 | Exit 0, final text `OK`, two startup error items, no tool item types |

The diagnostic exposed a classification bug: Codex uses `item.type=error` for nonfatal warnings as well as model rerouting. These now produce a fixed warning/error failure instead of an invented tool-call failure. Unknown events still fail, and explicit tool activity remains rejected. The two warning messages were not retained, so they are not assumed harmless. The invocation remains unvalidated. [Codex 0.153.0 event handling](https://github.com/openai/codex/blob/rust-v0.153.0/codex-rs/exec/src/event_processor_with_jsonl_output.rs#L401-L498).

Non-generative quota reads immediately before and after both invocations reported the same active 300-minute reset: **2026-09-09 21:56:25 UTC**, a difference of zero seconds. An earlier read at 16:54:15 UTC reported 21:53:09 UTC. Other account activity was ongoing, so that earlier change cannot be attributed to these experiments. **Active has not passed**; Idle A, Idle B, and Scheduled remain pending.

The quota helper verified effective integration restrictions and subscription status before reading usage data. This verifies status access, not model-request isolation.

For Claude, an isolated 2.1.169 binary passed signed-manifest, SHA-256, and Authenticode verification. Its frozen safe-mode arguments with `auth status --json` confirmed `claude.ai`, `firstParty`, and `pro`. Updates were disabled; the existing installations were not changed. No Claude model request was sent.

Sanitized local evidence is under `build/validation/` and is ignored by Git. It contains invocation metadata, timestamps, numeric quota observations, and diagnostic event types, without credentials, account identities, or conversation output.

## Experiment per provider

Use a deliberate test window on an eligible subscription. Keep unrelated requests and automations from contaminating the measurement. Do not put live experiments in setup, CI, or a general unit-test command.

| Case | Before | Request evidence | After | Pass condition |
| --- | --- | --- | --- | --- |
| Idle A | Relevant window expired or inactive | Actual request start/end in UTC; exact invocation | Provider-reported reset and allowance | Reset approximately five elapsed hours after the request |
| Idle B | A separate inactive window | Same frozen invocation | Provider-reported reset and allowance | Repeats Idle A |
| Active | Active window with recorded reset | Same invocation | Reset before/after | Existing reset unchanged |
| Scheduled | Previously validated inactive window | OS-triggered invocation after terminal closes | Request result and provider reset | Same behavior through the real scheduler |

Define approximately before testing: initial acceptance is a reset within two minutes of five hours after request initiation, accounting for provider display rounding and measured request duration. Record raw timestamps and precision; do not mark a result passing when the available display is too coarse to establish it.

Each experiment record must include:

- Date, OS, CLI version, plan, model, relevant allowance name, and its pre-request state.
- Exact executable/argument array and non-secret isolation settings.
- Request start/end times, provider-observed reset instant with timezone, and the difference.
- Response result, evidence location, other account activity, and conclusion.
- Whether general and model-specific allowances behave differently.

No credentials, account emails, raw auth-status dumps, or provider conversation transcripts belong in this file. Redact account identifiers in any screenshots.

## Freeze the invocation

Record the final argument array here for each passing adapter. Both are pending. Current candidate arguments and source findings are in [provider-notes.md](provider-notes.md).

Validate the subscription login in the same configuration/environment as execution. Confirm no inherited instructions, hooks, tools, MCP integrations, memory, or session persistence. Run in an empty application-owned directory.

Capture sanitized fixtures for: subscription login, signed out, API billing, unsupported CLI, OK, rate limit, model unavailable, structured provider failure, and timeout. Simulated fixtures test the code; distinguish them from provider-observed responses.

Do not use Claude --bare as an isolation shortcut: it does not read subscription OAuth or the system keychain. [Programmatic usage](https://code.claude.com/docs/en/headless)

## Documentation checked on 2026-09-08

- Codex supports scripted execution and ephemeral sessions. This does not promise a particular reset anchor. [Non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode)
- Codex exposes an authentication-status command and supports multiple credential stores. [Authentication](https://learn.chatgpt.com/docs/auth)
- Claude documents noninteractive execution and JSON authentication status. [CLI reference](https://code.claude.com/docs/en/cli-reference)
- Anthropic's June 15 update says the proposed separate Agent SDK credit was paused and claude -p still uses subscription limits. Some indexed documentation retains the earlier announcement. Recheck this before release. Shared billing still does not prove reset activation. [Updated announcement](https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan)

## Decision

A provider passes only when its final isolated request satisfies both idle experiments, the active-window check, and the scheduled check. Mark unsupported combinations explicitly. An adapter failure excludes that adapter; failure of both stops this release.

Revalidate after changes to the model, invocation, authentication/isolation strategy, or provider metering. Never convert an unknown result into request succeeded or reset verified.

## Historical prototype checks

The previous pre-publish notes reported the following on Windows 11 / Go 1.25.5, using a stand-in Codex:

- gofmt, go vet, and unit tests passed; darwin/arm64 and linux/amd64 cross-compiles succeeded.
- Midnight rollover, LastTrigger, sequential duplicate suppression, and the old catch-up guards were exercised.
- Simulated auth failures were scrubbed in last-run state and logs.
- A real Task Scheduler job fired, recorded OK from the stand-in, and was removed.
- The old StartWhenAvailable setting and one absolute .cmd launcher were checked.

These are preserved historical reports, not fresh validation of the new implementation. They do not establish provider authentication, actual resets, or macOS/Linux lifecycle behavior. Current scheduler tests use isolated roots and job names. Current offline results are in [testing.md](testing.md).
