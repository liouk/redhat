#! /usr/bin/env bash

# Create a htpasswd IDP by interactively adding users to
# a htpasswd file and deploy to cluster

set -e

args="-c"
while true; do
  read -p "Add user (q to stop): " user
  if [ "$user" == "q" ]; then break; fi
  htpasswd $args temp.htpasswd $user
  # use -c only once in order to keep adding users to the same file
  args=""
  echo
done

if [ ! -f temp.htpasswd ]; then echo "no users added; abort" && exit; fi
htpasswd_hash=$(cat temp.htpasswd | base64)
rm temp.htpasswd

oc create -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: htpass-secret
  namespace: openshift-config
type: Opaque
data:
  htpasswd: $htpasswd_hash
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
