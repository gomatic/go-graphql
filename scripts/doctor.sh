#!/bin/bash
# Diagnose gomatic/build freshness + Go version drift. Read-only and best-effort:
# warnings only, never fails, so it is safe to run anywhere. Catches the
# "green in CI, stale locally" class. Usage: doctor.sh <BUILD_HOME>
set -o errexit
set -o nounset
set -o pipefail

readonly home="${1:?usage: doctor.sh <BUILD_HOME>}"
# shellcheck source=scripts/lib-build.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib-build.sh"

report_freshness() {
  build_is_git_checkout "${home}" || {
    echo "gomatic/build   : baked image (no .git) — nothing to update"
    return
  }

  local remote behind
  remote="$(build_canonical_remote "${home}")" || {
    echo >&2 "WARNING: no remote points at canonical gomatic/build — can't check freshness"
    echo >&2 "         (add a gomatic/build remote)"
    return
  }

  git -C "${home}" fetch --quiet "${remote}" main 2>/dev/null || true
  behind="$(git -C "${home}" rev-list --count HEAD..FETCH_HEAD 2>/dev/null || echo 0)"
  if [[ "${behind}" == "0" ]]; then
    echo "gomatic/build   : up to date (vs ${remote}/main)"
  else
    echo >&2 "WARNING: gomatic/build is ${behind} commit(s) behind ${remote}/main — run 'make build-self-update'"
  fi
}

report_go_drift() {
  local build_go consumer_go
  build_go="$(cd "${home}" && go mod edit -json | jq -r .Go)"
  consumer_go="$(go mod edit -json | jq -r .Go)"
  echo "toolchain Go    : ${build_go}"
  echo "consumer Go     : ${consumer_go}"
  [[ "${build_go}" == "${consumer_go}" ]] || {
    echo >&2 "WARNING: consumer Go (${consumer_go}) != gomatic/build Go (${build_go})"
  }
}

echo "gomatic/build home : ${home}"
report_freshness
report_go_drift
