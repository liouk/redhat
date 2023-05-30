#!/usr/bin/env bash

set -e

usage () {
  printf "\n$0 <namespace> <override>\n    Patch the Cluster Version Operator to not manage the specified component\n"
}

main () {
  ns="$1"
  name="$2"
  value="${3:-true}"

  [[ -z "$ns" ]] && { echo "error: undefined namespace"; usage; exit 1; }
  [[ -z "$name" ]] && { echo "error: undefined override name"; usage; exit 1; }

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

main "$@"
