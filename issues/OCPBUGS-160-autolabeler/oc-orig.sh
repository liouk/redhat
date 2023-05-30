#!/usr/bin/env bash
oc delete namespace test && { echo -n "ns test deleted at "; date; }

echo -n "will create ns at "; date
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
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: myrole
  namespace: test
rules:
- apiGroups:
  - security.openshift.io
  resourceNames:
  - privileged
  resources:
  - securitycontextconstraints
  verbs:
  - use

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
EOF

read -p "Run job? [yN] " yn
case $yn in
  y|yes) ;;
  *) exit;;
esac

echo -n "will execute job at "; date
cat <<EOF | oc apply -f -
---
kind: Job
apiVersion: batch/v1
metadata:
  name: myjob
  namespace: test
spec:
  template:
    spec:
      containers:
        - name: ubi
          image: registry.access.redhat.com/ubi8
          command: ["/bin/bash", "-c"]
          args: ["whoami; sleep infinity"]
      restartPolicy: Never
      securityContext:
        runAsUser: 0
      serviceAccount: mysa
      terminationGracePeriodSeconds: 2
EOF
