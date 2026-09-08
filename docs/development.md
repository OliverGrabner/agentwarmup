# Development

The public repository is [OliverGrabner/agentwarmup](https://github.com/OliverGrabner/agentwarmup). Source publication and release downloads are pending. These instructions assume you already have the local source folder.

## Local setup

Install [Go 1.25 or later](https://go.dev/dl/). Open a terminal in the AgentWarmup folder containing `go.mod` and `README.md`.

Windows, in PowerShell:

```powershell
go build -o agentwarmup.exe .
.\agentwarmup.exe setup
```

macOS or Linux:

```sh
go build -o agentwarmup .
./agentwarmup setup
```

Setup uses your installed provider CLI and subscription login. It sends no model request, but confirming enables future scheduled requests. Use an awake computer with a supported scheduler. See [provider validation](validation.md) for the current limits of this preview.

Arrow keys and Enter select options. Space toggles custom days. Escape or Ctrl-C cancels; plain terminals use numbered/text answers. Existing settings remain the defaults when editing.

## Tests

Ordinary tests use fakes and temporary directories. They do not register your schedule or send provider requests.

```sh
go test ./...
go vet ./...
npm --prefix npm test
node --test .github/scripts/package-npm.test.mjs
```

The npm launcher tests require [Node 22 or later](https://nodejs.org/en/download). There are no npm dependencies to install. The Go executable runs independently of Node. Additional checks and native test precautions are in [testing.md](testing.md).

## Package a preview

From PowerShell, with Go, Node, and npm available:

```powershell
./.github/scripts/release.ps1 -Tag v0.1.0-rc.1 -Output build/release-preview
```

Choose a fresh output directory each time. Packaging cross-compiles the executable, creates native archives and gzip downloads, embeds the version and executable hashes in an npm tarball, and writes checksums. It does not publish anything.

The source npm package stays private and has no download targets. Packaging generates the publishable metadata in a temporary copy. Inspect the generated package without installing a schedule:

```sh
npx --yes --package ./build/release-preview/agentwarmup-0.1.0-rc.1.tgz agentwarmup --help
```

Help runs locally. Actual installation through a packaged launcher needs its matching GitHub release downloads to exist. Do not present an npm registry command as working before the package and assets are published.

Follow [pre-publish.md](pre-publish.md) for candidate testing and release acceptance. Stable packaging remains blocked until provider and platform validation pass.
