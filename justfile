# ---- .tools family standard: identical in every family repo ----------------
# `just verify` is exactly what CI runs: this tool's `prepare` (files the build
# needs, e.g. an embedded asset), the family gate (tools-actions go-ci, at the
# version .github/workflows/ci.yml pins), then this tool's `verify-extra`.
# The pre-push hook (lefthook.yml) runs it too, so local and CI never differ.
set shell := ["bash", "-euo", "pipefail", "-c"]

default: verify

verify: prepare gate verify-extra

# Everything: verify plus this tool's slow checks (browser, containers), which
# CI runs as their own required jobs.
verify-all: verify verify-slow

# The family Go gate: gofmt, vet, golangci-lint (family config), race tests,
# cross-build. Fetched once per tools-actions version into ~/.cache.
gate:
    #!/usr/bin/env bash
    set -euo pipefail
    v="$(grep -oE 'go-ci@v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/ci.yml | head -1 | cut -d@ -f2)"
    f="${XDG_CACHE_HOME:-$HOME/.cache}/tools-actions/$v/go-ci/local.sh"
    [ -f "$f" ] || { mkdir -p "$(dirname "$f")"; curl -fsSL "https://raw.githubusercontent.com/subaud-tools/tools-actions/$v/go-ci/local.sh" -o "$f"; }
    bash "$f"

fmt:
    gofmt -w $(git ls-files '*.go')

# Install the lefthook hooks into this clone's own .git/hooks (once per
# clone). A global core.hooksPath (casebook's recorder) forwards to them;
# plain `lefthook install` refuses to run under one.
hooks:
    #!/usr/bin/env bash
    set -euo pipefail
    d="$(cd "$(git rev-parse --git-common-dir)" && pwd)/hooks"
    git config --local core.hooksPath "$d"
    trap 'git config --local --unset core.hooksPath' EXIT
    lefthook install --force >/dev/null
    echo "lefthook hooks installed in $d"

# ---- muda -------------------------------------------------------------------
# Files the gate needs that are not committed (built before the gate, locally
# and in CI). muda has none: the standard, recipes, skills and page bundle it
# embeds are committed.
prepare:

# Slow checks (a browser, a container) that CI runs as their own jobs: the
# page's browser probe.
verify-slow: probe

# Tool-specific checks beyond the gate (CI runs this too): actionlint on the
# workflows, the page's TypeScript gate, the committed bundle matching its
# source, and no cloud words in muda's page and channel code.
verify-extra: lint-workflows web bundle-fresh no-cloud

lint-workflows:
    actionlint

# Rewrite internal/web/wire.d.ts from the Go structs; go test fails while it is
# stale.
wire:
    go test ./internal/serve -run WireType -update

# The page (internal/web) is TypeScript built by esbuild into
# internal/serve/assets, which is COMMITTED and embedded, so node is dev-time
# only. Its node_modules can hold Go source; the family gate skips anything
# under node_modules.
web_dir := "internal/web"

# Install the page's dependencies once (CI caches node_modules keyed on the
# lockfile, so a hit skips `npm ci`); the postinstall copies the page kit's types
# out of the tools-common module go.mod pins (kit.d.ts is generated).
_web-deps:
    cd {{ web_dir }} && { [ -d node_modules ] || npm ci; } && npm run --silent kit-types

# The page's TypeScript gate: unit tests, tsc against wire.d.ts, eslint,
# prettier. Pass-throughs: the commands live in internal/web/package.json.
web: _web-deps
    cd {{ web_dir }} && npm test
    cd {{ web_dir }} && npm run --silent typecheck
    cd {{ web_dir }} && npm run --silent lint
    cd {{ web_dir }} && npm run --silent fmt:check

# Rebuild the committed bundle.
assets: _web-deps
    cd {{ web_dir }} && npm run --silent build

# The committed bundle matches its source, and the page's copies of tools-common's
# build files match the version go.mod pins: nothing else notices either.
bundle-fresh: assets
    git diff --exit-code -- internal/serve/assets/muda.js internal/serve/assets/muda.css \
      || { echo "the committed muda bundle does not match internal/web: run 'just assets' and commit the result"; exit 1; }
    cd {{ web_dir }} && npm run --silent page-build
    git diff --exit-code -- internal/web/build/eslint.base.mjs internal/web/build/probe.mjs \
      || { echo "internal/web/build differs from the tools-common version go.mod pins: edit it in tools-common, or run 'cd internal/web && npm run page-build' and commit the result"; exit 1; }

# The spec's line: muda's own page and channel code carries no cloud code or
# words. The copied kit build files and the lockfile are not muda's own.
no-cloud:
    #!/usr/bin/env bash
    set -euo pipefail
    if git grep -ilE 'aws|cloud' -- internal/web internal/channel internal/serve internal/store internal/cli/serve.go ':!internal/web/build' ':!internal/web/package-lock.json'; then
      echo "muda's page and channel code must carry no cloud words (these files do)"
      exit 1
    fi

# The page in a real headless chromium against a seeded serve (--no-open, its own
# state; internal/web/serve-fixture.mjs builds the binary). KIT_BROWSER=required:
# a probe that finds no chromium fails rather than skipping. Install one with
# `cd internal/web && node node_modules/playwright-core/cli.js install chromium chromium-headless-shell`.
probe: _web-deps
    cd {{ web_dir }} && KIT_BROWSER=required npm run --silent probe

# A local binary stamped the way a release is.
build:
    go build -ldflags "-X github.com/subaud-tools/muda/internal/version.version=$(cat VERSION) -X github.com/subaud-tools/muda/internal/version.commit=$(git rev-parse --short HEAD) -X github.com/subaud-tools/muda/internal/version.date=$(date -u +%Y-%m-%d)" -o muda ./cmd/muda
