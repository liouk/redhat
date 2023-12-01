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
kubectl patch clusterversion version --type="merge" --patch="$(cat <<- EOF
spec:
  overrides:
  - group: apiextensions.k8s.io
    kind: CustomResourceDefinition
    name: authentications.config.openshift.io
    namespace: ""
    unmanaged: true
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
step "CVO overrides: authentication CRD, console-operator deployment, console-operator roles\n"

# deploy new CRD
crd_file="$HOME/redhat/repos/stlaz/api/config/v1/0000_10_config-operator_01_authentication.crd-TechPreviewNoUpgrade.yaml"
echo "New CRD file: $crd_file"
read -p "Enter or new: " user_inp
[[ -n "$user_inp" ]] && crd_file="$user_inp"
if [ -f "$crd_file" ]; then
	echo "Will apply '$crd_file'"
	kubectl apply -f "$crd_file"
	step "deploy new Authentication CRD\n"
elif [[ "$user_inp" == "q" ]]; then
	yellow "SKIP" "deploy new Authentication CRD\n"
else
	red "FAIL" "deploy new Authentication CRD\n"
	exit 1
fi

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
    "path": "/rules/2/resources/-",
    "value": "authentications"
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

# update authentication CR with type oidc and fake oidc provider
kubectl get authentication cluster -oyaml | grep -q myoidc
if [ $? -ne 0 ]; then
	kubectl patch authentication cluster --type="merge" -p "$(cat <<- EOF
spec:
  type: OIDC
  webhookTokenAuthenticator:
  oidcProviders:
  - name: myoidc
    issuer:
      issuerURL: https://meh.tld
      audiences: ['openshift-aud']
EOF
	)"
	step "patch Authentication with type OIDC and OIDCProvider\n"
else
	yellow "SKIP" "patch Authentication with type OIDC and OIDCProvider\n"
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

echo stop && exit
read -p "Continue?" user_inp

# at this point the operator aborts configuration of OIDC as there is no oidc client configured; what remains
# is to create an oauthclient suitable for OIDC and configure it in the Authentication CR so that the operator
# picks it up

cat <<EOF | oc apply -f -
apiVersion: v1
kind: Secret
metadata:
	namespace: openshift-config
	name: oidc-client-secret
data:
	clientSecret: somesecret
---
apiVersion: oauth.openshift.io/v1
grantMethod: auto
kind: OAuthClient
metadata:
  name: oidc-client
redirectURIs:
- https://meh.tld
respondWithChallenges: true
EOF
step "create fake OIDC client and secret\n"


kubectl patch authentication cluster --type="merge" -p "$(cat <<- EOF
spec:
  oidcProviders:
  - name: myoidc
		oidcClients:
		- clientID: oidc-client
			componentNamespace: openshift-console
			componentName: console
			clientSecret: oidc-client-secret
EOF
)"
step "patch Authentication with fake OIDC client\n"
