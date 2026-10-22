#!/usr/bin/env bash

# Continuously verifies External OIDC authentication without using credentials
# from the current kubeconfig. If fetching a new token fails, retain the last
# valid token briefly so an issuer outage is not misreported as a KAS failure.

set -u

INTERVAL_SECONDS="${INTERVAL_SECONDS:-5}"
TOKEN_MAX_AGE_SECONDS="${TOKEN_MAX_AGE_SECONDS:-50}"
KEYCLOAK_USERNAME="${KEYCLOAK_USERNAME:-admin}"
KEYCLOAK_PASSWORD="${KEYCLOAK_PASSWORD:-password}"
KEYCLOAK_CLIENT_ID="${KEYCLOAK_CLIENT_ID:-admin-cli}"

ISSUER=$(oc get authentication cluster \
  -o jsonpath='{.spec.oidcProviders[0].issuer.issuerURL}')
API_SERVER=$(oc whoami --show-server)

if [[ -z "${ISSUER}" ]]; then
  echo "Authentication/cluster has no OIDC issuer" >&2
  exit 1
fi

ID_TOKEN=""
token_time=0

while true; do
  now=$(date +%s)
  issuer=fail

  if new_token=$(
    curl -sk --max-time 10 "${ISSUER}/protocol/openid-connect/token" \
      -d grant_type=password \
      -d client_id="${KEYCLOAK_CLIENT_ID}" \
      -d username="${KEYCLOAK_USERNAME}" \
      -d password="${KEYCLOAK_PASSWORD}" \
      -d scope=openid |
      jq -er '.id_token' 2>/dev/null
  ); then
    ID_TOKEN=${new_token}
    token_time=${now}
    issuer=ok
  fi

  token_age=$((now - token_time))
  if [[ -z "${ID_TOKEN}" || ${token_age} -ge ${TOKEN_MAX_AGE_SECONDS} ]]; then
    printf '%s SKIP issuer=%s no-valid-token\n' \
      "$(date --iso-8601=seconds)" "${issuer}"
  elif result=$(
    printf '%s' \
      '{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview"}' |
      oc --kubeconfig=/dev/null \
        --server="${API_SERVER}" \
        --insecure-skip-tls-verify \
        --token="${ID_TOKEN}" \
        create -f - \
        -o jsonpath='{.status.userInfo.username}' 2>&1
  ); then
    printf '%s OK issuer=%s tokenAge=%ss %s\n' \
      "$(date --iso-8601=seconds)" "${issuer}" "${token_age}" "${result}"
  else
    printf '%s AUTH_FAIL issuer=%s tokenAge=%ss %s\n' \
      "$(date --iso-8601=seconds)" "${issuer}" "${token_age}" "${result}"
  fi

  sleep "${INTERVAL_SECONDS}"
done
