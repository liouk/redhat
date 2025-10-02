#! /usr/bin/env bash

if [ "$#" -ne 1 ]; then
  echo "expected only one positional argument"
  exit 1
fi

sha256prefix="sha256~"
token=${1#"$sha256prefix"}
hash=$(echo -n $token | sha256sum | cut -d' ' -f1 | xxd -r -p | basenc --base64url)
echo -n "${sha256prefix}${hash//=/}"
