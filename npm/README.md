# AgentWarmup npm launcher

Requires Node.js 22 or later and an installed, signed-in provider CLI supported
by the release. The repository's source package is private; release packages
include version-pinned executable metadata.

Run `agentwarmup` to install or update the native executable. First installation
opens setup; updates preserve settings. The launcher downloads and verifies the
matching executable when run. Installing the npm package sends no model request
and runs no installation scripts. Setup also sends no model request.

Use `agentwarmup status`, `setup`, `pause`, `resume`, or `uninstall` to manage
your schedule. `--help` and `--version` work offline. Scheduled checks use the
installed executable independently of Node.js and the npm cache. Uninstall
removes the native installation; remove the npm package separately if desired.

Keep your computer awake, online, and logged in at the scheduled time. Warmups
consume allowance and cannot guarantee a reset or move an existing window.

See the [repository](https://github.com/OliverGrabner/agentwarmup) for release
status, provider requirements, and installation details.
