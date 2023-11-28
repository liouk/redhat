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

start_from_step="${1:-0}"

if [ $start_from_step -le 1 ]; then
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
  step "un-manage the Authentication CRD on the CVO\n"
else
  yellow "SKIP" "un-manage the Authentication CRD on the CVO\n"
fi

if [ $start_from_step -le 2 ]; then
  # deploy new CRD
  crd_file="$HOME/redhat/repos/stlaz/api/config/v1/0000_10_config-operator_01_authentication.crd-TechPreviewNoUpgrade.yaml"
  echo "New CRD file: $crd_file"
  read -p "Enter or new: " user_inp
  [[ -n "$user_inp" ]] && crd_file="$user_inp"
  echo "Will apply '$crd_file'"
  kubectl apply -f "$crd_file"
  step "deploy new Authentication CRD\n"
else
  yellow "SKIP" "deploy new Authentication CRD\n"
fi

if [ $start_from_step -le 3 ]; then
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
  kubectl patch authentication cluster --type='merge' -p "$patch"
  step "update Authentication with type OIDC and OIDCProvider\n"
else
  yellow "SKIP" "update Authentication with type OIDC and OIDCProvider\n"
fi

yellow "TODO" "create fake OIDC client\n"

yellow "TODO" "update Authentication with fake OIDC client\n"

yellow "TODO" "deploy console-operator\n"
