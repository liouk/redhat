#!/usr/bin/env bash
oc delete namespace test
oc delete namespace test2

cat <<EOF | oc apply -f -
---
apiVersion: v1
kind: Namespace
metadata:
  name: test

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
  namespace: test

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mysa
  namespace: test2

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mysa2
  namespace: test2

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

---
kind: Job
apiVersion: batch/v1
metadata:
  name: myjob
  namespace: test2
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
