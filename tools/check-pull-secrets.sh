#!/usr/bin/env bash

docker_cfg="$HOME/.docker/config.json"

# Iterate over each registry in the auths object
jq -r '.auths | keys[]' "$docker_cfg" | while read -r registry; do
	echo -n "Checking $registry ... "
	error=$(docker login "$registry" 2>&1 >/dev/null)
	if [ $? -eq 0 ]; then
		echo "ok"
	else
		echo "failed"
		echo "$error" | sed 's/^/\x1b[31m│\x1b[0m /'
		echo ""
	fi
done