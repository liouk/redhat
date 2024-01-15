#!/usr/bin/env bash

red () {
  echo -e "\e[41m\e[30m\e[1m $1 \e[0m $2"
}

green () {
  echo -e "\e[42m\e[30m\e[1m $1 \e[0m $2"
}

yellow () {
  echo -e "\e[43m\e[30m\e[1m $1 \e[0m $2"
}

step () {
  if [ $? -ne 0 ]; then
    red "FAIL" "$1"
    exit $?
  fi

  green " OK " "$1"
}

# CVO overrides
read -p "Continue to CVO overrides?" user_inp
kubectl patch clusterversion version --type="merge" --patch="$(cat <<- EOF
spec:
  overrides:
  - group: rbac.authorization.k8s.io
    kind: Role
    name: console-operator
    namespace: openshift-config-managed
    unmanaged: true
  - group: rbac.authorization.k8s.io
    kind: ClusterRole
    name: console-operator
    namespace: ""
    unmanaged: true
  - group: apps
    kind: Deployment
    namespace: openshift-console-operator
    name: console-operator
    unmanaged: true
EOF
)"
step "CVO overrides: console-operator deployment, console-operator roles\n"

read -p "Enable featuregate?" user_inp
# oc edit featuregate/cluster
# spec:
#   featureSet: TechPreviewNoUpgrade

read -p "Patch console-operator role?" user_inp

# give required permissions to the console-operator SA
# as seen in https://github.com/openshift/console-operator/pull/811
kubectl -n openshift-config-managed get role console-operator -oyaml | grep -q secrets
if [ $? -ne 0 ]; then
	kubectl -n openshift-config-managed patch role console-operator --type="json" --patch="$(cat <<- EOF
[
  {
    "op": "add",
    "path": "/rules/-",
    "value": {
      "apiGroups": [""],
      "resources": ["secrets"],
      "verbs": ["get", "list", "watch"]
    }
  }
]
EOF
	)"
	step "update openshift-config-managed role console-operator\n"
else
	yellow "SKIP" "update openshift-config-managed role console-operator\n"
fi

read -p "Patch console-operator cluster role?" user_inp

kubectl get clusterrole console-operator -oyaml | grep -q authentications
if [ $? -ne 0 ]; then
	kubectl patch clusterrole console-operator --type="json" --patch="$(cat <<- EOF
[
  {
    "op": "replace",
    "path": "/rules/1",
    "value": {
      "apiGroups": ["oauth.openshift.io"],
      "resources": ["oauthclients"],
      "verbs": ["get", "list", "watch"],
    }
  },
  {
    "op": "add",
    "path": "/rules/3/resources/-",
    "value": "authentications"
  },
  {
    "op": "add",
    "path": "/rules/-",
    "value": {
      "apiGroups": ["oauth.openshift.io"],
      "resources": ["oauthclients"],
      "verbs": ["update"],
      "resourceNames": ["console"]
    }
  },
  {
    "op": "add",
    "path": "/rules/-",
    "value": {
      "apiGroups": ["config.openshift.io"],
      "resources": ["authentications/status"],
      "verbs": ["patch"]
    }
  }
]
EOF
	)"
	step "patch clusterrole console-operator\n"
else
	yellow "SKIP" "patch clusterrole console-operator\n"
fi

# set console-operator image
read -p "console-operator image (empty skips): " img
if [[ -n "$img" ]]; then
	echo "image: $img"
	kubectl -n openshift-console-operator set image deployment/console-operator console-operator="$img"
	step "override console-operator image\n"
else
	yellow "SKIP" "override console-operator image\n"
fi

# at this point the operator aborts configuration of OIDC as there is no oidc provider/client configured; what remains
# is to configure it in the Authentication CR so that the operator picks it up

read -p "Continue to change auth type to OIDC? " user_inp

# update authentication CR with type oidc and fake oidc provider
kubectl get authentication cluster -oyaml | grep -q myoidc
if [ $? -ne 0 ]; then
	kubectl patch authentication cluster --type="merge" -p "$(cat <<- EOF
spec:
  type: OIDC
  webhookTokenAuthenticator:
EOF
	)"
	step "patch Authentication with type OIDC\n"
else
	yellow "SKIP" "patch Authentication with type OIDC\n"
fi

read -p "Continue to patch auth CR with the OIDC client config? " user_inp

kubectl get authentication cluster -oyaml | grep -q myoidc-client
if [ $? -ne 0 ]; then
  kubectl patch authentication cluster --type="merge" -p "$(cat <<- EOF
spec:
  oidcProviders:
  - name: myoidc
    issuer:
      issuerURL: https://meh.tld
      audiences:
        - openshift-aud
    oidcClients:
    - clientID: myoidc-client
      componentName: console
      componentNamespace: openshift-console
      clientSecret:
        name: myoidc-client-secret
EOF
  )"
  step "patch Authentication with fake OIDC provider and client\n"
else
  yellow "SKIP" "patch Authentication with fake OIDC provider and client\n"
fi

read -p "Continue to create the OIDC client secret? " user_inp

kubectl -n openshift-config get secret myoidc-client-secret 2>&1 >/dev/null
if [ $? -ne 0 ]; then
  cat <<EOF | oc apply -f -
apiVersion: v1
kind: Secret
metadata:
  namespace: openshift-config
  name: myoidc-client-secret
data:
  clientSecret: c2VjcmV0Cg==
EOF
  step "create OIDC client secret\n"
else
  yellow "SKIP" "create OIDC client secret\n"
fi
