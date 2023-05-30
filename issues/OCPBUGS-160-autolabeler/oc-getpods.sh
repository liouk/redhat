#!/usr/bin/env bash

set -e

oc delete namespace test || true
oc delete namespace test2 || true

cat <<EOF | oc apply -f -
---
apiVersion: v1
kind: Namespace
metadata:
  name: test
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mysa
  namespace: test
---
apiVersion: v1
kind: Namespace
metadata:
  name: test2
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mysa
  namespace: test2
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: myrole
  namespace: test
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - list
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb
  namespace: test
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: myrole
subjects:
- kind: ServiceAccount
  name: mysa
  # namespace: test
EOF

curltest () {
  apiserver=$(oc config view -o jsonpath='{.clusters[0].cluster.server}')
  ns="$1"
  sa="$2"

  echo
  echo "TESTING $ns/$sa"
  secret=$(oc -n $ns get secrets -o name | grep "$sa-token")
  token=$(oc -n $ns get $secret -o jsonpath='{.data.token}' | base64 -d)
  oc -n $ns get $secret -o json | jq -Mr '.data["ca.crt"]' | base64 -d > "$ns-$sa-ca.crt"
  curl -s "$apiserver/api/v1/namespaces/$ns/pods/" --header "Authorization: Bearer $token" --cacert "$ns-$sa-ca.crt"
  echo

  rm "$ns-$sa-ca.crt"
}

oc -n test get rolebinding myrb -o yaml
curltest "test" "mysa"
curltest "test2" "mysa"
