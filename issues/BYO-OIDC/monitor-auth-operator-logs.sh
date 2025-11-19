#!/usr/bin/env bash

trap 'exit 0' SIGINT SIGTERM

while true; do
  AVAILABLE=$(oc get clusterversion version -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null)
  PROGRESSING=$(oc get clusterversion version -o jsonpath='{.status.conditions[?(@.type=="Progressing")].status}' 2>/dev/null)

  echo "[$(date +%H:%M:%S)] Available=$AVAILABLE Progressing=$PROGRESSING"

  # Dump pod logs (only from Running pods)
  PODS=$(oc -n openshift-authentication-operator get pods -l app=authentication-operator --field-selector=status.phase=Running -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
  for POD in $PODS; do
    CREATED=$(oc -n openshift-authentication-operator get pod "$POD" -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null | sed 's/[:-]//g; s/T/-/; s/Z.*//')
    oc -n openshift-authentication-operator logs "$POD" --all-containers=true > "${CREATED}-${POD}.log" 2>&1
  done

  # Check if upgrade complete
  if [[ "$AVAILABLE" == "True" && "$PROGRESSING" == "False" ]]; then
    echo "Upgrade complete - capturing events"
    oc get event -n openshift-authentication-operator -ojson > "events-openshift-authentication-operator-$(date +%Y%m%d-%H%M%S).json"
    exit 0
  fi

  sleep 10
done
