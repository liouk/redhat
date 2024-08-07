#!/usr/bin/env bash

# helper script that performs a login to registry.ci.openshift.org with a new token, then saves ~/.docker/config.json as a pull secret into 1password

# registries
ci_registry_url="registry.ci.openshift.org"
ci_registry_token_url="https://oauth-openshift.apps.ci.l2s4.p1.openshiftapps.com/oauth/token/request"
ci_registry_build05_url="registry.build05.ci.openshift.org"
ci_registry_build05_token_url="https://oauth-openshift.apps.build05.l9oh.p1.openshiftapps.com/oauth/token/request"

docker_config="$HOME/.docker/config.json"

prompt () {
  read -p "Proceed? [yN] " yn
  case $yn in
    y|yes) ;;
    *) echo "Abort." && exit 1;;
  esac
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

do_registry () {
	local url="$1"
	local token_url="$2"

	echo -e "\n### Registry: $url"
  echo "Opening to default browser: $token_url"
  xdg-open $token_url

  echo "Get token from above URL and use it below to login:"
  docker login $url

  prompt
}

main () {
	do_registry $ci_registry_url $ci_registry_token_url
	do_registry $ci_registry_build05_url $ci_registry_build05_token_url

  echo -e "\n### Save pull secret to 1password"
  save_pull_secret
}

main "$@"
