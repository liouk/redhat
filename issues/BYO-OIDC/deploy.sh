#!/usr/bin/env bash

step () {
  if [ $? -eq 0 ]; then
    echo -e "\e[42m\e[30m\e[1m  OK  \e[0m $1"
  else
    echo -e "\e[41m\e[30m\e[1m FAIL \e[0m $1"
    exit $?
  fi
}

todo () {
  echo -e "\e[43m\e[30m\e[1m TODO \e[0m $1"
}

# un-manage the Authentication CRD on the CVO
kubectl patch clusterversion version --type='merge' -p "$(cat <<- EOF
spec:
  overrides:
  - group: config.openshift.io
    kind: CustomResourceDefinition
    name: authentications.config.openshift.io
    namespace: ""
    unmanaged: true
EOF
)"
step "un-manage the Authentication CRD on the CVO"

# deploy new CRD
crd_file="$HOME/redhat/repos/stlaz/api/config/v1/0000_10_config-operator_01_authentication.crd-CustomNoUpgrade.yaml"
echo "New CRD file: $crd_file"
read -p "OK or new: " user_inp
[[ -n "$user_inp" ]] && crd_file="$user_inp"
kubectl apply -f "$crd_file"
step "deploy new Authentication CRD"

todo "deploy an OIDC provider (keycloak)"

todo "create OIDC client"

todo "update Authentication with OIDC Provider"

todo "deploy console-operator"
