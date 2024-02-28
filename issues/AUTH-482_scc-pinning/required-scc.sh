#!/usr/bin/env bash

main () {
  ns_arg="$1"
  pod_arg="$2"
  [ -n "$ns_arg" ] && nslist="$ns_arg" || nslist=$(oc get namespaces -o json | jq -r '.items[] | select(.metadata.labels == null or .metadata.labels["openshift.io/run-level"] == null or .metadata.labels["openshift.io/run-level"] == "") | .metadata.name' | grep -E "^openshift")

  for ns in $nslist; do
    [ -n "$pod_arg" ] && podlist="$pod_arg" || podlist=$(oc -n $ns get pods -o json | jq -c '.items[] | select(.metadata.annotations == null or .metadata.annotations["openshift.io/required-scc"] == null or .metadata.annotations["openshift.io/required-scc"] == "") | .metadata.name' --raw-output)

    pod_count=$(echo "$podlist" | awk NF | wc -l)
    if [ "$pod_count" -eq 0 ]; then
      continue
    fi

    >&2 echo "$ns"
    echo "## $ns"
    echo "|pod|scc|required-scc|owner|"
    echo "| --- | --- | --- | --- |"
    for pod in $podlist; do
      oc -n $ns get pod $pod -o jsonpath='| {.metadata.name} | {.metadata.annotations.openshift\.io/scc} |{.metadata.annotations.openshift\.io/required-scc}| {.metadata.ownerReferences[0].kind}/{.metadata.ownerReferences[0].name} |{"\n"}'
    done
  done
}

main "$@"
