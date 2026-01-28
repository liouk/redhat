#!/usr/bin/env bash

# install loki
oc create namespace netobserv
oc wait --for=jsonpath='{.status.phase}'=Active namespace/netobserv --timeout=30s
oc apply -f <(curl -s -L https://raw.githubusercontent.com/netobserv/documents/5410e65b8e05aaabd1244a9524cfedd8ac8c56b5/examples/zero-click-loki/1-storage.yaml) -n netobserv
oc apply -f <(curl -s -L https://raw.githubusercontent.com/netobserv/documents/5410e65b8e05aaabd1244a9524cfedd8ac8c56b5/examples/zero-click-loki/2-loki.yaml) -n netobserv

# create network observability operator namespace and operator group
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-netobserv-operator
  labels:
    openshift.io/cluster-monitoring: "true"
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-netobserv-operator
  namespace: openshift-netobserv-operator
spec: {}
EOF

# create network observability operator subscription
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: netobserv-operator
  namespace: openshift-netobserv-operator
spec:
  channel: stable
  installPlanApproval: Automatic
  name: netobserv-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF

# Wait for the FlowCollector CRD to appear
TIMEOUT=180
ELAPSED=0
while ! oc get crd flowcollectors.flows.netobserv.io &>/dev/null; do
  if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "error: timeout waiting for FlowCollector CRD"
    exit 1
  fi
  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

# Wait for it to be established
oc wait --for condition=established --timeout=30s crd/flowcollectors.flows.netobserv.io

# Wait for the operator deployment to be created
TIMEOUT=180
ELAPSED=0
while ! oc get deployment netobserv-controller-manager -n openshift-netobserv-operator &>/dev/null; do
  if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "error: timeout waiting for NetObserv operator deployment"
    exit 1
  fi
  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

# Wait for the operator deployment to be ready
oc wait --for=condition=available --timeout=180s deployment/netobserv-controller-manager -n openshift-netobserv-operator

# create a basic FlowCollector with loki
oc apply -f flowcollector.yaml

echo ""
echo "Network Observability Operator + Loki have been deployed! To clean up, run:"
echo "  oc delete flowcollectors.flows.netobserv.io cluster"
echo "  oc delete ns openshift-netobserv-operator netobserv"