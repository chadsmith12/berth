#!/bin/sh
# =============================================================================
# Berth generated Laravel entrypoint
# Runs in EVERY container (app, worker, scheduler), so everything here
# must be safe to run concurrently.
# =============================================================================

set -e

role="${CONTAINER_ROLE:-app}"
echo "[entrypoint] role=${role}"

if [ -n "${DB_HOST}" ]; then
  echo "[entrypoint] waiting for ${DB_HOST}:${DB_PORT:-5432}"
  i=0
  until php -r "exit(@fsockopen(getenv('DB_HOST'), (int)(getenv('DB_PORT') ?: 5432), \$e, \$s, 2) ? 0 : 1);" 2>/dev/null; do
    i=$((i+1))
    if [ "$i" -ge 60 ]; then
      echo "[entrypoint] database unreachable after 60s, aborting"
      exit 1
    fi
    sleep 1
  done
  echo "[entrypoint] database reachable"
fi

if [ "${role}" != "ssr" ]; then
  php artisan config:cache
  php artisan route:cache
  php artisan view:cache
fi

if [ "${RUN_MIGRATIONS:-false}" = "true" ]; then
  echo "[entrypoint] running migrations"
  php artisan migrate --force
fi

if [ "${role}" = "app" ]; then
  php artisan storage:link --force || true
fi

echo "[entrypoint] exec: $*"
exec "$@"
