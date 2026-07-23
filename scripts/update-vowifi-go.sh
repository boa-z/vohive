#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export GOWORK="${GOWORK:-off}"

find_go() {
	if [[ -n "${GO_BIN:-}" ]]; then
		printf '%s\n' "$GO_BIN"
		return
	fi
	if command -v go >/dev/null 2>&1; then
		command -v go
		return
	fi
	if [[ -x /usr/local/go/bin/go ]]; then
		printf '%s\n' /usr/local/go/bin/go
		return
	fi
	printf 'go not found; set GO_BIN=/path/to/go\n' >&2
	return 127
}

run() {
	printf '\n==> %s\n' "$*"
	"$@"
}

GO_BIN="$(find_go)"
VOWIFI_MODULE="${VOWIFI_MODULE:-github.com/zanescope/vowifi-go}"
VOWIFI_VERSION="${VOWIFI_VERSION:-main}"

printf 'Using Go: %s\n' "$("$GO_BIN" version)"
printf 'Using GOWORK: %s\n' "$GOWORK"
printf 'Updating %s@%s\n' "$VOWIFI_MODULE" "$VOWIFI_VERSION"

resolved_version="$("$GO_BIN" list -mod=mod -m -f '{{.Version}}' "${VOWIFI_MODULE}@${VOWIFI_VERSION}")"
if [[ -z "$resolved_version" ]]; then
	printf 'failed to resolve %s@%s to a module version\n' "$VOWIFI_MODULE" "$VOWIFI_VERSION" >&2
	exit 1
fi
printf 'Resolved version: %s\n' "$resolved_version"

run "$GO_BIN" mod edit "-require=${VOWIFI_MODULE}@${resolved_version}"
run "$GO_BIN" mod edit "-dropreplace=${VOWIFI_MODULE}"
run "$GO_BIN" mod tidy

resolved="$("$GO_BIN" list -m -f '{{.Path}} {{.Version}}{{with .Replace}} replace={{.Path}} {{.Version}}{{end}}' "$VOWIFI_MODULE")"
expected="${VOWIFI_MODULE} ${resolved_version}"
printf '\n==> resolved %s\n' "$resolved"
if [[ "$resolved" != "$expected" ]]; then
	printf 'unexpected direct resolution for %s: got %s, want %s\n' "$VOWIFI_MODULE" "$resolved" "$expected" >&2
	exit 1
fi
