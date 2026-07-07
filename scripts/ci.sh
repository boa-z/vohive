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

workflow_lint() {
	local actionlint_bin tmpbin

	if [[ -n "${ACTIONLINT_BIN:-}" ]]; then
		actionlint_bin="$ACTIONLINT_BIN"
	elif command -v actionlint >/dev/null 2>&1; then
		actionlint_bin="$(command -v actionlint)"
	else
		tmpbin="$(mktemp -d)"
		ACTIONLINT_VERSION="${ACTIONLINT_VERSION:-v1.7.12}"
		run env GOBIN="$tmpbin" "$GO_BIN" install "github.com/rhysd/actionlint/cmd/actionlint@${ACTIONLINT_VERSION}"
		actionlint_bin="$tmpbin/actionlint"
	fi

	run "$actionlint_bin" .github/workflows/*.yml
}

dependency_hygiene() {
	local forbidden_refs forbidden_modules

	if [[ -f go.work ]]; then
		printf 'go.work must not be committed or required for CI builds\n' >&2
		return 1
	fi

	forbidden_refs="$(
		{
			git grep -nE 'github[.]com/iniwex5|github[.]com/boa-z/qqbot|iniwex[/]vohive|DOCKERHUB[_]|secrets[.]DOCKERHUB|vohive[-]release|GO[.]?PRIVATE|GO[.]?NOSUMDB|GH[_]PAT' -- \
				go.mod go.sum .github Dockerfile Dockerfile.github Dockerfile.runtime docker-compose.yml docker-compose.hub.yml DOCKERHUB.md Makefile scripts internal cmd pkg web/src \
				':!internal/web/dist/**' ':!web/dist/**' || true
			git grep -nE 'replace[[:space:]].*=>[[:space:]]*(\.{1,2}/|/|~)' -- \
				go.mod go.sum .github Dockerfile Dockerfile.github Dockerfile.runtime docker-compose.yml docker-compose.hub.yml DOCKERHUB.md Makefile scripts internal cmd pkg web/src \
				':!internal/web/dist/**' ':!web/dist/**' || true
		} | sed '/^$/d'
	)"
	if [[ -n "$forbidden_refs" ]]; then
		printf 'forbidden dependency or local-path references found:\n%s\n' "$forbidden_refs" >&2
		return 1
	fi

	forbidden_modules="$(env GOWORK=off "$GO_BIN" list -m all | grep -E 'github[.]com/iniwex5|github[.]com/boa-z/qqbot' || true)"
	if [[ -n "$forbidden_modules" ]]; then
		printf 'forbidden modules resolved by go list -m all:\n%s\n' "$forbidden_modules" >&2
		return 1
	fi

	printf '\n==> dependency hygiene ok\n'
}

release_hygiene() {
	local workflow
	workflow=".github/workflows/binary-release.yml"
	if [[ ! -f "$workflow" ]]; then
		printf 'release workflow not found: %s\n' "$workflow" >&2
		return 1
	fi
	if git grep -nE 'repository:[[:space:]]*[^[:space:]]+|GH[_]PAT|vohive[-]release|github[.]com/iniwex5' -- "$workflow"; then
		printf 'release workflow must publish only to the current repository without cross-repo PAT wiring\n' >&2
		return 1
	fi
	if ! git grep -n 'softprops/action-gh-release' -- "$workflow" >/dev/null; then
		printf 'release workflow does not publish through softprops/action-gh-release\n' >&2
		return 1
	fi
	if ! git grep -n 'files: dist/*' -- "$workflow" >/dev/null; then
		printf 'release workflow must upload dist artifacts to the current release\n' >&2
		return 1
	fi
	printf '\n==> release hygiene ok\n'
}

web_build() {
	run npm ci --prefix web
	run npm run build --prefix web
	rm -rf internal/web/dist
	mkdir -p internal/web
	cp -R web/dist internal/web/dist
}

tidy_check() {
	run "$GO_BIN" mod tidy -diff
}

go_tests() {
	read -r -a packages <<< "${CI_GO_TEST_PACKAGES:-./internal/device ./internal/mbim ./internal/qmi ./internal/backend ./internal/esim ./internal/cscall ./internal/proxy/traffic ./internal/notify ./internal/qqbot/...}"
	if [[ ${#packages[@]} -eq 0 ]]; then
		printf '\n==> no Go test packages configured\n'
		return
	fi
	run "$GO_BIN" test "${packages[@]}"
}

go_build() {
	(
		export CGO_ENABLED="${CGO_ENABLED:-0}"
		export GOOS="${GOOS:-linux}"
		run "$GO_BIN" build -trimpath -buildvcs=false -tags "${GO_TAGS:-with_utls nomsgpack}" -o "${CI_BUILD_OUTPUT:-/tmp/vohive}" ./cmd/vohive
	)
}

usage() {
	cat <<'USAGE'
Usage: scripts/ci.sh [all|workflow-lint|hygiene|release-hygiene|web|tidy|test|build ...]

Default all runs workflow-lint, hygiene, release-hygiene, web, tidy, test, and build.

Environment:
  GO_BIN               path to go binary
  GOWORK               Go workspace mode, default: off
  ACTIONLINT_BIN       path to an existing actionlint binary
  ACTIONLINT_VERSION   actionlint version to install when needed
  CI_GO_TEST_PACKAGES  package list for Go tests
  CI_BUILD_OUTPUT      output path for the CI build binary
  GO_TAGS              build tags, default: with_utls nomsgpack
USAGE
}

GO_BIN="$(find_go)"

if [[ $# -eq 0 || "${1:-}" == "all" ]]; then
	tasks=(workflow-lint hygiene release-hygiene web tidy test build)
else
	tasks=("$@")
fi

printf 'Using Go: %s\n' "$("$GO_BIN" version)"
printf 'Using GOWORK: %s\n' "$GOWORK"

for task in "${tasks[@]}"; do
	case "$task" in
		workflow-lint | actionlint) workflow_lint ;;
		hygiene | dependency-hygiene) dependency_hygiene ;;
		release-hygiene | release) release_hygiene ;;
		web | frontend) web_build ;;
		tidy | tidy-check) tidy_check ;;
		test | go-test) go_tests ;;
		build | go-build) go_build ;;
		-h | --help | help)
			usage
			exit 0
			;;
		*)
			printf 'unknown CI task: %s\n' "$task" >&2
			usage >&2
			exit 2
			;;
	esac
done
