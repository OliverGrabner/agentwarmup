# Provider implementation evidence

Checked 2026-09-08. The adapters are implemented candidates; neither provider has passed the live reset or isolation release checks in [validation.md](validation.md). This investigation used help, feature listings, schemas, official sources, and fake processes. It sent no model requests and changed no login or provider installation. Separate account-status observations are recorded by the main implementation investigation.

## Codex

Local Windows x64 help reports `codex-cli 0.153.0`. The adapter requires at least that version and rechecks help after CLI changes. Every feature name below was recognized by that binary's non-generative `features list`, using a separate empty home.

Implemented argv, shown with the default credential-store selection:

```json
[
  "exec", "--ignore-user-config", "--ephemeral", "--json",
  "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never",
  "--model", "gpt-6-astra",
  "-c", "model_provider=\"openai\"",
  "-c", "cli_auth_credentials_store=\"file\"",
  "-c", "model_reasoning_effort=\"low\"",
  "-c", "project_doc_max_bytes=0",
  "-c", "skills.include_instructions=false",
  "-c", "web_search=\"disabled\"",
  "-c", "approval_policy=\"never\"",
  "-c", "history.persistence=\"none\"",
  "-c", "analytics.enabled=false",
  "--disable", "hooks", "--disable", "plugins", "--disable", "apps",
  "--disable", "remote_plugin", "--disable", "memories",
  "--disable", "multi_agent", "--disable", "shell_tool",
  "--disable", "shell_snapshot", "--disable", "unified_exec",
  "--disable", "code_mode", "--disable", "code_mode_host",
  "--disable", "skill_mcp_dependency_install", "--disable", "skill_search",
  "--disable", "image_generation", "--disable", "goals",
  "--enable", "skip_host_skill_discovery",
  "Reply only with OK. Do not use tools."
]
```

`--ignore-user-config` retains authentication in `CODEX_HOME`. Detection reads only the non-secret credential-store selector from `config.toml`, preserving `file`, `keyring`, or `auto` explicitly in both auth and request arguments. No credential file is read or copied. The status argv is `['-c', 'model_provider="openai"', '-c', 'cli_auth_credentials_store="file"', 'login', 'status']`, with the stored selector substituted. It has no `--ignore-user-config` flag. [Non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode), [authentication](https://learn.chatgpt.com/docs/auth)

Tagged source accepts zero instruction bytes and reads `skills.include_instructions`; these control project instruction content and the skills catalog. `skip_host_skill_discovery` is conditional on registered contributors, so it does not independently prove that every skill file is unread. The runtime disables known execution/integration features and rejects observed tool events, but those result checks cannot undo an already executed call. Managed configuration and integration startup still need deliberate isolation validation; this is not a claim that all built-in tools or all managed integrations are absent. [Configuration schema](https://github.com/openai/codex/blob/rust-v0.153.0/codex-rs/core/config.schema.json), [config resolution](https://github.com/openai/codex/blob/rust-v0.153.0/codex-rs/core/src/config/mod.rs), [conditional discovery](https://github.com/openai/codex/blob/rust-v0.153.0/codex-rs/core/src/session/session.rs)

The bundled model catalog lacks Astra; bundled defaults do not establish account availability. No model substitution is implemented. Successful JSONL requires an `OK` assistant item and a completed turn without observed tool activity or structured errors.

## Claude

The native CLI at `%USERPROFILE%/.local/bin/claude.exe` reports `2.1.92`. It is rejected because safe mode first appeared in `2.1.169`; the adapter never upgrades it. Version and required help flags are checked before use. The fixed `claude-sonnet-4-6` model is a candidate whose affected allowance remains unverified. [Official changelog](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md#21169), [CLI reference](https://code.claude.com/docs/en/cli-reference)

Implemented argv for a compatible CLI:

```json
[
  "--safe-mode", "--setting-sources", "",
  "--settings", "{\"disableAllHooks\":true,\"autoMemoryEnabled\":false}",
  "--strict-mcp-config", "--mcp-config", "{\"mcpServers\":{}}",
  "--tools", "", "--disallowedTools", "mcp__*", "--no-chrome",
  "--no-session-persistence", "--output-format", "json",
  "--model", "claude-sonnet-4-6", "--effort", "low", "--max-turns", "1",
  "-p", "Reply only with OK. Do not use tools."
]
```

Auth uses the same isolation arguments through `--no-chrome`, followed by `['auth', 'status', '--json']`. Only recognized subscription status is accepted. JSON success must report `OK`, no permission denials or additional agentic turns, and no reported fallback model. Global flags with the auth subcommand still need a real compatible-CLI acceptance check.

Safe mode preserves authentication but managed hooks still apply. Session settings cannot disable managed hooks; incompatible managed environments must not be advertised. `--bare` ignores subscription OAuth and the keychain and is never used. [Safe mode](https://code.claude.com/docs/en/cli-reference#cli-flags), [hooks](https://code.claude.com/docs/en/hooks), [programmatic usage](https://code.claude.com/docs/en/headless)

## Environment, probes, and remaining evidence

Requests run in an empty owned directory with closed stdin. Authentication probes use fresh temporary directories, a 15-second deadline, bounded output, and hidden Windows processes. A child environment allowlist preserves home/config locations, executable lookup, supported proxy/CA settings, and OS keychain plumbing; it removes API keys, injected tokens, cloud-provider selectors, provider URL/model overrides, telemetry exporters, and interpreter injection variables. Login alone retains the user's original environment and terminal streams. Tests use only the Go helper process.

`Verify` refreshes the version/help evidence while retaining the saved executable, launcher prefix, config root, and auth store. Auth status does not prove a token is unrevoked or a model is usable. The code does not poll private quota APIs or infer reset success from process success.

Codex app-server documents non-generative `account/read` with `refreshToken:false`, `model/list`, and `account/rateLimits/read`. The rate-limit schema includes separate buckets and nullable duration/reset fields, with reset represented as Unix seconds. These could supply deliberate reset evidence after app-server startup isolation is established; no such read is implemented here. Starting no thread does not itself prevent app-scoped integrations from initializing. No comparable Claude non-generative model/allowance endpoint was verified. [App-server protocol](https://learn.chatgpt.com/docs/app-server)

Release evidence still needs isolated subscription execution, exact model access, no actual tools/hooks/inherited instructions/history, two idle windows, an active-window comparison, and real scheduled runs. Current output is always **Request succeeded; reset unverified**.
