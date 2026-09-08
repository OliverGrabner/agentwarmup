# AgentWarmup

AgentWarmup is a small local usage-window scheduler. The repository is [OliverGrabner/agentwarmup](https://github.com/OliverGrabner/agentwarmup) and the command is agentwarmup.

Read [docs/product.md](docs/product.md) for product behavior and [docs/implementation.md](docs/implementation.md) for the coding plan. [docs/validation.md](docs/validation.md) holds provider evidence; [docs/pre-publish.md](docs/pre-publish.md) controls release readiness.

## Working rules

- Build one prebuilt Go executable with a consistent Codex/Claude setup flow. Each provider must independently pass validation before being advertised.
- Keep npm as a small version-pinned launcher. Scheduling uses the permanently installed Go executable, independently of npm's cache.
- Use the user's OS scheduler and existing CLI subscription login. No hosted backend, API billing fallback, copied credentials, or telemetry.
- Setup must not send a model request. Live experiments belong to deliberate provider validation, not ordinary tests.
- Keep reset-day rollover, five elapsed hours across DST, a two-minute late limit, and lock/record-before-launch duplicate prevention.
- Keep the interface small: status/first-run setup, setup, pause, resume, uninstall. No catch-up or run-now feature.
- Prefer explicit small packages and tested provider arguments over a generic framework.
- Tests use fake providers and isolated job names/roots. Never overwrite a user's existing schedule during testing.
- Describe request success separately from a verified reset. Keep current behavior distinct from planned support.
- Keep user-facing writing concise. Document implementation detail under docs/ and avoid duplicate plans at the repository root.
- Use short, plain commit subjects with conventional prefixes such as `feat:`, `fix:`, `chore:`, or `docs:`. Add a body only when it helps explain the change.
- Do not publish placeholder install commands, unverified compatibility, or fabricated validation results.
- Add source structure as it is implemented; do not create empty stubs to resemble a finished repository.
- User instructions in the current session take precedence when scope changes.

The implementation is a development preview. Live provider and release acceptance checks remain pending.
