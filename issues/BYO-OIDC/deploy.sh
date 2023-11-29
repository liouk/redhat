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
kubectl patch clusterversion version --type="merge" -p "$(cat <<- EOF
spec:
  overrides:
  - group: config.openshift.io
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
echo "Will apply '$crd_file'"
kubectl apply -f "$crd_file"
step "deploy new Authentication CRD\n"

# give required permissions to the console-operator SA
# as seen in https://github.com/openshift/console-operator/pull/811
kubectl -n openshift-config-managed get role console-operator -oyaml | grep -q secrets || \
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
kubectl get clusterrole console-operator -oyaml | grep -q authentications || \
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
  }
]
EOF
)"
step "update clusterrole console-operator\n"

# update authentication CR with type oidc and fake oidc provider
patch="$(cat <<- EOF
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
echo -e "Will patch Authentication with:\n$patch\n"
kubectl patch authentication cluster --type="merge" -p "$patch"
step "update Authentication with type OIDC and OIDCProvider\n"

yellow "TODO" "create fake OIDC client\n"

yellow "TODO" "update Authentication with fake OIDC client\n"

# set console-operator image
read -p "console-operator image: " img
echo "image: $img"
kubectl -n openshift-console-operator set image deployment/console-operator console-operator="$img"
step "override console-operator image\n"
