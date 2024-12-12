# /usr/bin/env bash

ns=test-rbrs
sa1="sa1"
sa2="sa2"
user1="user1"
user2="user2"
group1="group1"
group2="group2"
role="role"

### Cleanup
oc delete --ignore-not-found=true ns $ns
oc create ns $ns

### Create rolebinding restrictions
oc apply -f - <<EOF
apiVersion: authorization.openshift.io/v1
kind: RoleBindingRestriction
metadata:
  name: test-user-restriction
  namespace: ${ns}
spec:
  userrestriction:
    users:
      - ${user1}
---
apiVersion: authorization.openshift.io/v1
kind: RoleBindingRestriction
metadata:
  name: test-group-restriction
  namespace: ${ns}
spec:
  grouprestriction:
    groups:
      - ${group1}
---
apiVersion: authorization.openshift.io/v1
kind: RoleBindingRestriction
metadata:
  name: test-sa-restriction
  namespace: ${ns}
spec:
  serviceaccountrestriction:
    serviceaccounts:
    - name: ${sa1}
      namespace: ${ns}
EOF

### Create allowed rolebindings
echo -e "\n>>> Test: create allowed rolebindings"
oc apply -f - <<EOF && echo -e "===> PASS" || echo -e "===> FAIL (should have been allowed)"
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-rb-user-${user1}
  namespace: ${ns}
subjects:
  - kind: User
    name: ${user1}
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: ${role}
  apiGroup: rbac.authorization.k8s.io
EOF

oc apply -f - <<EOF && echo -e "===> PASS" || echo -e "===> FAIL (should have been allowed)"
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-rb-group-${group1}
  namespace: ${ns}
subjects:
  - kind: Group
    name: ${group1}
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: ${role}
  apiGroup: rbac.authorization.k8s.io
EOF

oc apply -f - <<EOF && echo -e "===> PASS" || echo -e "===> FAIL (should have been allowed)"
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-rb-sa-${sa1}
  namespace: ${ns}
subjects:
  - kind: ServiceAccount
    name: ${sa1}
    namespace: ${ns}
roleRef:
  kind: Role
  name: ${role}
  apiGroup: rbac.authorization.k8s.io
EOF

### Create violating rolebindings
echo -e "\n>>> Test: create violating rolebindings"
oc apply -f - <<EOF && echo -e "===> FAIL (should have been forbidden)" || echo -e "===> PASS"
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-rb-user-${user2}
  namespace: ${ns}
subjects:
  - kind: User
    name: ${user2}
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: ${role}
  apiGroup: rbac.authorization.k8s.io
EOF

oc apply -f - <<EOF && echo -e "===> FAIL (should have been forbidden)" || echo -e "===> PASS"
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-rb-group-${group2}
  namespace: ${ns}
subjects:
  - kind: Group
    name: ${group2}
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: ${role}
  apiGroup: rbac.authorization.k8s.io
EOF

oc apply -f - <<EOF && echo -e "===> FAIL (should have been forbidden)" || echo -e "===> PASS"
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: test-rb-sa-${sa2}
  namespace: ${ns}
subjects:
  - kind: ServiceAccount
    name: ${sa2}
    namespace: ${ns}
roleRef:
  kind: Role
  name: ${role}
  apiGroup: rbac.authorization.k8s.io
EOF
