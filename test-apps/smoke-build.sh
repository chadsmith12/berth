#!/usr/bin/env bash
# Docker-builds every fixture variant through the real generate command.
# Templates are only valid if their images build — run this before pushing
# template changes. Requires docker; uses the default builder (the bug
# classes this guards against fail identically under BuildKit).
set -uo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$root/.." && pwd)"

declare -A ARGS=(
  [php-only]="--workers 2 --scheduler=false --port 8080"
  [npm-vite]="--workers 2 --scheduler=false --port 8080"
  [bun-ssr-wayfinder]="--workers 2 --scheduler=false --port 8080"
  [horizon]="--scheduler=false --port 8080"
  [horizon-ssr]="--scheduler=false --port 8080"
)

if ! command -v docker >/dev/null; then
  echo "docker not available — skipping smoke builds"
  exit 0
fi

failed=()
for variant in php-only npm-vite bun-ssr-wayfinder horizon horizon-ssr; do
  work="$(mktemp -d)"
  cp -r "$root/$variant/." "$work/"
  rm -rf "$work/node_modules"

  echo "==> $variant: generate"
  if ! (cd "$repo" && go run ./cmd/berth-cli generate --path "$work" ${ARGS[$variant]}) >/dev/null 2>&1; then
    echo "✗ $variant: generate failed"
    (cd "$repo" && go run ./cmd/berth-cli generate --path "$work" ${ARGS[$variant]}) 2>&1 | tail -5
    failed+=("$variant")
    rm -rf "$work"
    continue
  fi

  echo "==> $variant: docker build"
  if (cd "$work" && docker build --target runtime -f docker/app/Dockerfile . >/dev/null 2>&1); then
    echo "✓ $variant"
  else
    echo "✗ $variant: build failed"
    (cd "$work" && docker build --target runtime -f docker/app/Dockerfile . 2>&1) | tail -15
    failed+=("$variant")
  fi
  rm -rf "$work"
done

echo
if [ ${#failed[@]} -gt 0 ]; then
  echo "failed: ${failed[*]}"
  exit 1
fi
echo "all variants build"
