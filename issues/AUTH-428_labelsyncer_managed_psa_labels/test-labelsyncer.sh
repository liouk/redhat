#!/usr/bin/env bash

main () {
  rm -f results.log
  clu4_13="irinis-v4.13.11-20230904"
  clu4_14="irinis-v4.14.0-0.ci-2023-09-04-004446-20230904"
  do_create=
  do_update=1

  if [ -n "$do_create" ]; then
    # 4.13/create
    test_create "${clu4_13}" "false" "baseline" "restricted"
    test_create "${clu4_13}" "false" "baseline" "privileged"
    test_create "${clu4_13}" "false" "privileged" "restricted"
    test_create "${clu4_13}" "false" "restricted" "privileged"
    test_create "${clu4_13}" "false" "privileged" "privileged"
    test_create "${clu4_13}" "true" "baseline" "restricted"
    test_create "${clu4_13}" "true" "baseline" "privileged"
    test_create "${clu4_13}" "true" "privileged" "restricted"
    test_create "${clu4_13}" "true" "restricted" "privileged"
    test_create "${clu4_13}" "true" "privileged" "privileged"

    # 4.14/create
    test_create "${clu4_14}" "false" "baseline" "restricted"
    test_create "${clu4_14}" "false" "baseline" "privileged"
    test_create "${clu4_14}" "false" "privileged" "restricted"
    test_create "${clu4_14}" "false" "restricted" "privileged"
    test_create "${clu4_14}" "false" "privileged" "privileged"
    test_create "${clu4_14}" "true" "baseline" "restricted"
    test_create "${clu4_14}" "true" "baseline" "privileged"
    test_create "${clu4_14}" "true" "privileged" "restricted"
    test_create "${clu4_14}" "true" "restricted" "privileged"
    test_create "${clu4_14}" "true" "privileged" "privileged"
  fi

  if [ -n "$do_update" ]; then
    # 4.13/update: the labelsyncer completely ignores user choice, and sets
    # the PSa labels to whatever it thinks based on the SCCs
    test_update "${clu4_13}" "false" "restricted" "privileged"
    test_update "${clu4_13}" "false" "restricted" "baseline"
    test_update "${clu4_13}" "false" "privileged" "baseline" "privileged"
    test_update "${clu4_13}" "false" "privileged" "restricted" "privileged"
    test_update "${clu4_13}" "true" "restricted" "privileged"
    test_update "${clu4_13}" "true" "restricted" "baseline"
    test_update "${clu4_13}" "true" "privileged" "baseline" "privileged"
    test_update "${clu4_13}" "true" "privileged" "restricted" "privileged"

    # 4.14/update: the labelsyncer will respect user choice as long as the
    # podSecurityLabelSync label is set to true
    test_update "${clu4_14}" "false" "privileged" "privileged"
    test_update "${clu4_14}" "false" "baseline" "baseline"
    test_update "${clu4_14}" "false" "baseline" "baseline" "privileged"
    test_update "${clu4_14}" "false" "restricted" "restricted" "privileged"
    test_update "${clu4_14}" "true" "restricted" "privileged"
    test_update "${clu4_14}" "true" "restricted" "baseline"
    test_update "${clu4_14}" "true" "privileged" "baseline" "privileged"
    test_update "${clu4_14}" "true" "privileged" "restricted" "privileged"
  fi
}

