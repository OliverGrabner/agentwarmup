# Native installation

The native executable needs no Node or Go. This is a development preview; [provider validation](validation.md) and [platform acceptance](pre-publish.md) are unfinished.

Download the `.zip` or `.tar.gz` for your operating system and architecture from [v0.1.0-rc.2](https://github.com/OliverGrabner/agentwarmup/releases/tag/v0.1.0-rc.2), then extract it. `amd64` means x64; `arm64` includes Apple Silicon. The release includes `checksums.txt` for download verification.

Open a terminal in the extracted folder and run:

Windows PowerShell:

```powershell
.\agentwarmup.exe setup
```

macOS or Linux:

```sh
./agentwarmup setup
```

Setup finds your installed provider CLI and subscription login, then asks for a target reset time and days. Confirming installs a permanent copy and enables the schedule. Setup sends no model request.

Open a new terminal afterward to use `agentwarmup`. You can remove the extracted download; scheduled checks use the installed copy. Run `agentwarmup uninstall` to remove the app and schedule.

Prefer the [npm setup command](../README.md#setup) if you already have Node 22 or later.
