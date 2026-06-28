#!/bin/bash
# Update the gomatic/build checkout to the canonical gomatic/build main, then let
# the Makefile rebuild the pinned tools. Usage: self-update.sh <BUILD_HOME>
#
# Works in every layout — direct clone, fork (origin=fork, upstream=gomatic/build),
# worktree — because it never trusts a remote name or branch tracking. It finds
# the remote whose URL is the canonical gomatic/build repo and fast-forwards main
# to it. Forks are never pulled from. fetch+merge are split (not `git pull`) so
# no upstream-tracking config is required on the branch.
#
# Currency is guaranteed or this fails: with no canonical remote, or a main
# that cannot fast-forward, it errors out rather than leave the developer stale.
set -o errexit
set -o nounset
set -o pipefail

readonly home="${1:?usage: self-update.sh <BUILD_HOME>}"
# shellcheck source=scripts/lib-build.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib-build.sh"

# Baked Docker image: copied files, no .git — nothing to update, succeed quietly.
build_is_git_checkout "${home}" || {
  echo "${home} is not a git checkout (baked image?) — nothing to update"
  exit 0
}

remote="$(build_canonical_remote "${home}")" || {
  echo >&2 "ERROR: no remote points at the canonical gomatic/build repo — add one"
  echo >&2 "       (git -C ${home} remote add upstream git@github.com:gomatic/build.git)"
  echo >&2 "       so updates come from gomatic/build, not a fork"
  exit 1
}

echo "Updating gomatic/build from '${remote}' (canonical gomatic/build)…"
git -C "${home}" fetch "${remote}" main
git -C "${home}" merge --ff-only FETCH_HEAD || {
  echo >&2 "ERROR: local gomatic/build main cannot fast-forward to ${remote}/main"
  echo >&2 "       (diverged, dirty, or detached HEAD) — reconcile ${home} manually"
  echo >&2 "       so you are current"
  exit 1
}
