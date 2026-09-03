#!/usr/bin/env bash
# Local-only helper: call the Coolify API on the test instance and print the full body.
# NOT part of the CLI — never commit real tokens here; they live in scripts/coolify.env (gitignored).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${COOLIFY_ENV:-$SCRIPT_DIR/coolify.env}"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
fi

BASE_URL="${COOLIFY_URL:-http://10.190.122.34:8000}"
TOKEN_READ="${COOLIFY_TOKEN_READ:-}"
TOKEN_WRITE="${COOLIFY_TOKEN_WRITE:-}"

usage() {
  cat <<'EOF'
usage: coolify-api.sh [-t read|write] [-X METHOD] <path> [json-body]

  -t          token kind: read (default) or write
  -X METHOD   HTTP method (default GET)
  path        API path, with or without leading slash
  json-body   optional JSON request body

examples:
  coolify-api.sh /teams/current
  coolify-api.sh teams
  coolify-api.sh -t write projects
  coolify-api.sh -t write -X POST applications/dockerfile '{"project_uuid":"..."}'
  coolify-api.sh -X DELETE -t write applications/some-uuid

Override the instance or use a one-off token via env:
  COOLIFY_URL=http://... coolify-api.sh /teams/current
  COOLIFY_TOKEN=67|abc... coolify-api.sh /teams/current
EOF
}

TOKEN_KIND="read"
METHOD="GET"
while getopts "t:X:h" opt; do
  case "$opt" in
    t) TOKEN_KIND="$OPTARG" ;;
    X) METHOD="$OPTARG" ;;
    h) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done
shift $((OPTIND - 1))

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi
PATH_PART="$1"
BODY="${2:-}"

if [[ -n "${COOLIFY_TOKEN:-}" ]]; then
  TOKEN="$COOLIFY_TOKEN"
else
  case "$TOKEN_KIND" in
    read) TOKEN="$TOKEN_READ" ;;
    write) TOKEN="$TOKEN_WRITE" ;;
    *) echo "unknown token kind '$TOKEN_KIND' (use read|write)" >&2; exit 2 ;;
  esac
  if [[ -z "$TOKEN" ]]; then
    echo "no token configured for '$TOKEN_KIND' — set it in $ENV_FILE" >&2
    exit 3
  fi
fi

[[ "$PATH_PART" == /* ]] || PATH_PART="/$PATH_PART"
URL="${BASE_URL%/}/api/v1$PATH_PART"

ARGS=(-sS -m 30 -X "$METHOD"
  -H "Authorization: Bearer $TOKEN"
  -H "Accept: application/json")
if [[ -n "$BODY" ]]; then
  ARGS+=(-H "Content-Type: application/json" --data "$BODY")
fi

BODY_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE"' EXIT
STATUS=$(curl "${ARGS[@]}" -o "$BODY_FILE" -w '%{http_code}' "$URL") || {
  echo "curl failed for $URL" >&2
  exit 1
}

echo "→ $METHOD $URL"
echo "HTTP $STATUS"
echo
if [[ -s "$BODY_FILE" ]]; then
  if python3 -m json.tool "$BODY_FILE" >/dev/null 2>&1; then
    python3 -m json.tool "$BODY_FILE"
  else
    cat "$BODY_FILE"
    echo
  fi
fi

[[ "$STATUS" -ge 400 ]] && exit 1
exit 0
