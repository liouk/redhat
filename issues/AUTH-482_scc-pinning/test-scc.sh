#!/usr/bin/env bash

target_file="$1"

for scc in "anyuid" "hostaccess" "hostmount-anyuid" "hostnetwork" "hostnetwork-v2" "nonroot" "nonroot-v2" "privileged" "restricted" "restricted-v2"; do
	sed -i "s|openshift.io/required-scc: .*|openshift.io/required-scc: $scc|g" $target_file
	grep "required-scc" $target_file
	oc apply --dry-run='server' -f $target_file
	echo
done
