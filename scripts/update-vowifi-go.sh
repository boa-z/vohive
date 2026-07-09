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
VOWIFI_MODULE="${VOWIFI_MODULE:-github.com/boa-z/vowifi-go}"
VOWIFI_VERSION="${VOWIFI_VERSION:-main}"

printf 'Using Go: %s\n' "$("$GO_BIN" version)"
printf 'Using GOWORK: %s\n' "$GOWORK"
printf 'Updating %s@%s\n' "$VOWIFI_MODULE" "$VOWIFI_VERSION"

run "$GO_BIN" get "${VOWIFI_MODULE}@${VOWIFI_VERSION}"
run "$GO_BIN" mod tidy

resolved="$("$GO_BIN" list -m -f '{{.Path}} {{.Version}}{{with .Replace}} replace={{.Path}}{{end}}' "$VOWIFI_MODULE")"
printf '\n==> resolved %s\n' "$resolved"
if [[ "$resolved" == *" replace="* ]]; then
	printf 'unexpected local replace for %s after update: %s\n' "$VOWIFI_MODULE" "$resolved" >&2
	exit 1
fi
