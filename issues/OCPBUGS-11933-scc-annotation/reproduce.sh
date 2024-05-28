#!/usr/bin/env bash

pod_name="${1:-badpod}"
ns="irinis-test"

# SCCs are disabled in the 'default' namespace, therefore we need
# to create a new one to reproduce the issue
oc delete namespace $ns
oc create namespace $ns

# Warning: would violate PodSecurity "restricted:v1.24": allowPrivilegeEscalation != false (container "fedora" must set securityContext.allowPrivilegeEscalation=false), unrestricted capabilities (container "fedora" must set securityContext.capabilities.drop=["ALL"]), runAsNonRoot != true (pod or container "fedora" must set securityContext.runAsNonRoot=true), seccompProfile (pod or container "fedora" must set securityContext.seccompProfile.type to "RuntimeDefault" or "Localhost")

cat <<EOF | oc -n $ns create -f -
---
kind: Pod
apiVersion: v1
metadata:
  name: $pod_name
spec:
    restartPolicy: Never
    containers:
    - name: fedora
      image: fedora:latest
      command:
      - sleep
      args:
      - "infinity"
---
kind: Pod
apiVersion: v1
metadata:
  name: goodpod
spec:
    restartPolicy: Never
    securityContext:
      runAsNonRoot: true
      seccompProfile:
        type: RuntimeDefault
    containers:
    - name: fedora
      image: fedora:latest
      command:
      - sleep
      args:
      - "infinity"
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop:
          - ALL
EOF

oc -n $ns get pod $pod_name -ojson|jq '.|{podname: .metadata.name, "openshift.io/scc": .metadata.annotations."openshift.io/scc"}'
oc -n $ns get pod goodpod -ojson|jq '.|{podname: .metadata.name, "openshift.io/scc": .metadata.annotations."openshift.io/scc"}'

echo
oc -n $ns adm policy scc-subject-review -u $(oc whoami) -f <(oc -n $ns get pod $pod_name -oyaml)

# oc describe scc anyuid