test_update () {
  local cluster="$1"
  local labelsync="$2"
  local expected_psa_label="$3"
  local user_psa_label="$4"
  local role_scc="$5"
  local ns="test-update"

  export KUBECONFIG="/home/ilias/redhat/.openshift_clusters/$cluster/auth/kubeconfig"
  oc version
  oc delete namespace $ns
  cat <<EOF | oc apply -f -
---
apiVersion: v1
kind: Namespace
metadata:
  name: $ns
  labels:
    security.openshift.io/scc.podSecurityLabelSync: "$labelsync"
EOF

  test_id="UPDATE_${cluster}_${labelsync}_${user_psa_label}_${role_scc:-no_role}"
  logfile_oc="oc_$test_id.log"
  logfile_cluster="cluster_$test_id.log"

  if [ -n "$role_scc" ]; then
    cat <<EOF | oc apply -f -
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mysa
  namespace: $ns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: myrole
  namespace: $ns
rules:
- apiGroups:
  - security.openshift.io
  resourceNames:
  - $role_scc
  resources:
  - securitycontextconstraints
  verbs:
  - use
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb
  namespace: $ns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: myrole
subjects:
- kind: ServiceAccount
  name: mysa
EOF
  fi

  echo "-----------------------BEFORE OVERWRITE-----------------------------" > $logfile_oc
  oc describe namespace $ns >> $logfile_oc
  oc label --overwrite namespace $ns "pod-security.kubernetes.io/enforce=$user_psa_label"
  echo "-----------------------AFTER OVERWRITE------------------------------" >> $logfile_oc
  oc describe namespace $ns >> $logfile_oc

  for pod in $(oc -n openshift-kube-controller-manager get pods -o custom-columns="NAME:.metadata.name" | grep kube-controller-manager-ip); do echo $pod && oc -n openshift-kube-controller-manager logs $pod -c cluster-policy-controller >> tmp.log; done && cat tmp.log | grep liouk > $logfile_cluster
  rm -f tmp.log

  result=$(oc get ns $ns -ojson|jq -r '.metadata.labels."pod-security.kubernetes.io/enforce"')
  [[ "$result" == "$expected_psa_label" ]] && mark="OK" || mark="XX"
  echo "$test_id => $result (expected: $expected_psa_label) $mark" >> results.log
}

test_create () {
  local cluster="$1"
  local labelsync="$2"
  local user_psa_label="$3"
  local role_scc="$4"
  local ns="test-create"

  export KUBECONFIG="/home/ilias/redhat/.openshift_clusters/$cluster/auth/kubeconfig"

  oc version
  oc delete namespace $ns && { echo -n "ns $ns deleted at "; date; }

  echo -n "will create ns at "; date
  cat <<EOF | oc apply -f -
---
apiVersion: v1
kind: Namespace
metadata:
  name: $ns
  labels:
    pod-security.kubernetes.io/enforce: $user_psa_label
    pod-security.kubernetes.io/enforce-version: v1.24
    pod-security.kubernetes.io/audit: $user_psa_label
    pod-security.kubernetes.io/audit-version: v1.24
    pod-security.kubernetes.io/warn: $user_psa_label
    pod-security.kubernetes.io/warn-version: v1.24
    security.openshift.io/scc.podSecurityLabelSync: "$labelsync"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mysa
  namespace: $ns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: myrole
  namespace: $ns
rules:
- apiGroups:
  - security.openshift.io
  resourceNames:
  - $role_scc
  resources:
  - securitycontextconstraints
  verbs:
  - use
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb
  namespace: $ns
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: myrole
subjects:
- kind: ServiceAccount
  name: mysa
EOF

  test_id="CREATE_${cluster}_${labelsync}_${user_psa_label}_${role_scc}"
  logfile_oc="oc_$test_id.log"
  logfile_cluster="cluster_$test_id.log"
  oc -n $ns describe role myrole > $logfile_oc
  oc describe ns $ns >> $logfile_oc
  echo "gathering cluster logs"
  for pod in $(oc -n openshift-kube-controller-manager get pods -o custom-columns="NAME:.metadata.name" | grep kube-controller-manager-ip); do echo $pod && oc -n openshift-kube-controller-manager logs $pod -c cluster-policy-controller >> tmp.log; done && cat tmp.log | grep liouk > $logfile_cluster
  rm -f tmp.log

  result=$(oc get ns $ns -ojson|jq '.metadata.labels."pod-security.kubernetes.io/audit"')
  echo "$test_id => $result" >> results.log
}

main "$@"
