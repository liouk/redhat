#!/usr/bin/env bash

# Observes the direct-OIDC to webhook-OIDC transition. The rollout value is
# updated/ready/desired. The KAS mode describes the latest rendered revision,
# not necessarily the revision currently installed on every control-plane node.

set -u

INTERVAL_SECONDS="${INTERVAL_SECONDS:-2}"

while true; do
  gates=$(
    oc get featuregate cluster -o json |
      jq -r '
        [.status.featureGates[].enabled[]?.name] as $enabled |
        if (
          ($enabled | index("ExternalOIDCAsWebhook")) and
          ($enabled | index("ExternalOIDCExternalClaimsSourcing"))
        ) then "on" else "off" end
      '
  )

  if service=$(
    oc -n openshift-oauth-apiserver get service token-review -o json 2>/dev/null
  ); then
    endpoint=$(jq -r '"\(.metadata.uid)/\(.spec.clusterIP)"' <<<"${service}")
  else
    endpoint=absent
  fi

  if cert=$(
    oc -n openshift-oauth-apiserver \
      get secret token-review-serving-cert -o json 2>/dev/null
  ); then
    cert=$(
      jq -r '
        if (.data["tls.crt"] and .data["tls.key"])
        then "ready" else "pending" end
      ' <<<"${cert}"
    )
  else
    cert=absent
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

  webhook_secret=$(
    oc -n openshift-config \
      get secret webhook-authentication-integrated-oauth \
      -o name 2>/dev/null || true
  )
  if [[ -n "${webhook_secret}" ]]; then
    webhook_secret=present
  else
    webhook_secret=absent
  fi

  revision=$(oc get kubeapiserver cluster \
    -o jsonpath='{.status.latestAvailableRevision}')

  if kas_config=$(
    oc -n openshift-kube-apiserver \
      get configmap "config-${revision}" \
      -o jsonpath='{.data.config\.yaml}' 2>/dev/null
  ); then
    kas_auth=$(
      jq -r '
        .apiServerArguments |
        if ."authentication-token-webhook-config-file" then "webhook"
        elif ."authentication-config" then "direct"
        else "unknown" end
      ' <<<"${kas_config}"
    )
  else
    kas_auth=unknown
  fi

  printf '%s gates=%s endpoint=%s cert=%s oauthAPIServer=%s rollout=%s webhookSecret=%s kas=%s@%s\n' \
    "$(date --iso-8601=seconds)" \
    "${gates}" "${endpoint}" "${cert}" "${mode}" "${rollout}" \
    "${webhook_secret}" "${kas_auth}" "${revision}"

  sleep "${INTERVAL_SECONDS}"
done
