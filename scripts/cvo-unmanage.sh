#!/usr/bin/env bash

set -e

usage () {
  printf "\n$0 <namespace> <override>\n    Patch the Cluster Version Operator to not manage the specified component\n"
}

deployment () {
  ns="$1"
  name="$2"
  value="${3:-true}"

  [[ -z "$ns" ]] && { echo "error: undefined namespace"; usage; exit 1; }
  [[ -z "$name" ]] && { echo "error: undefined deployment override name"; usage; exit 1; }

  kubectl patch clusterversion version --type='merge' -p "$(cat <<- EOF
spec:
  overrides:
  - group: apps
    kind: Deployment
    namespace: $ns
    name: $name
    unmanaged: $value
EOF
  )"
}

crd () {
  name="$1"
  value="${2:-true}"

  [[ -z "$name" ]] && { echo "error: undefined CRD override name"; usage; exit 1; }

  kubectl patch clusterversion version --type='merge' -p "$(cat <<- EOF
spec:
  overrides:
  - group: config.openshift.io
    kind: CustomResourceDefinition
    name: $name
    namespace: ""
    unmanaged: $value
EOF
  )"
}

main () {
  if [[ "$1" == "crd" ]]; then
    shift
    crd "$@"
  else
    deployment "$@"
  fi
}

main "$@"
