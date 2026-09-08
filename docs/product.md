# Product brief

AgentWarmup schedules an early request so an eligible coding-agent usage window can reset at a more useful time.

The first release targets Codex and Claude Code through their installed CLIs. It runs on the user's computer, uses their existing subscription login, and requires no AgentWarmup account or backend. Provider support is conditional on the experiments in [validation.md](validation.md).

## The useful outcome

For a validated first-request window:

    5:00 AM   Warmup request
    9:00 AM   Start work
    10:00 AM  Target reset

The value is an earlier replenishment during the workday. A reset before work starts may offer little benefit. Warmups consume allowance, do not increase weekly limits, and cannot force an active window to move.

Do not promise unlimited usage, guaranteed resets, or support for ordinary API billing.

## Who it serves

Start with people who already use Codex or Claude Code on a subscription, regularly exhaust a short usage window, and have a computer awake at the trigger time.

Support both providers through the same setup rather than asking users to learn two scheduler products. The initial configuration shares one reset time and reset-day selection. Each provider runs and reports results independently.

Closed-laptop users remain outside the local release's guarantee. Mention the awake, online, and logged-in requirement before installation and during confirmation. Native provider cloud scheduling is a later path to investigate; we do not host a service or collect credentials.

## Setup

The npm launcher selects the right prebuilt executable and opens setup. Native installers provide the same flow without Node. A person with an installed, signed-in provider should finish in under a minute.

Example of the intended released flow, assuming both adapters have passed validation:

    AgentWarmup

    Codex   Ready
    Claude  Ready

    Agents       [Both]
    Target reset [10:00 AM]
    Days         [Weekdays]
    Timezone     America/Chicago

    Warmup       Wednesday, 5:00 AM
    Target reset Wednesday, 10:00 AM

    Keep this computer awake, online, and logged in at 5:00 AM.

    Enable? [Y/n]

    Schedule enabled.
    Change it with agentwarmup setup.

Skip agent selection if only one supported provider is present. Show authentication problems only when they matter to the selected provider. Offer its normal login and return to setup.

Use arrow keys and Enter for providers, reset-time presets, and reset days. Default to Both when available, 10 AM, and weekdays. Offer a custom time and a day picker without adding questions to the common path. Preserve existing settings on edit and provide plain prompts when cursor control is unavailable.

No test request during setup. No catch-up question, model picker, cloud account, API key, or cron syntax.

## Daily use

Opening agentwarmup shows schedule health, the next trigger, target reset, and each provider's last result.

    AgentWarmup   Enabled

    Next warmup   Thursday, 5:00 AM
    Target reset  Thursday, 10:00 AM
    Codex         Request succeeded, Wednesday 5:00 AM
    Claude        Sign-in needed: claude auth login

    Change: agentwarmup setup

Predicted resets are labeled Target reset. A missed trigger says Missed scheduled time; it does not assert that the computer was asleep when that cause is unknown.

Keep setup, pause, resume, and uninstall as the management commands. Fix failures through short messages with a useful next step.

## Presentation

The repository should feel maintained because the product works and the writing is specific.

- Use AgentWarmup consistently; repository and command are agentwarmup.
- Start the README with the benefit, a concrete example, and the machine requirement.
- Lead with the same npm command on each supported OS, with native installation underneath for people without Node.
- Link manual downloads as the fallback. Do not make Go installation the user path.
- Keep the released README around 250-350 words, excluding the setup transcript and commands.
- Keep badges factual and linked to evidence. Never imply that configured CI has passed or that preview support is validated.
- Use the concise ASCII timing example requested by the owner. No decorative hero art, stock logo wall, or fabricated result.
- Keep engineering decisions and test matrices under docs/.
- Keep comments about behavior and tradeoffs. Remove filler that narrates obvious code.
- Avoid star counters, star-history charts, generic feature grids, long FAQs, and requests to star the repository.
- Do not fabricate authorship, endorsements, adoption, or validation. The repository needs no statement about how code was produced.

## README at release

Use this order:

1. Name and one sentence explaining the benefit.
2. The 5 AM request / 9 AM work / 10 AM target example, supported-provider scope, and the awake-computer requirement.
3. Installation commands and a manual release download link.
4. A short setup transcript and the five management commands.
5. Three compact limitations: existing windows cannot be moved; requests consume allowance without adding weekly capacity; late runs are skipped.
6. License and links to development/validation details.

Until the release exists, show an honest development status rather than placeholder installation URLs or a claim that Claude already works.

## Launch

Lead with the user problem and a direct repository link. If both providers qualify:

    Show HN: AgentWarmup - Schedule early Codex and Claude requests

Explain the earlier-reset example and laptop requirement in the launch text. Use Astra as context only when supported by the experiments. Publish a useful artifact before announcing it. No separate website, backend, or launch video.

Similar projects, including [agent-warmup](https://github.com/diyanbogdanov/agent-warmup), already exist. Differentiate through reliable local execution, simple binary installation, and a consistent two-provider experience. Do not claim the idea is new.

## Success criteria

Stars are a distribution signal, not a result the implementation can guarantee. Improve the likelihood of recommendations by measuring:

- At least four of five unfamiliar testers complete setup unaided in under a minute, with a supported CLI and login already present.
- Every advertised OS/provider combination passes installation and lifecycle checks.
- Eligible pilot runs produce provider-observed reset timing within the documented tolerance.
- Pilot users can explain whether this reduced waiting during their normal workday.
- Misses and failures are understandable without reading source code.

Collect pilot feedback directly. No product telemetry is required.
