#!/usr/bin/env bash
set -euo pipefail

# Single source of truth for "what build is this?" (same contract as lectarr's).
#
# The APK's versionName/versionCode derive from this script, so an installed build can
# always be traced back to one commit. Resolution order:
#
#   1. TALOSDEV_MOBILE_VERSION in the environment (CI, releases).
#   2. `git describe` against the vMAJOR.MINOR.PATCH release tags.
#   3. The commit SHA alone, when the checkout has no tags yet.
#   4. A literal "unknown", when there is no git or no commit at all.
#
# Usage:
#   scripts/version.sh              # print the full version string
#   scripts/version.sh --env        # KEY=value lines, safe for `eval`/`export`
#   scripts/version.sh --json       # JSON object
#   scripts/version.sh --field NAME # print one field (NAME without the prefix)

UNKNOWN_VERSION="0.0.0-unknown"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

has_commit() {
    command -v git >/dev/null 2>&1 &&
        git -C "$repo_root" rev-parse --verify -q HEAD >/dev/null 2>&1
}

resolve() {
    local describe="" sha="" branch="" dirty="false" build_number="0" version="" base=""

    if has_commit; then
        describe="$(git -C "$repo_root" describe --tags --match 'v[0-9]*' --always --dirty --broken 2>/dev/null || true)"
        sha="$(git -C "$repo_root" rev-parse HEAD)"
        branch="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
        [[ "$branch" == "HEAD" ]] && branch=""
        [[ -n "$(git -C "$repo_root" status --porcelain 2>/dev/null)" ]] && dirty="true"
        # Monotonic on a linear history: what Android's versionCode needs.
        build_number="$(git -C "$repo_root" rev-list --count HEAD)"
    fi

    if [[ "$describe" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)(-dirty|-broken)?$ ]]; then
        base="${BASH_REMATCH[1]}"
        version="$base"
    elif [[ "$describe" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)-g([0-9a-f]+)(-dirty|-broken)?$ ]]; then
        base="${BASH_REMATCH[1]}"
        version="${base}+${BASH_REMATCH[2]}.g${BASH_REMATCH[3]}"
    elif [[ -n "$sha" ]]; then
        base="0.0.0"
        version="0.0.0+g${sha:0:12}"
    else
        base="0.0.0"
        version="$UNKNOWN_VERSION"
    fi

    if [[ "$dirty" == "true" && "$version" != "$UNKNOWN_VERSION" ]]; then
        # `+meta` may only appear once, so extend the existing metadata segment.
        if [[ "$version" == *+* ]]; then version="${version}.dirty"; else version="${version}+dirty"; fi
    fi

    if [[ -n "${TALOSDEV_MOBILE_VERSION:-}" ]]; then
        version="$TALOSDEV_MOBILE_VERSION"
        [[ "$version" =~ ^v?([0-9]+\.[0-9]+\.[0-9]+) ]] && base="${BASH_REMATCH[1]}"
    fi

    VERSION="$version"
    VERSION_BASE="$base"
    # versionCode must be >= 1, even before the first commit.
    BUILD_NUMBER="${TALOSDEV_MOBILE_BUILD_NUMBER:-$(( build_number > 0 ? build_number : 1 ))}"
    GIT_SHA="$sha"
    GIT_DESCRIBE="$describe"
    GIT_BRANCH="$branch"
    GIT_DIRTY="$dirty"
}

emit_env() {
    printf 'TALOSDEV_MOBILE_VERSION=%q\n' "$VERSION"
    printf 'TALOSDEV_MOBILE_VERSION_BASE=%q\n' "$VERSION_BASE"
    printf 'TALOSDEV_MOBILE_BUILD_NUMBER=%q\n' "$BUILD_NUMBER"
    printf 'TALOSDEV_MOBILE_GIT_SHA=%q\n' "$GIT_SHA"
    printf 'TALOSDEV_MOBILE_GIT_DESCRIBE=%q\n' "$GIT_DESCRIBE"
    printf 'TALOSDEV_MOBILE_GIT_BRANCH=%q\n' "$GIT_BRANCH"
    printf 'TALOSDEV_MOBILE_GIT_DIRTY=%q\n' "$GIT_DIRTY"
}

emit_json() {
    python3 - "$VERSION" "$VERSION_BASE" "$BUILD_NUMBER" "$GIT_SHA" "$GIT_DESCRIBE" "$GIT_BRANCH" "$GIT_DIRTY" <<'PYTHON'
import json, sys
keys = ["version", "version_base", "build_number", "git_sha", "git_describe", "git_branch", "git_dirty"]
values = dict(zip(keys, sys.argv[1:]))
values["build_number"] = int(values["build_number"])
values["git_dirty"] = values["git_dirty"] == "true"
print(json.dumps(values))
PYTHON
}

resolve
case "${1:-}" in
    "") printf '%s\n' "$VERSION" ;;
    --env) emit_env ;;
    --json) emit_json ;;
    --field)
        name="${2:?--field needs a NAME}"
        # Portable to macOS's bash 3.2 (no ${x^^} or [[ -v ]]).
        var="$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')"
        [[ -n "${!var+set}" ]] || { echo "unknown field: $name" >&2; exit 2; }
        printf '%s\n' "${!var}"
        ;;
    *) echo "usage: $0 [--env|--json|--field NAME]" >&2; exit 2 ;;
esac
