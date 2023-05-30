#! /usr/bin/env bash

# htpasswd -c -B -b ilias.htpasswd ilias 1234
# cat ilias.htpasswd | base64

oc create -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: htpass-secret
  namespace: openshift-config
type: Opaque
data:
  htpasswd: aWxpYXM6JDJ5JDA1JDY2VExRUmYyWGZmNG1Cd0lkUDhlVk81eXdnSllKTXcuZjBEVzRSMWtYMDFOaUovWHpUMFhxCg==
EOF
oc -n openshift-config get secret htpass-secret

oc apply -f - <<EOF
apiVersion: config.openshift.io/v1
kind: OAuth
metadata:
  name: cluster
spec:
  identityProviders:
  - name: my_htpasswd_provider
    mappingMethod: claim
    type: HTPasswd
    htpasswd:
      fileData:
        name: htpass-secret
EOF
oc get oauth cluster
