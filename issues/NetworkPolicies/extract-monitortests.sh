#!/usr/bin/env bash

set -euo pipefail

pr="$1"
specific_job="${2:-}"

namespaces=()
for prow_job in $(gh pr checks $pr --json name,state,link --jq '.[] | select(.state == "FAILURE")' | jq -c '.'); do
	job_link=$(echo $prow_job | jq -r '.link')
	job_name=$(echo $prow_job | jq -r '.name' | sed 's|^ci/prow/||')

	if [[ -n "$specific_job" && "$specific_job" != "$job_name" ]]; then
		echo "[$job_name] skipped"
    continue
  fi

	case $job_name in
		*microshift*)
			continue
			;;
	esac

	echo "[$job_name] job link: $job_link"
	echo "[$job_name] extracting artifacts URL"
	artifacts_url=$(curl -s $job_link | sed -n 's/.*href="\([^"]*\)">Artifacts.*/\1/p')
	gs_link=$(curl -s $artifacts_url | grep "gcloud storage" | sed -n 's/.*\(gs:\/\/[^ <]*\).*/\1/p')

	local_dir_name="temp_artifacts_$job_name"
	mkdir "$local_dir_name"
	subdir_name=${job_name%-1of2}
	subdir_name=${subdir_name%-2of2}

	case $job_name in
		e2e-metal*)
			platform="baremetalds"
			;;
		*)
			platform="openshift"
	esac

	junit_dir_path="artifacts/${subdir_name}/${platform}-e2e-test/artifacts/junit/"
	echo "[$job_name] downloading $junit_dir_path -> $local_dir_name"
	gcloud storage cp -r "$gs_link$junit_dir_path" "$local_dir_name" --no-user-output-enabled

	echo "[$job_name] extracting namespaces"
	pushd . >/dev/null
	failed_monitors_file=$(find . -type f -name *test-failures-summary_monitor*)
	mapfile -t namespaces < <(cat $failed_monitors_file | jq -r '.Tests[].Test.Name | select(contains("[Monitor:network-policy-invariants][sig-network] ns/")) | match("ns/([^ ]+)") | .captures[0].string' | sort -u)
	popd >/dev/null

  echo "[$job_name] found ${#namespaces[@]} failing namespaces"
	rm -rf $local_dir_name
done

echo
echo "all failing namespaces:"
echo
printf '%s\n' "${namespaces[@]}" | sort -u