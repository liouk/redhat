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

  update_container_env_image "$container" "$container_idx" "$env_var" "$env_var_idx"
}

patch_cluster-authentication-operator () {
  update_deployment_image "openshift-authentication-operator" "authentication-operator" "authentication-operator" "0"
}

patch_console-operator () {
  update_deployment_image "openshift-console-operator" "console-operator" "console-operator" "0"
}

patch_oauth-apiserver () {
  kubectl get deployment -n {openshift-,}authentication-operator -o json \
    | jq ".spec.template.spec.containers[0].env[1].value = \"$IMG\"" \
    | kubectl replace -f -

  kubectl get deployment -n {openshift-,}authentication-operator -o json \
    | jq "
    {
      containers: {
        name: .spec.template.spec.containers[0].name,
        env: .spec.template.spec.containers[0].env[1]}
    }"
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

patch_kube-apiserver () {
  local container="kube-apiserver-operator"
  local container_idx=0
  local env_var="IMAGE"
  local env_var_idx=0

  update_container_env_image "$container" "$container_idx" "$env_var" "$env_var_idx"
}

patch_kube-apiserver-operator () {
  local container="kube-apiserver-operator"
  local container_idx=0
  local env_var_idx=1

  local deployment=$(kubectl get deployment -n {openshift-,}$container -o json)

  # replace OPERATOR_IMAGE
  deployment=$(echo "$deployment" \
    | jq ".spec.template.spec.containers[$container_idx].env[$env_var_idx].value = \"$IMG\"")

  # replace container.image
  deployment=$(echo "$deployment" \
    | jq ".spec.template.spec.containers[$container_idx].image = \"$IMG\"")

  echo $deployment | kubectl replace -f -

  echo "New image:"
  kubectl get deployment -n {openshift-,}$container -o json \
    | jq "
    {
      containers: {
        name: .spec.template.spec.containers[$container_idx].name,
        env: .spec.template.spec.containers[$container_idx].env[$env_var_idx],
        image: .spec.template.spec.containers[$container_idx].image}
    }"
}

update_deployment_image () {
  ns="$1"
  deployment="$2"
  container="$3"
  idx="$4"

  kubectl -n $ns set image deployment/$deployment $container="$IMG"
  kubectl -n $ns get deployment $deployment -o custom-columns="NAME:.metadata.name,IMAGE:.spec.template.spec.containers[$idx].image"
}

update_container_env_image () {
  local container="$1"
  local container_idx="$2"
  local env_var="$3"
  local env_var_idx="$4"
  local show_only="$5"

  local deployment=$(kubectl get deployment -n {openshift-,}$container -o json)

  [[ $(echo "$deployment" | jq ".spec.template.spec.containers[$container_idx].name") == "\"$container\"" ]] || {
    echo "container at index $container_idx is not '$container'"
    exit 1
  }

  [[ $(echo "$deployment" | jq ".spec.template.spec.containers[$container_idx].env[$env_var_idx].name") == "\"$env_var\"" ]] || {
    echo "env_var at index $env_var_idx is not '$env_var'"
    exit 1
  }

  if [ -z "$show_only" ]; then
    echo "$deployment" \
      | jq ".spec.template.spec.containers[$container_idx].env[$env_var_idx].value = \"$IMG\"" \
      | kubectl replace -f -

    echo "New image:"
  else
    echo "Current image:"
  fi

  kubectl get deployment -n {openshift-,}$container -o json \
    | jq "
    {
      containers: {
        name: .spec.template.spec.containers[$container_idx].name,
        env: .spec.template.spec.containers[$container_idx].env[$env_var_idx]}
    }"
}

main () {
  WHAT="$1"
  IMG="$2"

  [ -n "$IMG" ] || { echo "image not specified"; usage; exit 1; }
  patch_"$WHAT" || { echo "I don't know how to patch '$what'"; usage; exit 1; }
}

main "$@"
