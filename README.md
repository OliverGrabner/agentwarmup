<h1 align="center">AgentWarmup</h1>

<p align="center">Start your coding-agent usage window before your workday.</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-39785A?style=flat-square" alt="MIT license"></a>
  <a href="https://github.com/OliverGrabner/agentwarmup/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/OliverGrabner/agentwarmup/ci.yml?branch=main&amp;style=flat-square&amp;label=CI" alt="CI status"></a>
  <a href="docs/product.md"><img src="https://img.shields.io/badge/telemetry-none-526475?style=flat-square" alt="No telemetry"></a>
  <a href="docs/validation.md"><img src="https://img.shields.io/badge/status-preview-B47A27?style=flat-square" alt="Development preview"></a>
</p>

```text
5 AM               9 AM              10 AM
Warmup ----------> Start work ------> Target reset
       4 hours               1 hour
```

Example schedule. Reset timing is not yet verified.

Choose a target reset time. AgentWarmup schedules a minimal request five hours earlier through your computer's scheduler and your existing subscription login. No hosted service, extra account, or API key.

## Setup

**Development preview:** [reset timing is unverified](docs/validation.md). The npm package and release downloads are not published yet. To try the local source, follow the [build and setup instructions](docs/development.md#local-setup).

You need a subscription login in **Codex CLI 0.153.0+** or **Claude Code 2.1.169+**.

The release will offer an npm launcher for **Node 22+** users and native installers for everyone else. Both install the same executable, with no Go or clone needed.

Setup detects your installed agents and timezone. Use **arrow keys and Enter** to choose agents, a target reset time, and days; the defaults are **10 AM, weekdays**. Review the warmup time and confirm. Custom times and days are available.

Setup sends **no test request**. After saving, open a new terminal to use `agentwarmup` from any folder. The schedule runs locally after the terminal closes.

## Everyday use

```sh
agentwarmup             # status
agentwarmup setup       # change providers, time, or days
agentwarmup pause       # pause future requests
agentwarmup resume      # resume the schedule
agentwarmup uninstall   # remove AgentWarmup
```

## Keep in mind

- Your computer must be **awake, online, and logged in** at the scheduled time. Requests more than two minutes late are skipped.
- An active window cannot be forced to move. Warmups consume allowance and do not increase weekly limits.
- Windows uses Task Scheduler, macOS uses LaunchAgents, and Linux requires a systemd user session. Native platform acceptance is [still in progress](docs/pre-publish.md).

For closed-laptop scheduling with Claude, see [Claude routines](https://code.claude.com/docs/en/web-scheduled-tasks).

[Development](docs/development.md) · [Testing and recovery](docs/testing.md) · [MIT license](LICENSE)
