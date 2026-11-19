#!/usr/bin/env bash

# Log in to selected CI registries and store credentials in ~/.docker/config.json.
# With no arguments, log in only to registry.ci.openshift.org.

# registries
ci_registry_url="registry.ci.openshift.org"
ci_registry_token_url="https://oauth-openshift.apps.ci.l2s4.p1.openshiftapps.com/oauth/token/request"
ci_registry_build01_url="registry.build01.ci.openshift.org"
ci_registry_build01_token_url="https://oauth-openshift.apps.build01.ci.devcluster.openshift.com/oauth/token/request"
ci_registry_build02_url="registry.build02.ci.openshift.org"
ci_registry_build02_token_url="https://oauth-openshift.apps.build02.gcp.ci.openshift.org/oauth/token/request"
ci_registry_build03_url="registry.build03.ci.openshift.org"
ci_registry_build03_token_url="https://oauth-openshift.apps.build03.ci.devcluster.openshift.com/oauth/token/request"
ci_registry_build05_url="registry.build05.ci.openshift.org"
ci_registry_build05_token_url="https://oauth-openshift.apps.build05.l9oh.p1.openshiftapps.com/oauth/token/request"
ci_registry_build08_url="registry.build08.ci.openshift.org"
ci_registry_build08_token_url="https://oauth-openshift.apps.build13.gcp.ci.openshift.org/oauth/token/request"
ci_registry_build13_url="registry.build13.ci.openshift.org"
ci_registry_build13_token_url="https://oauth-openshift.apps.build13.gcp.ci.openshift.org/oauth/token/request"

docker_config="$HOME/.docker/config.json"

prompt () {
  read -p "Proceed? [yN] " yn
  case $yn in
    y|yes) ;;
    *) echo "Abort." && exit 1;;
  esac
}

do_registry () {
	local url="$1"
	local token_url="$2"

	echo -e "\n### Registry: $url"

  read -p "Proceed? [yN] " yn
  case $yn in
    y|yes) ;;
    *) return;;
  esac

  echo "Opening to default browser: $token_url"
  xdg-open $token_url

  echo "Get token from above URL and use it below to login:"
  podman login --authfile "$docker_config" "$url"
}

usage () {
  printf 'Usage: %s [registry|build01|build02|build03|build05|build13 ...]\n' "${0##*/}"
  printf 'With no arguments, selects registry only.\n'
}

main () {
  if [ "$#" -eq 0 ]; then
    set -- registry
  fi

  local target
  # Validate all selectors before starting any logins.
  for target in "$@"; do
    case "$target" in
      registry|build01|build02|build03|build05|build08|build13) ;;
      -h|--help) usage; return ;;
      *) printf 'Unknown registry selector: %s\n' "$target" >&2; usage >&2; return 2 ;;
    esac
  done

  for target in "$@"; do
    case "$target" in
      registry) do_registry "$ci_registry_url" "$ci_registry_token_url" ;;
      build01) do_registry "$ci_registry_build01_url" "$ci_registry_build01_token_url" ;;
      build02) do_registry "$ci_registry_build02_url" "$ci_registry_build02_token_url" ;;
      build03) do_registry "$ci_registry_build03_url" "$ci_registry_build03_token_url" ;;
      build05) do_registry "$ci_registry_build05_url" "$ci_registry_build05_token_url" ;;
      build08) do_registry "$ci_registry_build08_url" "$ci_registry_build08_token_url" ;;
      build13) do_registry "$ci_registry_build13_url" "$ci_registry_build13_token_url" ;;
    esac || return
  done

}

main "$@"
