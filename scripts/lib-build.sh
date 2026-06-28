#!/bin/bash
# Shared helpers for gomatic/build self-maintenance scripts. Sourced, not executed.
#
# Every function operates on the gomatic/build checkout passed as its first
# argument (the consumer's Makefile passes $(BUILD_HOME)) so these never touch
# the consumer's own repo.

# Echo the name of the remote whose URL is the canonical gomatic/build repo,
# whatever it is called locally (upstream, origin, ...) and regardless of branch
# tracking. We match on the remote URL, not its name, so the wrong remote is
# never trusted. Forks are excluded by construction: a user fork is
# <user>/build, so only the canonical .../gomatic/build(.git) matches the
# "/gomatic/build" glob. Prints nothing (and the caller decides if that is
# fatal) when none is found.
build_canonical_remote() {
  local build_home="${1}" remote
  for remote in $(git -C "${build_home}" remote); do
    case "$(git -C "${build_home}" remote get-url "${remote}")" in
      *[:/]gomatic/build | *[:/]gomatic/build.git)
        echo "${remote}"
        return 0
        ;;
    esac
  done
  return 1
}

# True when the path is a real git checkout (vs. a baked Docker image, which has
# copied files and no .git).
build_is_git_checkout() {
  git -C "${1}" rev-parse --git-dir >/dev/null 2>&1
}
