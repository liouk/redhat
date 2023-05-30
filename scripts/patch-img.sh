#!/usr/bin/env bash

set -e

usage () {
  printf "\n$0 <what> <img>\n    Patch the value of an env var image\n"
}

patch_cluster-policy-controller () {
  local container="kube-controller-manager-operator"
  local container_idx=0
  local env_var="CLUSTER_POLICY_CONTROLLER_IMAGE"
  local env_var_idx=2
  local deployment=$(kubectl get deployment -n {openshift-,}$container -o json)

  [[ $(echo "$deployment" | jq ".spec.template.spec.containers[$container_idx].name") == "\"$container\"" ]] || {
    echo "container at index $container_idx is not '$container'"
    exit 1
  }

  [[ $(echo "$deployment" | jq ".spec.template.spec.containers[$container_idx].env[$env_var_idx].name") == "\"$env_var\"" ]] || {
    echo "env_var at index $env_var_idx is not '$env_var'"
    exit 1
  }

  echo "$deployment" \
    | jq ".spec.template.spec.containers[$container_idx].env[$env_var_idx].value = \"$IMG\"" \
    | kubectl replace -f -

  echo "New image:"
  kubectl get deployment -n {openshift-,}$container -o json \
    | jq "
    {
      containers: {
        name: .spec.template.spec.containers[$container_idx].name,
        env: .spec.template.spec.containers[$container_idx].env[$env_var_idx]}
    }"
}

patch_cluster-authentication-operator () {
  kubectl -n openshift-authentication-operator \
    set image deployment/authentication-operator \
    authentication-operator="$IMG"

  kubectl -n openshift-authentication-operator \
    get deployment authentication-operator \
    -o custom-columns="NAME:.metadata.name,IMAGE:.spec.template.spec.containers[0].image"
}

patch_oauth-server () {
  kubectl get deployment -n {openshift-,}authentication-operator -o json \
    | jq ".spec.template.spec.containers[0].env[0].value = \"$IMG\"" \
    | kubectl replace -f -

  kubectl get deployment -n {openshift-,}authentication-operator -o json \
    | jq "
    {
      containers: {
        name: .spec.template.spec.containers[0].name,
        env: .spec.template.spec.containers[0].env[0]}
    }"
}

main () {
  WHAT="$1"
  IMG="$2"

  [ -n "$IMG" ] || { echo "image not specified"; usage; exit 1; }
  patch_"$WHAT" || { echo "I don't know how to patch '$what'"; usage; exit 1; }
}

main "$@"
