#!/usr/bin/env bash

# Observes transitions between Integrated OAuth and webhook-based External OIDC.
# The rollout value is updated/ready/desired. In the forward direction, the
# legacy api Service and APIServices must disappear before external-oidc mode.
# In the reverse direction, they must return before standard mode.

set -u

INTERVAL_SECONDS="${INTERVAL_SECONDS:-5}"

resource_state() {
  local resource=$1
  local name=$2
  shift 2

  if oc get "${resource}" "${name}" "$@" >/dev/null 2>&1; then
    printf 'present'
  else
    printf 'absent'
  fi
}

while true; do
  auth=$(oc get authentication cluster -o jsonpath='{.spec.type}')
  if [[ -z "${auth}" ]]; then
    auth=IntegratedOAuth
  fi

  api=$(resource_state service api \
    --namespace=openshift-oauth-apiserver)
  oauth_api=$(resource_state apiservice v1.oauth.openshift.io)
  user_api=$(resource_state apiservice v1.user.openshift.io)

  if token_review=$(
    oc -n openshift-oauth-apiserver \
      get service token-review -o json 2>/dev/null
  ); then
    token_review=$(
      jq -r '"\(.metadata.uid)/\(.spec.clusterIP)"' <<<"${token_review}"
    )
  else
    token_review=absent
  fi

  if deployment=$(
    oc -n openshift-oauth-apiserver \
      get deployment apiserver -o json 2>/dev/null
  ); then
    mode=$(
      jq -r '
        [.spec.template.spec.containers[]
          | select(.name == "oauth-apiserver")
          | (.command[]?, .args[]?)]
        | join(" ")
        | if contains("external-oidc")
          then "external-oidc" else "standard" end
      ' <<<"${deployment}"
    )
    rollout=$(
      jq -r '
        "\(.status.updatedReplicas // 0)/\(.status.readyReplicas // 0)/\(.spec.replicas // 0)"
      ' <<<"${deployment}"
    )
  else
    mode=absent
    rollout=-
  fi

  printf '%s auth=%s api=%s oauthAPI=%s userAPI=%s tokenReview=%s mode=%s rollout=%s\n' \
    "$(date --iso-8601=seconds)" \
    "${auth}" "${api}" "${oauth_api}" "${user_api}" \
    "${token_review}" "${mode}" "${rollout}"

  sleep "${INTERVAL_SECONDS}"
done
