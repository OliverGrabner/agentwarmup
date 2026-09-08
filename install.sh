#!/bin/sh
# Release packaging replaces this token with the exact release tag.
set -eu
release_tag='@RELEASE_TAG@'
destination=''
fail() { printf 'AgentWarmup: %s\n' "$*" >&2; exit 1; }
while [ "$#" -gt 0 ]; do
    case "$1" in
        --version) [ "$#" -ge 2 ] || fail '--version needs a release tag'; release_tag=$2; shift 2 ;;
        --destination) [ "$#" -ge 2 ] || fail '--destination needs an absolute directory'; destination=$2; shift 2 ;;
        *) fail "unknown installer argument: $1" ;;
    esac
done
command -v curl >/dev/null 2>&1 || fail 'curl is required to download the release.'
command -v tar >/dev/null 2>&1 || fail 'tar is required to unpack the release.'
case "$(uname -s)" in
    Darwin) platform=darwin; default_root="$HOME/Library/Application Support/AgentWarmup" ;;
    Linux) platform=linux; default_root="${XDG_CONFIG_HOME:-$HOME/.config}/agentwarmup" ;;
    *) fail 'this installer supports macOS and Linux.' ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) fail 'only amd64 and arm64 release builds are available.' ;;
esac
[ -n "$destination" ] || destination=$default_root
case "$destination" in /*) ;; *) fail 'destination must be an absolute directory.' ;; esac
download() { curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --retry 2 "$1" --output "$2" || fail 'download failed; check your connection and the release tag.'; }
if [ "${release_tag#@}" != "$release_tag" ]; then
    resolved=$(curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --output /dev/null --write-out '%{url_effective}' 'https://github.com/OliverGrabner/agentwarmup/releases/latest') || fail 'no stable release could be resolved; use --version for a published prerelease.'
    release_tag=${resolved##*/}
fi
case "$release_tag" in v[0-9]*) ;; *) fail 'invalid release tag; expected vMAJOR.MINOR.PATCH.' ;; esac
case "$release_tag" in *[!a-zA-Z0-9.+-]*) fail 'invalid release tag.' ;; esac
version=${release_tag#v}
archive="agentwarmup_${version}_${platform}_${arch}.tar.gz"
base="https://github.com/OliverGrabner/agentwarmup/releases/download/$release_tag"
umask 077
stage=$(mktemp -d "${TMPDIR:-/tmp}/agentwarmup-install.XXXXXXXX") || fail 'could not create a temporary directory.'
trap 'rm -rf -- "$stage"' EXIT HUP INT TERM
download "$base/$archive" "$stage/$archive"
download "$base/checksums.txt" "$stage/checksums.txt"
expected=$(awk -v file="$archive" '$2 == file {print $1}' "$stage/checksums.txt")
[ "${#expected}" -eq 64 ] || fail 'checksum manifest has no unique archive entry.'
case "$expected" in *[!0-9a-fA-F]*) fail 'invalid checksum entry.' ;; esac
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$stage/$archive"); actual=${actual%% *}
elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$stage/$archive"); actual=${actual%% *}
else
    fail 'sha256sum or shasum is required to verify the download.'
fi
[ "$actual" = "$expected" ] || fail 'checksum mismatch; the downloaded archive was not executed.'
members=$(tar -tzf "$stage/$archive") || fail 'could not read the archive.'
[ "$members" = "agentwarmup
LICENSE" ] || [ "$members" = "LICENSE
agentwarmup" ] || fail 'archive contains unexpected files.'
tar -tvzf "$stage/$archive" | awk 'substr($0,1,1) != "-" {bad=1} END {exit bad}' || fail 'archive contains non-regular files.'
tar -xzf "$stage/$archive" -C "$stage" agentwarmup LICENSE || fail 'could not unpack the executable.'
chmod 700 "$stage/agentwarmup"
reported=$("$stage/agentwarmup" --version) || fail 'the executable cannot run on this computer.'
[ "$reported" = "agentwarmup $version" ] || fail 'downloaded executable reports an unexpected version.'
printf 'Installing AgentWarmup %s\n' "$version"
# The shell program may arrive on stdin. Setup always reads the actual terminal.
[ -r /dev/tty ] && [ -w /dev/tty ] || fail 'run this installer from an interactive terminal.'
"$stage/agentwarmup" install --home "$destination" </dev/tty >/dev/tty 2>/dev/tty || fail 'installation did not complete; see the explanation above.'
printf 'Open a new terminal to use agentwarmup.\n'
