#!/usr/bin/env bash

# helper script that performs a login to registry.ci.openshift.org with a new token, then saves ~/.docker/config.json as a pull secret into 1password

registry_url="registry.ci.openshift.org"
request_token_url="https://oauth-openshift.apps.ci.l2s4.p1.openshiftapps.com/oauth/token/request"
docker_config="$HOME/.docker/config.json"

prompt () {
  read -p "Proceed? [yN] " yn
  case $yn in
    y|yes) ;;
    *) echo "Abort." && exit 1;;
  esac
}

get_ci_token () {
  echo "Opening to default browser: $request_token_url"
  xdg-open $request_token_url
}

ci_login () {
  docker login $registry_url
}

op_signin() {
  local op_session_file="$HOME/.config/op/.session-token"
  OP_SESSION=$(cat $op_session_file 2>/dev/null)
  op --session "$OP_SESSION" user list > /dev/null 2>&1 && return

  OP_SESSION=$(op signin --account my --raw)
  chmod 600 "$op_session_file"
  echo -n "$OP_SESSION" > "$op_session_file"
}

save_pull_secret () {
  echo "Constructing pull secret from $docker_config"
  new_pull_secret=$(cat $docker_config | jq --compact-output)

  item=$(echo "$OP_ITEM_OCP_PULL_SECRET" | cut -d'/' -f4)
  field=$(echo "$OP_ITEM_OCP_PULL_SECRET" | cut -d'/' -f5)
  op_signin
  op --session "$OP_SESSION" item edit "$item" --vault "$OP_VAULT_OCP" "$field"="$new_pull_secret" > /dev/null
  echo "Pull secret saved in $OP_ITEM_OCP_PULL_SECRET"
}

main () {
  echo -e "### STEP 1: Obtain token for $registry_url"
  get_ci_token 2>/dev/null
  prompt

  echo -e "\n### STEP 2: Login to $registry_url"
  ci_login
  prompt

  echo -e "\n### STEP 3: Save pull secret to 1password"
  save_pull_secret
}

main "$@"
