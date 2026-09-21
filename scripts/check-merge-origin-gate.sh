#!/usr/bin/env bash
#
# check-merge-origin-gate.sh is the compatibility gate for the parameter
# override-order / origin-tracing refactor. It only passes when all four
# coupled pieces pass together:
#
#   1. locked override order (env/file order validation on the merge chain)
#   2. origin tracing (string, bool, integer layers)
#   3. boundary table (empty text vs unset, inverse bool, subcommand
#      shadowing, required-missing errors)
#   4. compatibility (full existing test suite, migration notes present,
#      no unrelated changes to shell completion)
#
# The script is deterministic: it prints the failing layer and exits
# non-zero on the first failed check, so re-running on another machine
# yields the same verdict.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 2

BASELINE="cf8e1ad"

fail() {
	echo "GATE FAIL: $*" >&2
	exit 1
}

step() {
	echo "==> $*"
}

# --- block 0: toolchain ------------------------------------------------------
step "go build"
go build ./... || fail "build failed"

step "go vet"
go vet ./... || fail "go vet failed"

# --- blocks 1-3: ordering + tracing + boundary table -------------------------
step "order lock, origin tracing and boundary table tests"
go test -count=1 -run \
	'TestLayerOfBuiltinSources|TestValidateSourceOrder|TestTraceOrigin|TestTraceUnknownFlagErrors|TestSubcommandShadowsSameNamedFlag|TestPersistentFlagTracedFromSubcommand|TestInvalidSourceOrderStopsInstall|TestRequiredMissingStillReportsRequiredError|TestRequiredSatisfiedBySource|TestZeroValueSourceChain|TestEnvVarValueSource|TestEnvVars|TestFileValueSource|TestFilePaths|TestValueSourceChain|TestMapValueSource' \
	./... || fail "order/tracing/boundary checks failed"

# --- block 4a: no regressions in the previously-valid merge combinations ------
step "full existing test suite (backward compatibility)"
go test -count=1 ./... || fail "existing tests regressed"

# --- block 4b: migration notes must exist and mention the locked order --------
step "migration notes present"
DOC="docs/v3/value-source-migration.md"
[ -f "$DOC" ] || fail "migration notes $DOC are missing"
grep -q "command line > environment > file" "$DOC" \
	|| fail "migration notes do not document the locked override order"

# --- block 4c: completion machinery must be left untouched --------------------
step "shell completion files unchanged"
if git rev-parse --verify "${BASELINE}^{commit}" >/dev/null 2>&1; then
	COMPLETION_CHANGES="$(git diff --name-only "$BASELINE" -- \
		completion.go completion_test.go completion_partial_regression_test.go \
		autocomplete fish.go fish.go 2>/dev/null)"
	if [ -n "$COMPLETION_CHANGES" ]; then
		echo "$COMPLETION_CHANGES" >&2
		fail "unrelated shell completion files were modified"
	fi
else
	echo "    baseline $BASELINE not present, skipping completion-diff check" >&2
fi

echo "GATE OK: order lock, origin tracing, boundary table and compatibility all pass"
