#!/usr/bin/env bash
# Scaffolds a real Laravel app under test-apps/real/<name> for docker/deploy
# e2e testing.
#
# Usage: scaffold.sh <name> [--horizon] [--ssr] [--wayfinder] [--npm]
#
# Requires php + composer + network. JS toolchain defaults to bun (bun.lock);
# pass --npm to use npm instead — note that a package-lock.json is only
# produced on a machine that has npm.
set -euo pipefail

name=""
horizon=0
ssr=0
wayfinder=0
npm=0

positional=()
for a in "$@"; do
  case "$a" in
  --horizon | --ssr | --wayfinder | --npm) ;;
  -*) echo "unknown flag $a" >&2; exit 2 ;;
  *) positional+=("$a") ;;
  esac
done
if [ "${#positional[@]}" -ne 1 ]; then
  echo "usage: scaffold.sh <name> [--horizon] [--ssr] [--wayfinder] [--npm]" >&2
  exit 2
fi
name="${positional[0]}"
for a in "$@"; do
  case "$a" in
  --horizon) horizon=1 ;;
  --ssr) ssr=1 ;;
  --wayfinder) wayfinder=1 ;;
  --npm) npm=1 ;;
  esac
done

root="$(cd "$(dirname "$0")" && pwd)"
dir="$root/$name"
if [ -e "$dir" ]; then
  echo "error: $dir already exists" >&2
  exit 1
fi

echo "==> composer create-project laravel/laravel $name"
composer create-project laravel/laravel "$dir" --no-interaction --prefer-dist
cd "$dir"

pm() {
  if [ "$npm" = 1 ]; then npm "$@"; else bun "$@"; fi
}

if [ "$horizon" = 1 ]; then
  echo "==> laravel/horizon"
  composer require laravel/horizon --no-interaction
fi

if [ "$ssr" = 1 ]; then
  echo "==> inertia + ssr"
  composer require inertiajs/inertia-laravel --no-interaction
  pm add @inertiajs/vue3 vue
  php artisan inertia:publish --middleware || true
  cat > config/inertia.php <<'PHP'
<?php

return [

    'ssr' => [

        'enabled' => true,

        'url' => env('INERTIA_SSR_URL', 'http://127.0.0.1:13714'),

    ],

];
PHP
  php -r '
    $f = "package.json";
    $j = json_decode(file_get_contents($f), true);
    $j["scripts"]["build:ssr"] = "vite build --ssr";
    file_put_contents($f, json_encode($j, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . PHP_EOL);
  '
fi

if [ "$wayfinder" = 1 ]; then
  echo "==> wayfinder"
  composer require laravel/wayfinder --no-interaction || echo "laravel/wayfinder unavailable — add it manually" >&2
  pm add -d @laravel/vite-plugin-wayfinder
fi

# Lockfile must exist before generate runs: bun install with no lockfile yet
# is fine, npm ci would fail without one.
echo "==> installing js deps"
pm install

git init -q . 2>/dev/null || true
git add -A
git commit -qm "scaffold $name" || true

echo
echo "done: $dir"
echo "next:"
echo "  berth-cli generate --path $dir --workers 2 --scheduler --port 8080"
echo "  docker build --target runtime -f $dir/docker/app/Dockerfile $dir"
