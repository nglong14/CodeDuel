#!/usr/bin/env bash
# ==============================================================================
# CodeDuel post-deploy smoke test
# ==============================================================================
# Called by .github/workflows/deploy.yml after a rollout, and runnable by hand
# against a port-forward or a public host.
#
# Read-only by default. /readyz is the check that carries the weight: the handler
# pings PostgreSQL and Redis, so a 200 proves the pods can reach RDS and
# ElastiCache, which is the failure mode a deploy actually introduces.
#
# --with-auth adds a register -> /api/me round-trip, which additionally proves
# the database is writable and JWT signing works. It creates a user row, so it is
# opt-in and meant for dev; prod deploys run without it rather than accumulating
# throwaway accounts in the real users table.
#
# Requires: curl, and jq when --with-auth is used.
#
# Usage:
#   ./infra/scripts/smoke.sh <base-url> [--with-auth]
#
# Example:
#   kubectl port-forward -n codeduel-dev svc/gateway 8080:8080 &
#   ./infra/scripts/smoke.sh http://localhost:8080 --with-auth
# ==============================================================================

set -euo pipefail

BASE_URL="${1:-}"
WITH_AUTH="${2:-}"

if [ -z "$BASE_URL" ]; then
  echo "Usage: $0 <base-url> [--with-auth]"
  exit 1
fi

FAILURES=0

# check <description> <expected-status> <curl-args...>
# Prints the body on mismatch; bodies are small JSON API errors.
check() {
  local description="$1" expected="$2"
  shift 2
  local body status
  body="$(curl -sS --max-time 15 -w '\n%{http_code}' "$@")" || {
    echo "FAIL  $description: curl failed"
    FAILURES=$((FAILURES + 1))
    return 0
  }
  status="${body##*$'\n'}"
  body="${body%$'\n'*}"
  if [ "$status" = "$expected" ]; then
    echo "ok    $description ($status)"
    SMOKE_BODY="$body"
  else
    echo "FAIL  $description: got $status, want $expected"
    echo "      $body"
    FAILURES=$((FAILURES + 1))
    SMOKE_BODY=""
  fi
}

echo "==> Smoke testing $BASE_URL"

check "GET /healthz" 200 "$BASE_URL/healthz"
check "GET /readyz (proves RDS + ElastiCache reachable from the pod)" 200 "$BASE_URL/readyz"
case "${SMOKE_BODY:-}" in
  *'"ready"'*) echo "ok    /readyz reports ready" ;;
  '') ;; # the check above already counted this failure
  *)
    echo "FAIL  /readyz did not report ready: $SMOKE_BODY"
    FAILURES=$((FAILURES + 1))
    ;;
esac

# No token: proves the auth path is wired and rejecting, without writing anything.
check "GET /api/me without a token is rejected" 401 "$BASE_URL/api/me"

if [ "$WITH_AUTH" = "--with-auth" ]; then
  # .invalid is reserved by RFC 2606, so these addresses can never be delivered to.
  EMAIL="smoke-$(date +%s)-$RANDOM@codeduel.invalid"
  PASSWORD="$(head -c 24 /dev/urandom | base64)"

  check "POST /api/auth/register" 201 \
    -X POST "$BASE_URL/api/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"

  TOKEN="$(printf '%s' "${SMOKE_BODY:-}" | jq -r '.token // empty')"
  if [ -z "$TOKEN" ]; then
    echo "FAIL  register returned no token"
    FAILURES=$((FAILURES + 1))
  else
    check "GET /api/me with the new token" 200 \
      "$BASE_URL/api/me" -H "Authorization: Bearer $TOKEN"
  fi
fi

echo ""
if [ "$FAILURES" -ne 0 ]; then
  echo "Smoke test FAILED ($FAILURES check(s))"
  exit 1
fi
echo "Smoke test passed."
